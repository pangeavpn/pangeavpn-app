package api

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestOfflineForState(t *testing.T) {
	cases := []struct {
		st          state.DaemonState
		hostOffline bool
		want        bool
	}{
		{state.StateConnected, true, true},
		{state.StateConnecting, true, true},
		{state.StateError, true, true},
		{state.StateDisconnected, true, true},
		{state.StateDisconnected, false, false},
		{state.StateDisconnecting, true, false},
		{state.StateConnected, false, false},
		{state.StateError, false, false},
	}
	for _, c := range cases {
		if got := offlineForState(c.st, c.hostOffline); got != c.want {
			t.Errorf("offlineForState(%q, %v) = %v, want %v", c.st, c.hostOffline, got, c.want)
		}
	}
}

// When the OS reports no internet, a dropped session must hold, not churn
// through rebuild attempts, and status must carry the offline flag.
func TestHealthCheck_HoldsWhenHostOffline(t *testing.T) {
	svc, naive, wgMgr, _ := recoveryTestService(t)
	svc.hostInternet = func() (bool, bool) { return false, true }
	svc.physicalRoute = func() (string, string, error) { return "", "", platform.ErrNoDefaultRoute }

	dropSession(naive, wgMgr)
	for range 4 {
		svc.runHealthCheck(context.Background())
	}

	status := svc.Status(context.Background())
	if !status.Offline {
		t.Errorf("Offline = false, want true while the OS reports no internet")
	}
	if status.Reconnecting {
		t.Error("Reconnecting = true, want a stable hold rather than churning attempts while offline")
	}
	if svc.recoveryPending() {
		t.Error("recovery attempts accumulated while offline; expected a hold, not a retry loop")
	}

	// Link returns: recovery resumes and the session comes back, offline clears.
	svc.hostInternet = func() (bool, bool) { return true, true }
	svc.physicalRoute = func() (string, string, error) { return "eth0", "192.0.2.1", nil }
	restoreNetwork(naive)
	for range 3 {
		svc.runHealthCheck(context.Background())
	}
	status = svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q (%s), want CONNECTED after the link returned", status.State, status.Detail)
	}
	if status.Offline {
		t.Error("Offline = true after reconnecting")
	}
}

// A link drop makes the transport's restart hit "unreachable network"; instead
// of thrashing ERROR/CONNECTED every ~3s, the daemon parks in an offline hold.
func TestHealthCheck_TransportRestartHoldsOnUnreachableNetwork(t *testing.T) {
	svc, naive, _, _ := recoveryTestService(t)

	// Link drops mid-session: the transport is down and its restart can't route
	// out. hostInternet stays "unknown" so this exercises the instant signal.
	naive.mu.Lock()
	naive.running = false
	naive.startErr = errors.New("dial tcp 95.179.239.1:443: connectex: A socket operation was attempted to an unreachable network.")
	naive.mu.Unlock()

	svc.runHealthCheck(context.Background())

	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED held in the offline hold, not flipped to ERROR", status.State)
	}
	if !status.Offline {
		t.Error("Offline = false, want true after an unreachable-network restart")
	}
	if !svc.offlineHoldActive() {
		t.Error("offline hold not active after an unreachable-network restart")
	}

	// Ticks within the hold must not hammer restart or thrash the state.
	naive.mu.Lock()
	naive.startCalled = false
	naive.mu.Unlock()
	svc.runHealthCheck(context.Background())
	svc.runHealthCheck(context.Background())
	if transportStarted(naive) {
		t.Error("transport restart was hammered while in the offline hold")
	}
	if st, _ := svc.machine.Get(); st != state.StateConnected {
		t.Errorf("state = %q during hold, want a stable CONNECTED (no ERROR thrash)", st)
	}

	// The link returns: a network-change event clears the hold and recovery runs.
	naive.mu.Lock()
	naive.startErr = nil
	naive.running = true
	naive.mu.Unlock()
	svc.onNetworkChanged()
	svc.runHealthCheck(context.Background())
	if svc.Status(context.Background()).Offline {
		t.Error("Offline = true after the link returned")
	}
}

// failFirstDialOffline makes the next naive dial fail "unreachable", optionally
// after the network changes mid-dial; later dials succeed.
func failFirstDialOffline(naive *fakeNaiveManager, midDial func()) {
	var taken atomic.Bool
	naive.mu.Lock()
	defer naive.mu.Unlock()
	naive.startHook = func(context.Context) error {
		if !taken.CompareAndSwap(false, true) {
			return nil
		}
		midDial()
		return errUnreachableNetwork
	}
}

