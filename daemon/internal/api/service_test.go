package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// ---------------------------------------------------------------------------
// Fake implementations
// ---------------------------------------------------------------------------

type fakeCloakManager struct {
	mu             sync.Mutex
	running        bool
	startErr       error
	stopErr        error
	waitErr        error
	startCalled    bool
	startCount     int
	stopCount      int
	startLocalPort int
	boundLocalPort int
}

func (f *fakeCloakManager) Start(_ context.Context, profile state.CloakProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalled = true
	f.startCount++
	f.startLocalPort = profile.LocalPort
	if f.startErr != nil {
		return f.startErr
	}
	f.running = true
	return nil
}

func (f *fakeCloakManager) BoundLocalPort() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.boundLocalPort
}

func (f *fakeCloakManager) Stop(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCount++
	if f.stopErr != nil {
		return f.stopErr
	}
	f.running = false
	return nil
}

func (f *fakeCloakManager) Status() state.CloakStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return state.CloakStatus{Running: f.running}
}

func (f *fakeCloakManager) WaitForSession(_ context.Context, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

type fakeNaiveManager struct {
	mu          sync.Mutex
	startCalled bool
	startErr    error
	stopCalled  bool
	running     bool

	// stayDown makes Start succeed (no startErr) without ever flipping
	// running to true, simulating a transport process that exits
	// immediately after a successful launch (waitForManagedTransportStable
	// then times out).
	stayDown bool

	// waitErr, when set, is returned by WaitForSession — simulating a
	// transport whose process/session came up (Start + stability check both
	// succeeded) but never completed its handshake.
	waitErr error

	// boundLocalPort, when non-zero, overrides the default BoundLocalPort
	// return value below.
	boundLocalPort int
}

func (f *fakeNaiveManager) Start(ctx context.Context, profile state.NaiveProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalled = true
	if f.startErr != nil {
		return f.startErr
	}
	if !f.stayDown {
		f.running = true
	}
	return nil
}

func (f *fakeNaiveManager) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalled = true
	f.running = false
	return nil
}

func (f *fakeNaiveManager) Status() state.TransportStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return state.TransportStatus{Running: f.running}
}

func (f *fakeNaiveManager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

func (f *fakeNaiveManager) BoundLocalPort() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.boundLocalPort != 0 {
		return f.boundLocalPort
	}
	return 51822
}

type fakeWGManager struct {
	mu             sync.Mutex
	running        bool
	startErr       error
	stopErr        error
	statusErr      error
	preflightErr   error
	interfaceName  string
	interfaceErr   error
	startCount     int
	stopCount      int
	preflightCount int
}

func (f *fakeWGManager) Start(_ context.Context, _ state.WireGuardProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCount++
	if f.startErr != nil {
		return f.startErr
	}
	f.running = true
	return nil
}

func (f *fakeWGManager) Stop(_ context.Context, _ state.WireGuardProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCount++
	if f.stopErr != nil {
		return f.stopErr
	}
	f.running = false
	return nil
}

func (f *fakeWGManager) Status(_ context.Context, _ state.WireGuardProfile) (state.WireGuardStatus, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.statusErr != nil {
		return state.WireGuardStatus{}, f.statusErr
	}
	return state.WireGuardStatus{Running: f.running, Detail: "fake"}, nil
}

func (f *fakeWGManager) Preflight(_ context.Context, _ state.WireGuardProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.preflightCount++
	return f.preflightErr
}

func (f *fakeWGManager) ActiveInterfaceName(_ context.Context, _ state.WireGuardProfile) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.interfaceErr != nil {
		return "", f.interfaceErr
	}
	return f.interfaceName, nil
}

func (f *fakeWGManager) ActiveLUIDs() map[uint64]struct{} {
	return map[uint64]struct{}{}
}

type fakeKillSwitch struct {
	mu              sync.Mutex
	active          bool
	enableEndpoints []string
	enableAllowLAN  bool
	updateInterface string
	enableCount     int
	updateCount     int
	clearCount      int
	enableErr       error
	updateErr       error
	clearErr        error
}

