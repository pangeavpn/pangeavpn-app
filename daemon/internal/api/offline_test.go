package api

import (
	"context"
	"errors"
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

// When the OS reports no internet, a dropped session must hold — not churn
// through rebuild attempts flipping between ERROR/CONNECTED/CONNECTING — and the
// status must carry the offline flag so the UI can say "no internet".
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

// The exact churn a link drop caused: the active transport dies, its restart
// gets "unreachable network", and instead of stamping ERROR every ~3s (which a
// still-recent handshake flips back to CONNECTED) the daemon parks in a stable
// offline hold and reports offline, then recovers when the link returns.
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

// Behind an armed kill switch the OS cannot probe for internet, so its "no
// internet" is blind: the route table decides. A lockdown lock that trusted the
// probe verdict could never be told the link is back.
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

// The kill switch is armed for the whole life of a session, so a dropped
// session under lockdown must resume on the route table, not wait for a probe
// verdict the lock will never let through.
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