// Only a moved physical network counts: our own route writes fire the same
// events but leave the fingerprint alone, and must not skip the hold.
var networkChangeMidDialCases = []struct {
	name    string
	event   bool
	changed bool
}{
	{"network moved mid-dial", true, true},
	{"own route churn mid-dial", true, false},
	{"no network change", false, false},
}

// midDialNetwork points svc's fingerprint at a switchable key and returns the
// mid-dial action for tc: fire the event, after moving the network if tc says so.
func midDialNetwork(svc *Service, event, moved bool) func() {
	var key atomic.Value
	key.Store("eth0:192.0.2.10")
	svc.networkKey = func() string { return key.Load().(string) }
	return func() {
		if !event {
			return
		}
		if moved {
			key.Store("wlan0:192.0.2.55")
		}
		svc.onNetworkChanged()
	}
}

// assertOfflineOutcomeLogged checks the failed dial logged what actually
// follows it, and only that: a hold, or a retry because the network moved.
func assertOfflineOutcomeLogged(t *testing.T, svc *Service, changed bool) {
	t.Helper()
	held, retried := logMentions(svc, "until it returns"), logMentions(svc, "changed during the attempt")
	if held == changed || retried != changed {
		t.Errorf("logged hold=%v retry=%v, want hold=%v retry=%v", held, retried, !changed, changed)
	}
}

// A restart dialled before the link returned and failing after it used to
// re-arm the hold the change had just cleared, so its kick found it armed.
func TestHealthCheck_NetworkChangeDuringFailedRestartRetriesNow(t *testing.T) {
	for _, tc := range networkChangeMidDialCases {
		t.Run(tc.name, func(t *testing.T) {
			svc, naive, _, _ := recoveryTestService(t)
			ctx := context.Background()
			naive.mu.Lock()
			naive.running = false
			naive.mu.Unlock()
			failFirstDialOffline(naive, midDialNetwork(svc, tc.event, tc.changed))

			svc.runHealthCheck(ctx)
			if !svc.Status(ctx).Offline {
				t.Error("Offline = false after an unreachable restart, want the UI to keep saying offline")
			}
			if armed := svc.offlineHoldActive(); armed == tc.changed {
				t.Fatalf("offline hold armed = %v, want %v", armed, !tc.changed)
			}
			assertOfflineOutcomeLogged(t, svc, tc.changed)

			restoreNetwork(naive)
			svc.runHealthCheck(ctx)
			if retried := transportStarted(naive); retried != tc.changed {
				t.Errorf("restart retried on the next check = %v, want %v", retried, tc.changed)
			}
		})
	}
}

// The same race through a bring-up, where the health loop has already spent the
// change's kick on a check that found CONNECTING.
func TestConnect_NetworkChangeDuringOfflineDialRetriesNow(t *testing.T) {
	for _, tc := range networkChangeMidDialCases {
		t.Run(tc.name, func(t *testing.T) {
			naive := &fakeNaiveManager{}
			svc := newTestService(t, &fakeCloakManager{}, naive, &fakeWGManager{}, &fakeKillSwitch{}, silentTunnelProfile())
			svc.recoveryDelays = []time.Duration{0}
			ctx := context.Background()
			change := midDialNetwork(svc, tc.event, tc.changed)
			failFirstDialOffline(naive, func() {
				change()
				select {
				case <-svc.healthKick:
				default:
				}
			})

			if err := svc.Connect(ctx, "p1", ConnectOptions{PreferredTransport: "naive"}); !errors.Is(err, ErrHostOffline) {
				t.Fatalf("Connect error = %v, want ErrHostOffline", err)
			}
			if status := svc.Status(ctx); status.State != state.StateError || !status.Offline {
				t.Fatalf("state = %q offline=%v, want ERROR held offline", status.State, status.Offline)
			}
			if armed := svc.offlineHoldActive(); armed == tc.changed {
				t.Fatalf("offline hold armed = %v, want %v", armed, !tc.changed)
			}
			if kicked := len(svc.healthKick) == 1; kicked != tc.changed {
				t.Errorf("health loop kicked = %v, want %v", kicked, tc.changed)
			}
			assertOfflineOutcomeLogged(t, svc, tc.changed)

			restoreNetwork(naive)
			svc.runHealthCheck(ctx)
			if connected := svc.Status(ctx).State == state.StateConnected; connected != tc.changed {
				t.Errorf("connected on the next check = %v, want %v", connected, tc.changed)
			}
		})
	}
}