func (f *fakeKillSwitch) Enable(_ context.Context, endpoints []string, allowLAN bool, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enableCount++
	f.enableEndpoints = append(f.enableEndpoints[:0], endpoints...)
	f.enableAllowLAN = allowLAN
	if f.enableErr != nil {
		return f.enableErr
	}
	f.active = true
	return nil
}

func (f *fakeKillSwitch) Update(_ context.Context, iface string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCount++
	f.updateInterface = iface
	if f.updateErr != nil {
		return f.updateErr
	}
	return nil
}

func (f *fakeKillSwitch) Clear(_ context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.clearCount++
	if f.clearErr != nil {
		return f.clearErr
	}
	f.active = false
	return nil
}

func (f *fakeKillSwitch) Active() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

// Verify fakeKillSwitch satisfies the interface.
var _ platform.KillSwitch = (*fakeKillSwitch)(nil)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func testProfile() state.Profile {
	return state.Profile{
		ID:   "test-profile-1",
		Name: "Test",
		Cloak: state.CloakProfile{
			LocalPort:  51820,
			RemoteHost: "vpn.example.com",
			RemotePort: 443,
		},
		WireGuard: state.WireGuardProfile{
			ConfigText: "[Interface]\nPrivateKey = YWJjZGVmZw==\n\n[Peer]\nPublicKey = eHl6MTIzNDU=\nEndpoint = 10.0.0.1:51820\nAllowedIPs = 0.0.0.0/0\n",
			TunnelName: "PangeaVPN",
		},
	}
}

func testConfigStore(t *testing.T, profiles ...state.Profile) *state.ConfigStore {
	t.Helper()
	dir := t.TempDir()
	cs, err := state.NewConfigStore(dir + "/config.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) > 0 {
		if err := cs.Set(state.Config{Profiles: profiles}); err != nil {
			t.Fatal(err)
		}
	}
	return cs
}

func newTestService(
	t *testing.T,
	cloak *fakeCloakManager,
	naive *fakeNaiveManager,
	wgMgr *fakeWGManager,
	ks *fakeKillSwitch,
	profiles ...state.Profile,
) *Service {
	t.Helper()
	machine := state.NewMachine()
	logs := state.NewLogStore(100)
	config := testConfigStore(t, profiles...)
	return NewService(machine, logs, config, cloak, naive, wgMgr, ks)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestConnect_KillSwitchEnabledBeforeCloakAndWG(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.enableCount != 1 {
		t.Errorf("expected enable called once, got %d", ks.enableCount)
	}
	if len(ks.enableEndpoints) == 0 || ks.enableEndpoints[0] != profile.Cloak.RemoteHost {
		t.Errorf("expected enable endpoints[0]=%q, got %v", profile.Cloak.RemoteHost, ks.enableEndpoints)
	}
	if !ks.active {
		t.Error("expected kill switch to be active after connect")
	}
}

func TestConnect_UpdateCalledAfterWGSuccess(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.updateCount != 1 {
		t.Errorf("expected update called once, got %d", ks.updateCount)
	}
	if ks.updateInterface != profile.WireGuard.TunnelName {
		t.Errorf("expected update interface %q, got %q", profile.WireGuard.TunnelName, ks.updateInterface)
	}
}

func TestConnect_UsesReportedWireGuardInterfaceForKillSwitch(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{interfaceName: "PangeaVPN Tunnel"}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.updateCount != 1 {
		t.Errorf("expected update called once, got %d", ks.updateCount)
	}
	if ks.updateInterface != wgMgr.interfaceName {
		t.Errorf("expected update interface %q, got %q", wgMgr.interfaceName, ks.updateInterface)
	}
}

func TestConnect_WGFailure_KillSwitchStaysActive(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{startErr: errors.New("wg failed")}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail")
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.enableCount != 1 {
		t.Errorf("expected enable called once, got %d", ks.enableCount)
	}
	if ks.clearCount != 0 {
		t.Errorf("expected clear NOT called on failure, got %d", ks.clearCount)
	}
	if !ks.active {
		t.Error("expected kill switch to remain active after connect failure (fail-closed)")
	}
}

func TestConnect_CloakSessionFailure_KillSwitchStaysActive(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{waitErr: errors.New("session timeout")}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail when cloak session is not established")
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active {
		t.Error("expected kill switch to remain active after cloak session failure (fail-closed)")
	}
}

func TestConnect_CloakFailure_KillSwitchStaysActive(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{startErr: errors.New("cloak failed")}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail")
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active {
		t.Error("expected kill switch to remain active after cloak failure (fail-closed)")
	}
	if ks.clearCount != 0 {
		t.Errorf("expected clear NOT called on failure, got %d", ks.clearCount)
	}
}

func TestDisconnect_ClearsKillSwitch(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	// Connect first.
	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// Disconnect should clear the kill switch.
	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.clearCount != 1 {
		t.Errorf("expected clear called once, got %d", ks.clearCount)
	}
	if ks.active {
		t.Error("expected kill switch to be inactive after disconnect")
	}
}

func TestDisconnect_ClearsKillSwitchAfterFailedConnect(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{startErr: errors.New("wg failed")}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	// Connect fails — kill switch remains active.
	_ = svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if !ks.Active() {
		t.Fatal("expected kill switch active after failed connect")
	}

	// User calls disconnect to unlock.
	// Reset wg error so disconnect succeeds.
	wgMgr.mu.Lock()
	wgMgr.startErr = nil
	wgMgr.mu.Unlock()

	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}

	if ks.Active() {
		t.Error("expected kill switch cleared after disconnect")
	}
}

