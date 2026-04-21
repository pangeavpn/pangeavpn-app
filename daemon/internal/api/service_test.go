package api

import (
	"context"
	"errors"
	"reflect"
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
	mu              sync.Mutex
	running         bool
	startErr        error
	stopErr         error
	waitErr         error
	startCount      int
	stopCount       int
	startLocalPort  int
	boundLocalPort  int
}

func (f *fakeCloakManager) Start(_ context.Context, profile state.CloakProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
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
	enableEndpoint  string
	enableAllowLAN  bool
	updateInterface string
	enableCount     int
	updateCount     int
	clearCount      int
	enableErr       error
	updateErr       error
	clearErr        error
}

func (f *fakeKillSwitch) Enable(_ context.Context, endpoint string, allowLAN bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enableCount++
	f.enableEndpoint = endpoint
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
	wgMgr *fakeWGManager,
	ks *fakeKillSwitch,
	profiles ...state.Profile,
) *Service {
	t.Helper()
	machine := state.NewMachine()
	logs := state.NewLogStore(100)
	config := testConfigStore(t, profiles...)
	return NewService(machine, logs, config, cloak, wgMgr, ks)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestConnect_KillSwitchEnabledBeforeCloakAndWG(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{})
	if err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.enableCount != 1 {
		t.Errorf("expected enable called once, got %d", ks.enableCount)
	}
	if ks.enableEndpoint != profile.Cloak.RemoteHost {
		t.Errorf("expected enable endpoint %q, got %q", profile.Cloak.RemoteHost, ks.enableEndpoint)
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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

	// Connect first.
	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// Disconnect should clear the kill switch.
	if err := svc.Disconnect(context.Background()); err != nil {
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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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

	if err := svc.Disconnect(context.Background()); err != nil {
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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

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

func TestTunnelNamesForCleanup_UsesProfileTunnelNames(t *testing.T) {
	cfg := state.Config{
		Profiles: []state.Profile{
			{
				ID:   "profile-one",
				Name: "Profile One",
				WireGuard: state.WireGuardProfile{
					TunnelName: "wg-alpha",
				},
			},
			{
				ID:   "profile-two",
				Name: "Profile Two",
				WireGuard: state.WireGuardProfile{
					TunnelName: "actual-tunnel-name",
				},
			},
			{
				ID:   "profile-three",
				Name: "Profile Three",
				WireGuard: state.WireGuardProfile{
					TunnelName: "WG-ALPHA",
				},
			},
		},
	}

	current := state.Profile{
		ID:   "selected-profile-id",
		Name: "Selected Profile",
		WireGuard: state.WireGuardProfile{
			TunnelName: "current-tunnel",
		},
	}

	got := tunnelNamesForCleanup(cfg, current)
	want := []string{"current-tunnel", "wg-alpha", "actual-tunnel-name"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("tunnelNamesForCleanup() = %v, want %v", got, want)
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
	svc := newTestService(t, cloak, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID); err != nil {
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