// From an existing ERROR state, retryDroppedSession must not rebuild or flip to
// a transient CONNECTED while the host is offline.
func TestRetryDroppedSession_HoldsWhenOffline(t *testing.T) {
	svc, _, _, _ := recoveryTestService(t)
	svc.hostInternet = func() (bool, bool) { return false, true }
	svc.physicalRoute = func() (string, string, error) { return "", "", platform.ErrNoDefaultRoute }
	svc.machine.Set(state.StateError, "connection lost")

	svc.retryDroppedSession(context.Background())

	if st, _ := svc.machine.Get(); st != state.StateError {
		t.Fatalf("state = %q, want it held at ERROR while offline", st)
	}
	if svc.recoveryPending() {
		t.Error("a rebuild was booked while offline; expected a hold")
	}
	if !svc.Status(context.Background()).Offline {
		t.Error("Offline = false in ERROR while the OS reports no internet")
	}
}

// Connect with no internet parks the session (ERROR + offline, kill switch
// armed, profile kept) instead of failing, and recovery connects it on a link.
func TestConnect_HoldsOfflineUntilTheNetworkReturns(t *testing.T) {
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, naive, wgMgr, ks, silentTunnelProfile())
	svc.recoveryDelays = []time.Duration{0}
	svc.networkKey = func() string { return "eth0:192.0.2.10" }
	naive.mu.Lock()
	naive.startErr = errors.New("dial tcp 95.179.239.1:443: connectex: A socket operation was attempted to an unreachable network.")
	naive.mu.Unlock()

	err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"})
	if !errors.Is(err, ErrHostOffline) {
		t.Fatalf("Connect error = %v, want ErrHostOffline", err)
	}

	status := svc.Status(context.Background())
	if status.State != state.StateError || status.Detail != offlineHoldDetail {
		t.Fatalf("status = %q (%s), want ERROR with %q", status.State, status.Detail, offlineHoldDetail)
	}
	if !status.Offline || status.Reconnecting || status.TransportsExhausted {
		t.Errorf("Offline=%v Reconnecting=%v TransportsExhausted=%v, want a plain offline hold", status.Offline, status.Reconnecting, status.TransportsExhausted)
	}
	if !ks.Active() {
		t.Error("kill switch not armed while holding for the network")
	}

	// Ticks inside the hold must not re-dial.
	naive.mu.Lock()
	naive.startCalled = false
	naive.mu.Unlock()
	svc.runHealthCheck(context.Background())
	svc.runHealthCheck(context.Background())
	if transportStarted(naive) {
		t.Error("connect was re-dialled inside the offline hold")
	}

	// The hold expires with the host still offline: the re-dial fails again and
	// the status keeps saying no internet rather than flashing a plain ERROR.
	svc.recoveryMu.Lock()
	svc.offlineHoldUntil = time.Time{}
	svc.recoveryMu.Unlock()
	svc.runHealthCheck(context.Background())
	if !transportStarted(naive) {
		t.Error("expected a re-dial once the hold expired")
	}
	status = svc.Status(context.Background())
	if status.State != state.StateError || !status.Offline || status.Reconnecting {
		t.Fatalf("status = %q offline=%v reconnecting=%v after a failed re-dial, want ERROR still held offline", status.State, status.Offline, status.Reconnecting)
	}
	if !svc.offlineHoldActive() {
		t.Error("failed re-dial did not re-enter the offline hold")
	}

	restoreNetwork(naive)
	svc.onNetworkChanged()
	svc.runHealthCheck(context.Background())

	status = svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q (%s), want CONNECTED once the link returned", status.State, status.Detail)
	}
	if status.Offline || status.Reconnecting {
		t.Errorf("Offline=%v Reconnecting=%v after connecting", status.Offline, status.Reconnecting)
	}
	if !ks.Active() {
		t.Error("kill switch not armed after the held connect landed")
	}
}