func TestConnect_KillSwitchEnableError_ReturnsError(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{enableErr: errors.New("firewall error")}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail when kill switch enable fails")
	}

	// Cloak and WG should not have been started.
	cloak.mu.Lock()
	cloakStarts := cloak.startCount
	cloak.mu.Unlock()

	wgMgr.mu.Lock()
	wgStarts := wgMgr.startCount
	wgMgr.mu.Unlock()

	if cloakStarts != 0 {
		t.Errorf("expected cloak not started, got %d starts", cloakStarts)
	}
	if wgStarts != 0 {
		t.Errorf("expected wg not started, got %d starts", wgStarts)
	}
}

func TestConnect_WGPreflightFailure_DoesNotEnableKillSwitch(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{preflightErr: errors.New("wireguard runtime unavailable")}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail when wireguard preflight fails")
	}

	ks.mu.Lock()
	enableCount := ks.enableCount
	ks.mu.Unlock()
	if enableCount != 0 {
		t.Errorf("expected kill switch enable not called, got %d calls", enableCount)
	}

	cloak.mu.Lock()
	cloakStarts := cloak.startCount
	cloak.mu.Unlock()
	if cloakStarts != 0 {
		t.Errorf("expected cloak not started, got %d starts", cloakStarts)
	}
}

func TestRewriteLoopbackEndpointPort(t *testing.T) {
	tests := []struct {
		name       string
		in         string
		newPort    int
		wantSub    string
		wantRepl   bool
		mustNotSub string
	}{
		{
			name:     "rewrites loopback endpoint",
			in:       "[Interface]\nPrivateKey = x\n\n[Peer]\nPublicKey = y\nEndpoint = 127.0.0.1:51820\nAllowedIPs = 0.0.0.0/0\n",
			newPort:  54321,
			wantSub:  "Endpoint = 127.0.0.1:54321",
			wantRepl: true,
		},
		{
			name:       "ignores non-loopback endpoint",
			in:         "[Peer]\nEndpoint = 10.0.0.1:51820\n",
			newPort:    54321,
			wantRepl:   false,
			mustNotSub: "54321",
		},
		{
			name:     "preserves trailing whitespace",
			in:       "[Peer]\nEndpoint = 127.0.0.1:51820  \n",
			newPort:  9999,
			wantSub:  "Endpoint = 127.0.0.1:9999",
			wantRepl: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, replaced := rewriteLoopbackEndpointPort(tc.in, tc.newPort)
			if replaced != tc.wantRepl {
				t.Errorf("replaced = %v, want %v", replaced, tc.wantRepl)
			}
			if tc.wantSub != "" && !strings.Contains(got, tc.wantSub) {
				t.Errorf("output missing %q:\n%s", tc.wantSub, got)
			}
			if tc.mustNotSub != "" && strings.Contains(got, tc.mustNotSub) {
				t.Errorf("output should not contain %q:\n%s", tc.mustNotSub, got)
			}
		})
	}
}