// Disconnect while parked must drop the intent: nothing re-dials afterwards.
func TestDisconnect_WhileHoldingOfflineDropsTheSession(t *testing.T) {
	naive := &fakeNaiveManager{}
	svc := newTestService(t, &fakeCloakManager{}, naive, &fakeWGManager{}, &fakeKillSwitch{}, silentTunnelProfile())
	svc.recoveryDelays = []time.Duration{0}
	svc.networkKey = func() string { return "eth0:192.0.2.10" }
	naive.mu.Lock()
	naive.startErr = errors.New("dial udp 192.0.2.1:8488: connect: network is unreachable")
	naive.mu.Unlock()
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); !errors.Is(err, ErrHostOffline) {
		t.Fatalf("Connect error = %v, want ErrHostOffline", err)
	}

	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}
	status := svc.Status(context.Background())
	if status.State != state.StateDisconnected || status.Offline {
		t.Fatalf("status = %q offline=%v, want DISCONNECTED and not offline", status.State, status.Offline)
	}

	restoreNetwork(naive)
	svc.onNetworkChanged()
	svc.runHealthCheck(context.Background())
	if transportStarted(naive) {
		t.Error("a disconnected session was re-dialled when the link returned")
	}
}

// The first "unreachable network" dial ends the cascade: the other transports
// would fail the same way, each after its own handshake timeout.
func TestConnect_UnreachableNetworkStopsTheCascade(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})
	probe.reality.mu.Lock()
	probe.reality.startErr = errors.New("reality: handshake: dial tcp 95.179.239.1:443: connectex: A socket operation was attempted to an unreachable network.")
	probe.reality.mu.Unlock()

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})
	if !errors.Is(err, ErrHostOffline) {
		t.Fatalf("Connect error = %v, want ErrHostOffline", err)
	}
	if errors.Is(err, ErrTransportExhausted) {
		t.Error("an offline host must not read as exhausted transports")
	}
	if order := probe.order(); len(order) != 0 {
		t.Errorf("cascade kept walking after the unreachable dial: %v", order)
	}
	if !svc.Status(context.Background()).Offline {
		t.Error("Offline = false after an unreachable-network connect")
	}
}

// Behind an armed kill switch the OS can't probe for internet, so the route
// table decides — trusting the probe verdict would never learn the link is back.
func TestConnect_BehindTheKillSwitchTheRouteTableOutranksTheOSVerdict(t *testing.T) {
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, naive, wgMgr, ks, silentTunnelProfile())
	svc.hostInternet = func() (bool, bool) { return false, true }
	svc.physicalRoute = func() (string, string, error) { return "Wi-Fi", "192.0.2.1", nil }
	naive.mu.Lock()
	naive.startErr = errors.New("dial tcp 203.0.113.9:443: i/o timeout")
	naive.mu.Unlock()

	err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive", Lockdown: true})
	if err == nil || errors.Is(err, ErrHostOffline) {
		t.Fatalf("Connect error = %v, want the transport's own failure rather than an offline hold", err)
	}
	if !ks.Active() {
		t.Fatal("kill switch should stay armed after a failed connect")
	}
	if status := svc.Status(context.Background()); status.Offline {
		t.Error("Offline = true while a physical default route exists behind the kill switch")
	}

	// No route out behind the lock is a genuine outage, whatever the OS thinks.
	svc.physicalRoute = func() (string, string, error) { return "", "", platform.ErrNoDefaultRoute }
	if status := svc.Status(context.Background()); !status.Offline {
		t.Error("Offline = false with no physical default route behind the kill switch")
	}
}

// The kill switch stays armed for the whole session, so a drop under lockdown
// must resume on the route table, not a probe verdict the lock never lets through.
func TestRetryDroppedSession_ResumesBehindTheKillSwitchOnARoute(t *testing.T) {
	svc, naive, wgMgr, ks := recoveryTestService(t)
	if !ks.Active() {
		t.Fatal("expected the session to run with the kill switch armed")
	}
	svc.hostInternet = func() (bool, bool) { return false, true }
	svc.physicalRoute = func() (string, string, error) { return "", "", platform.ErrNoDefaultRoute }

	dropSession(naive, wgMgr)
	for range 3 {
		svc.runHealthCheck(context.Background())
	}
	if !svc.Status(context.Background()).Offline {
		t.Fatal("Offline = false with the link down")
	}

	svc.physicalRoute = func() (string, string, error) { return "Wi-Fi", "192.0.2.1", nil }
	restoreNetwork(naive)
	svc.onNetworkChanged()
	for range 3 {
		svc.runHealthCheck(context.Background())
	}
	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q (%s), want CONNECTED once the route returned", status.State, status.Detail)
	}
	if status.Offline {
		t.Error("Offline = true after the route returned")
	}
}