func TestConnect_UsesEphemeralCloakPortAndRewritesEndpoint(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.ConfigText = "[Interface]\nPrivateKey = YWJjZGVmZw==\n\n[Peer]\nPublicKey = eHl6MTIzNDU=\nEndpoint = 127.0.0.1:51820\nAllowedIPs = 0.0.0.0/0\n"

	cloak := &fakeCloakManager{boundLocalPort: 61234}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	cloak.mu.Lock()
	if cloak.startLocalPort != 0 {
		t.Errorf("cloak.Start should receive LocalPort=0 to request ephemeral; got %d", cloak.startLocalPort)
	}
	cloak.mu.Unlock()

	stored, ok := svc.getCurrentProfile()
	if !ok {
		t.Fatal("expected current profile to be set after connect")
	}
	if stored.Cloak.LocalPort != 61234 {
		t.Errorf("stored profile Cloak.LocalPort = %d, want 61234 (bound port)", stored.Cloak.LocalPort)
	}
}

func TestConnect_CloakOnlyProfile_DoesNotAttemptNaive(t *testing.T) {
	profile := state.Profile{
		ID:    "p1",
		Name:  "p1",
		Cloak: state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
	cloakMgr := &fakeCloakManager{}
	naiveMgr := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloakMgr, naiveMgr, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !cloakMgr.startCalled {
		t.Fatalf("expected cloak.Start to be called")
	}
	if naiveMgr.startCalled {
		t.Fatalf("naive.Start should not be called when profile.Naive is nil")
	}
	status := svc.Status(context.Background())
	if status.ActiveTransport != "cloak" {
		t.Fatalf("ActiveTransport = %q, want cloak", status.ActiveTransport)
	}
}

func TestConnect_CloakFails_FallsBackToNaive(t *testing.T) {
	profile := state.Profile{
		ID:    "p1",
		Name:  "p1",
		Cloak: state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Naive: &state.NaiveProfile{RemoteHost: "example.com", RemotePort: 443, Username: "u", Password: "p"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloakMgr, naiveMgr, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !cloakMgr.startCalled {
		t.Fatalf("expected cloak.Start to be attempted first")
	}
	if !naiveMgr.startCalled {
		t.Fatalf("expected naive.Start to be attempted after cloak failed")
	}
	status := svc.Status(context.Background())
	if status.ActiveTransport != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive", status.ActiveTransport)
	}
}

// naiveFallbackProfile builds a profile that falls back to naive (cloak
// always fails to start) for the fallbackToNaive regression tests below.
// Naive.LocalPort and the WireGuard config's loopback Endpoint line are
// both set to origNaiveLocalPort so rebindWireGuardEndpoint has a matching
// line to rewrite when the fake reports a different bound port.
func naiveFallbackProfile(origNaiveLocalPort int) state.Profile {
	return state.Profile{
		ID:   "p1",
		Name: "p1",
		Cloak: state.CloakProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
			LocalPort:  51810,
		},
		Naive: &state.NaiveProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
			Username:   "u",
			Password:   "p",
			LocalPort:  origNaiveLocalPort,
		},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: fmt.Sprintf("[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:%d\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n", origNaiveLocalPort),
		},
	}
}

// TestFallbackToNaive_DoesNotMutateConfigStoreProfile is the regression test
// for the config-store aliasing bug: state.ConfigStore's clone-on-read
// (cloneProfile in config_store.go) only deep-copies
// WireGuard.DNS/BypassHosts, not the Naive pointer, so profile.Naive
// returned by FindProfile aliases the config store's own internal
// *state.NaiveProfile. Before the fix, fallbackToNaive's
// `profile.Naive.LocalPort = s.rebindWireGuardEndpoint(...)` wrote straight
// through that alias, mutating the config store's data without its lock.
// This test proves that after a fallback-to-naive connect with a rebound
// local port, re-fetching the profile from s.config still shows the
// ORIGINAL LocalPort.
func TestFallbackToNaive_DoesNotMutateConfigStoreProfile(t *testing.T) {
	const origNaiveLocalPort = 51821
	profile := naiveFallbackProfile(origNaiveLocalPort)

	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{boundLocalPort: 61822} // different from origNaiveLocalPort
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloakMgr, naiveMgr, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	status := svc.Status(context.Background())
	if status.ActiveTransport != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive", status.ActiveTransport)
	}

	// Sanity check: the in-flight session's profile really did get rebound
	// to the fake's bound port (otherwise this test wouldn't be exercising
	// the aliasing path at all).
	active, ok := svc.getCurrentProfile()
	if !ok {
		t.Fatal("expected an active profile after connect")
	}
	if active.Naive == nil || active.Naive.LocalPort != 61822 {
		t.Fatalf("expected in-flight profile Naive.LocalPort rebound to 61822, got %+v", active.Naive)
	}

	// The regression check: the config store's own copy must be untouched.
	stored, found := svc.config.FindProfile("p1")
	if !found {
		t.Fatal("expected profile p1 to still be found in config store")
	}
	if stored.Naive == nil {
		t.Fatal("expected stored profile to still have a Naive config")
	}
	if stored.Naive.LocalPort != origNaiveLocalPort {
		t.Errorf("config store's Naive.LocalPort was mutated by fallbackToNaive: got %d, want original %d",
			stored.Naive.LocalPort, origNaiveLocalPort)
	}
}

// TestFallbackToNaive_StopsNaiveWhenStabilityCheckFails is a regression test
// for the leaked-naive-transport bug: when s.naive.Start succeeds but the
// transport never reports Running (waitForManagedTransportStable times
// out), fallbackToNaive must call s.naive.Stop before returning its error,
// so a failed connect attempt doesn't leave an orphaned naive process
// running in the background.
func TestFallbackToNaive_StopsNaiveWhenStabilityCheckFails(t *testing.T) {
	profile := naiveFallbackProfile(51821)

	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{stayDown: true}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloakMgr, naiveMgr, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail when naive never reports running after Start")
	}

	naiveMgr.mu.Lock()
	startCalled := naiveMgr.startCalled
	stopCalled := naiveMgr.stopCalled
	naiveMgr.mu.Unlock()

	if !startCalled {
		t.Fatal("expected naive.Start to have been called")
	}
	if !stopCalled {
		t.Error("expected naive.Stop to be called after naive failed to stabilize post-Start, to avoid leaking the naive transport")
	}
}

// TestFallbackToNaive_StopsNaiveWhenSessionWaitFails is a regression test
// for the same leaked-naive-transport bug, covering the other failure path:
// Start and the stability check both succeed, but WaitForSession fails.
// fallbackToNaive must still call s.naive.Stop before returning its error.
func TestFallbackToNaive_StopsNaiveWhenSessionWaitFails(t *testing.T) {
	profile := naiveFallbackProfile(51821)

	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{waitErr: errors.New("session handshake timed out")}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloakMgr, naiveMgr, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail when naive session wait fails")
	}

	naiveMgr.mu.Lock()
	startCalled := naiveMgr.startCalled
	stopCalled := naiveMgr.stopCalled
	naiveMgr.mu.Unlock()

	if !startCalled {
		t.Fatal("expected naive.Start to have been called")
	}
	if !stopCalled {
		t.Error("expected naive.Stop to be called after naive session wait failed, to avoid leaking the naive transport")
	}
}
