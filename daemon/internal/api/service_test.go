package api

import (
	"context"
	"errors"
	"fmt"
	"slices"
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
	mu             sync.Mutex
	startCalled    bool
	startLocalPort int
	startErr       error
	stopCalled     bool
	running        bool

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
	f.startLocalPort = profile.LocalPort
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

// fakeRealityManager mirrors fakeNaiveManager's shape for VLESS+REALITY.
type fakeRealityManager struct {
	mu             sync.Mutex
	startCalled    bool
	startLocalPort int
	startErr       error
	stopCalled     bool
	running        bool
	stayDown       bool
	waitErr        error
	boundLocalPort int
}

func (f *fakeRealityManager) Start(ctx context.Context, profile state.RealityProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalled = true
	f.startLocalPort = profile.LocalPort
	if f.startErr != nil {
		return f.startErr
	}
	if !f.stayDown {
		f.running = true
	}
	return nil
}

func (f *fakeRealityManager) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalled = true
	f.running = false
	return nil
}

func (f *fakeRealityManager) Status() state.TransportStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return state.TransportStatus{Running: f.running}
}

func (f *fakeRealityManager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

func (f *fakeRealityManager) BoundLocalPort() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.boundLocalPort != 0 {
		return f.boundLocalPort
	}
	return 51823
}

// fakeHysteria2Manager mirrors fakeNaiveManager; see its fields for docs.
type fakeHysteria2Manager struct {
	mu             sync.Mutex
	startCalled    bool
	startLocalPort int
	startErr       error
	stopCalled     bool
	running        bool
	stayDown       bool
	waitErr        error
	boundLocalPort int
}

func (f *fakeHysteria2Manager) Start(ctx context.Context, profile state.Hysteria2Profile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalled = true
	f.startLocalPort = profile.LocalPort
	if f.startErr != nil {
		return f.startErr
	}
	if !f.stayDown {
		f.running = true
	}
	return nil
}

func (f *fakeHysteria2Manager) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalled = true
	f.running = false
	return nil
}

func (f *fakeHysteria2Manager) Status() state.TransportStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return state.TransportStatus{Running: f.running}
}

func (f *fakeHysteria2Manager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

func (f *fakeHysteria2Manager) BoundLocalPort() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.boundLocalPort != 0 {
		return f.boundLocalPort
	}
	return 51823
}

// fakeSnowflakeManager mirrors fakeNaiveManager; see its fields for docs.
type fakeSnowflakeManager struct {
	mu             sync.Mutex
	startCalled    bool
	startLocalPort int
	startErr       error
	stopCalled     bool
	running        bool
	stayDown       bool
	waitErr        error
	boundLocalPort int
}

func (f *fakeSnowflakeManager) Start(ctx context.Context, profile state.SnowflakeProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCalled = true
	f.startLocalPort = profile.LocalPort
	if f.startErr != nil {
		return f.startErr
	}
	if !f.stayDown {
		f.running = true
	}
	return nil
}

func (f *fakeSnowflakeManager) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalled = true
	f.running = false
	return nil
}

func (f *fakeSnowflakeManager) Status() state.TransportStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return state.TransportStatus{Running: f.running}
}

func (f *fakeSnowflakeManager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.waitErr
}

func (f *fakeSnowflakeManager) BoundLocalPort() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.boundLocalPort != 0 {
		return f.boundLocalPort
	}
	return 51824
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

	// Handshake modeling for the connection-readiness gate. Default (all zero,
	// noHandshake false) = a live tunnel that handshakes as soon as it is
	// running. noHandshake = a dead tunnel that never handshakes. handshakeOnStart
	// = only handshake once startCount reaches it (to model a transport whose
	// server is dead so an earlier candidate fails and a later one succeeds).
	// handshakeUnix overrides the reported handshake time (e.g. a stale one).
	noHandshake      bool
	handshakeOnStart int
	handshakeUnix    int64
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
	return state.WireGuardStatus{Running: f.running, Detail: "fake", LastHandshakeUnix: f.lastHandshakeLocked()}, nil
}

// lastHandshakeLocked returns the handshake time the fake should report; caller
// holds f.mu.
func (f *fakeWGManager) lastHandshakeLocked() int64 {
	if f.handshakeUnix != 0 {
		return f.handshakeUnix
	}
	if !f.running || f.noHandshake {
		return 0
	}
	if f.handshakeOnStart > 0 && f.startCount < f.handshakeOnStart {
		return 0
	}
	return time.Now().Unix()
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

// fakeTransportMemory is an in-memory transportMemory for tests.
type fakeTransportMemory struct {
	mu      sync.Mutex
	entries map[string]string
}

func (f *fakeTransportMemory) Lookup(networkKey string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	transport, ok := f.entries[networkKey]
	return transport, ok && transport != ""
}

func (f *fakeTransportMemory) Record(networkKey, transport string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.entries == nil {
		f.entries = map[string]string{}
	}
	f.entries[networkKey] = transport
	return nil
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

// newTestService keeps its original signature (cloak + naive only) so the
// existing call sites below don't need touching; it wires in a no-op
// fakeRealityManager. Tests that specifically exercise reality behavior use
// newTestServiceWithReality instead.
func newTestService(
	t *testing.T,
	cloak *fakeCloakManager,
	naive *fakeNaiveManager,
	wgMgr *fakeWGManager,
	ks *fakeKillSwitch,
	profiles ...state.Profile,
) *Service {
	t.Helper()
	return newTestServiceWithReality(t, cloak, naive, &fakeRealityManager{}, wgMgr, ks, profiles...)
}

func newTestServiceWithReality(
	t *testing.T,
	cloak *fakeCloakManager,
	naive *fakeNaiveManager,
	reality *fakeRealityManager,
	wgMgr *fakeWGManager,
	ks *fakeKillSwitch,
	profiles ...state.Profile,
) *Service {
	t.Helper()
	return newTestServiceFull(t, cloak, naive, reality, &fakeHysteria2Manager{}, &fakeSnowflakeManager{}, wgMgr, ks, profiles...)
}

func newTestServiceFull(
	t *testing.T,
	cloak *fakeCloakManager,
	naive *fakeNaiveManager,
	reality *fakeRealityManager,
	hysteria2 *fakeHysteria2Manager,
	snowflake *fakeSnowflakeManager,
	wgMgr *fakeWGManager,
	ks *fakeKillSwitch,
	profiles ...state.Profile,
) *Service {
	t.Helper()
	machine := state.NewMachine()
	logs := state.NewLogStore(100)
	config := testConfigStore(t, profiles...)
	svc := NewService(machine, logs, config, cloak, naive, reality, hysteria2, snowflake, wgMgr, ks)
	// Keep handshake-gated failure paths fast in tests; a live fake tunnel
	// handshakes on the first status poll, so success paths are unaffected.
	svc.handshakeTimeout = 200 * time.Millisecond
	return svc
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

func TestKillSwitchPermits_IncludesNaiveHostWhenPresent(t *testing.T) {
	profile := testProfile()
	profile.Naive = &state.NaiveProfile{
		RemoteHost: "naive.example.com",
		RemotePort: 8443,
		Username:   "u",
		Password:   "p",
	}

	permits := killSwitchPermits(profile)

	if !slices.Contains(permits, "vpn.example.com") {
		t.Errorf("killSwitchPermits() = %v, want to contain cloak host vpn.example.com", permits)
	}
	if !slices.Contains(permits, "naive.example.com") {
		t.Errorf("killSwitchPermits() = %v, want to contain naive host naive.example.com — otherwise the kill switch (armed before Connect knows which transport will succeed) blocks the Naive fallback's own connection attempt", permits)
	}
}

func TestKillSwitchPermits_CloakOnlyProfileUnaffected(t *testing.T) {
	profile := testProfile()

	permits := killSwitchPermits(profile)

	if len(permits) != 1 || permits[0] != "vpn.example.com" {
		t.Errorf("killSwitchPermits() = %v, want exactly [vpn.example.com] for a Cloak-only profile", permits)
	}
}

func TestWithTransportBypassHosts_IncludesNaiveHostWhenPresent(t *testing.T) {
	profile := testProfile()
	profile.Naive = &state.NaiveProfile{
		RemoteHost: "naive.example.com",
		RemotePort: 8443,
		Username:   "u",
		Password:   "p",
	}

	wgProfile := withTransportBypassHosts(profile)

	if !slices.Contains(wgProfile.BypassHosts, "vpn.example.com") {
		t.Errorf("BypassHosts = %v, want to contain cloak host vpn.example.com", wgProfile.BypassHosts)
	}
	if !slices.Contains(wgProfile.BypassHosts, "naive.example.com") {
		t.Errorf("BypassHosts = %v, want to contain naive host naive.example.com", wgProfile.BypassHosts)
	}
}

func TestWithTransportBypassHosts_CloakOnlyProfileUnaffected(t *testing.T) {
	profile := testProfile()

	wgProfile := withTransportBypassHosts(profile)

	if len(wgProfile.BypassHosts) != 1 || wgProfile.BypassHosts[0] != "vpn.example.com" {
		t.Errorf("BypassHosts = %v, want exactly [vpn.example.com] for a Cloak-only profile", wgProfile.BypassHosts)
	}
}

func TestKillSwitchPermitsAndBypassHosts_SkipEmptyNaiveRemoteHost(t *testing.T) {
	profile := testProfile()
	profile.Naive = &state.NaiveProfile{
		RemoteHost: "   ", // whitespace-only, same as "unset" per strings.TrimSpace
		RemotePort: 8443,
		Username:   "u",
		Password:   "p",
	}

	permits := killSwitchPermits(profile)
	if len(permits) != 1 || permits[0] != "vpn.example.com" {
		t.Errorf("killSwitchPermits() = %v, want exactly [vpn.example.com] when Naive.RemoteHost is blank", permits)
	}

	wgProfile := withTransportBypassHosts(profile)
	if len(wgProfile.BypassHosts) != 1 || wgProfile.BypassHosts[0] != "vpn.example.com" {
		t.Errorf("BypassHosts = %v, want exactly [vpn.example.com] when Naive.RemoteHost is blank", wgProfile.BypassHosts)
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

func TestConnect_UsesEphemeralNaivePortAndRewritesEndpoint(t *testing.T) {
	profile := testProfile()
	profile.Naive = &state.NaiveProfile{
		RemoteHost: "naive.example.com",
		RemotePort: 8443,
		LocalPort:  51823,
		Username:   "u",
		Password:   "p",
	}
	profile.WireGuard.ConfigText = "[Interface]\nPrivateKey = YWJjZGVmZw==\n\n[Peer]\nPublicKey = eHl6MTIzNDU=\nEndpoint = 127.0.0.1:51823\nAllowedIPs = 0.0.0.0/0\n"

	cloak := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naive := &fakeNaiveManager{boundLocalPort: 61235}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	naive.mu.Lock()
	if naive.startLocalPort != 0 {
		t.Errorf("naive.Start should receive LocalPort=0 to request ephemeral (same Windows Hyper-V exclusion-range reasoning as Cloak); got %d", naive.startLocalPort)
	}
	naive.mu.Unlock()

	stored, ok := svc.getCurrentProfile()
	if !ok {
		t.Fatal("expected current profile to be set after connect")
	}
	if stored.Naive == nil {
		t.Fatal("expected stored profile to carry Naive config")
	}
	if stored.Naive.LocalPort != 61235 {
		t.Errorf("stored profile Naive.LocalPort = %d, want 61235 (bound port)", stored.Naive.LocalPort)
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

func TestConnect_PreferredTransportCloak_DoesNotFallBackOnFailure(t *testing.T) {
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

	err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "cloak"})
	if err == nil {
		t.Fatal("expected Connect to fail — cloak failed and cloak-only mode should not fall back")
	}
	if naiveMgr.startCalled {
		t.Fatal("naive.Start should not be called when PreferredTransport is cloak")
	}
}

func TestConnect_PreferredTransportNaive_SkipsCloakEntirely(t *testing.T) {
	profile := state.Profile{
		ID:    "p1",
		Name:  "p1",
		Cloak: state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Naive: &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"},
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

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if cloakMgr.startCalled {
		t.Fatal("cloak.Start should not be called when PreferredTransport is naive")
	}
	if !naiveMgr.startCalled {
		t.Fatal("expected naive.Start to be called")
	}
	status := svc.Status(context.Background())
	if status.ActiveTransport != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive", status.ActiveTransport)
	}
}

func TestConnect_PreferredTransportNaive_ErrorsWhenProfileHasNoNaiveConfig(t *testing.T) {
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

	err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"})
	if err == nil {
		t.Fatal("expected Connect to fail — profile has no naive configuration")
	}
	if cloakMgr.startCalled || naiveMgr.startCalled {
		t.Fatal("neither transport should be started when naive is requested but unconfigured")
	}
}

// TestConnect_NoWireGuardHandshake_DoesNotConnect proves the readiness gate:
// a transport whose process starts but whose tunnel never handshakes does not
// count as connected, and WireGuard is torn back down.
func TestConnect_NoWireGuardHandshake_DoesNotConnect(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{noHandshake: true}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "cloak"})
	if err == nil {
		t.Fatal("expected connect to fail when WireGuard never handshakes")
	}
	if st := svc.Status(context.Background()).State; st == state.StateConnected {
		t.Fatalf("state = %q, want not CONNECTED", st)
	}
	wgMgr.mu.Lock()
	running := wgMgr.running
	wgMgr.mu.Unlock()
	if running {
		t.Error("expected WireGuard to be stopped after handshake failure")
	}
}

// TestConnect_FallsBackWhenTransportStartsButHandshakeNeverCompletes proves the
// handshake is the fallback trigger: cloak starts cleanly but no handshake ever
// lands through it, so the daemon advances to naive, which does handshake.
func TestConnect_FallsBackWhenTransportStartsButHandshakeNeverCompletes(t *testing.T) {
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
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	// Handshake only completes once the second transport (naive) brings WG up.
	wgMgr := &fakeWGManager{handshakeOnStart: 2}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED", status.State)
	}
	if status.ActiveTransport != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive (cloak started but carried no handshake)", status.ActiveTransport)
	}
	if cloak.stopCount == 0 {
		t.Error("expected cloak to be stopped after its tunnel failed to handshake")
	}
	if !naive.startCalled {
		t.Error("expected naive to be attempted after cloak")
	}
}

// TestConnect_ConnectedReportsWireGuardHandshake proves the successful path
// surfaces the handshake time in status.
func TestConnect_ConnectedReportsWireGuardHandshake(t *testing.T) {
	profile := testProfile()
	cloak := &fakeCloakManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, &fakeNaiveManager{}, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED", status.State)
	}
	if status.WireGuard.LastHandshakeUnix <= 0 {
		t.Fatal("expected a non-zero WireGuard handshake time once connected")
	}
}

// TestHealthCheck_StaleWireGuardHandshakeMarksError proves the ongoing health
// check flags a tunnel that goes silent mid-session (no handshake within the
// stale window) even though the interface and transport still report running.
func TestHealthCheck_StaleWireGuardHandshakeMarksError(t *testing.T) {
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
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// Tunnel goes silent: the newest handshake is well past the stale window.
	wgMgr.mu.Lock()
	wgMgr.handshakeUnix = time.Now().Add(-(wireGuardHandshakeStaleAfter + time.Minute)).Unix()
	wgMgr.mu.Unlock()

	svc.runHealthCheck(context.Background())

	if st := svc.Status(context.Background()).State; st != state.StateError {
		t.Fatalf("state = %q, want ERROR after stale handshake", st)
	}
}

// transportMemoryProfile builds a profile with cloak + naive + reality all
// configured, for the per-network transport-memory tests.
func transportMemoryProfile() state.Profile {
	return state.Profile{
		ID:      "p1",
		Name:    "p1",
		Cloak:   state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Naive:   &state.NaiveProfile{RemoteHost: "example.com", RemotePort: 443, Username: "u", Password: "p"},
		Reality: &state.RealityProfile{RemoteHost: "reality.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
}

// TestConnect_RecordsLastGoodTransportForNetwork proves the daemon remembers
// which transport actually established a tunnel, keyed by network: cloak fails
// to handshake, naive succeeds, so naive is recorded for this network.
func TestConnect_RecordsLastGoodTransportForNetwork(t *testing.T) {
	// cloak + naive only, so the second transport attempted is deterministically
	// naive regardless of the wider cascade order.
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
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	// Handshake only completes on the second transport's WG bring-up (naive).
	wgMgr := &fakeWGManager{handshakeOnStart: 2}
	ks := &fakeKillSwitch{}
	mem := &fakeTransportMemory{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)
	svc.transportMemory = mem
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if got := svc.Status(context.Background()).ActiveTransport; got != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive", got)
	}
	if got, _ := mem.Lookup("wifi-home"); got != "naive" {
		t.Fatalf("remembered transport = %q, want naive", got)
	}
}

// TestConnect_TriesRememberedTransportFirst proves the remembered transport is
// attempted before the rest of the cascade: reality is remembered and works, so
// cloak (normally first) is never even started.
func TestConnect_TriesRememberedTransportFirst(t *testing.T) {
	profile := transportMemoryProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	reality := &fakeRealityManager{}
	wgMgr := &fakeWGManager{} // live tunnel: whichever transport is tried first handshakes
	ks := &fakeKillSwitch{}
	mem := &fakeTransportMemory{entries: map[string]string{"wifi-home": "reality"}}
	svc := newTestServiceWithReality(t, cloak, naive, reality, wgMgr, ks, profile)
	svc.transportMemory = mem
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if svc.Status(context.Background()).ActiveTransport != "reality" {
		t.Fatalf("ActiveTransport = %q, want reality (remembered)", svc.Status(context.Background()).ActiveTransport)
	}
	if cloak.startCalled {
		t.Error("cloak should not be started when the remembered transport works first")
	}
	if !reality.startCalled {
		t.Error("reality (remembered) should have been tried first")
	}
}

// TestConnect_RememberedTransportFailsNow_FallsBackAndRerecords proves a stale
// memory doesn't strand the connection: reality is remembered but no longer
// handshakes, so the daemon falls through the rest of the cascade and updates
// the memory to the new winner.
func TestConnect_RememberedTransportFailsNow_FallsBackAndRerecords(t *testing.T) {
	profile := transportMemoryProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	reality := &fakeRealityManager{}
	// Reordered cascade is [reality, cloak, naive]; only the 2nd WG bring-up
	// (cloak) handshakes, so reality fails and cloak becomes the new winner.
	wgMgr := &fakeWGManager{handshakeOnStart: 2}
	ks := &fakeKillSwitch{}
	mem := &fakeTransportMemory{entries: map[string]string{"wifi-home": "reality"}}
	svc := newTestServiceWithReality(t, cloak, naive, reality, wgMgr, ks, profile)
	svc.transportMemory = mem
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	active := svc.Status(context.Background()).ActiveTransport
	if active == "reality" {
		t.Fatal("expected to fall back off the stale remembered transport")
	}
	if active != "cloak" {
		t.Fatalf("ActiveTransport = %q, want cloak (next in reordered cascade)", active)
	}
	if got, _ := mem.Lookup("wifi-home"); got != "cloak" {
		t.Fatalf("remembered transport = %q, want cloak after re-record", got)
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

// ---------------------------------------------------------------------------
// Reality tests
// ---------------------------------------------------------------------------

func realityProfile() state.Profile {
	return state.Profile{
		ID:      "p1",
		Name:    "p1",
		Cloak:   state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Reality: &state.RealityProfile{RemoteHost: "reality.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
}

func TestConnect_PreferredTransportReality_SkipsCloakAndNaive(t *testing.T) {
	profile := realityProfile()

	cloakMgr := &fakeCloakManager{}
	naiveMgr := &fakeNaiveManager{}
	realityMgr := &fakeRealityManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceWithReality(t, cloakMgr, naiveMgr, realityMgr, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "reality"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if cloakMgr.startCalled {
		t.Fatal("cloak.Start should not be called when PreferredTransport is reality")
	}
	if naiveMgr.startCalled {
		t.Fatal("naive.Start should not be called when PreferredTransport is reality")
	}
	realityMgr.mu.Lock()
	startCalled := realityMgr.startCalled
	realityMgr.mu.Unlock()
	if !startCalled {
		t.Fatal("expected reality.Start to be called")
	}

	status := svc.Status(context.Background())
	if status.ActiveTransport != "reality" {
		t.Fatalf("ActiveTransport = %q, want reality", status.ActiveTransport)
	}
}

func TestConnect_PreferredTransportReality_ErrorsWhenProfileHasNoRealityConfig(t *testing.T) {
	profile := testProfile() // no Reality configured

	cloakMgr := &fakeCloakManager{}
	naiveMgr := &fakeNaiveManager{}
	realityMgr := &fakeRealityManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceWithReality(t, cloakMgr, naiveMgr, realityMgr, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "reality"})
	if err == nil {
		t.Fatal("expected Connect to fail: profile has no reality configuration")
	}
	realityMgr.mu.Lock()
	startCalled := realityMgr.startCalled
	realityMgr.mu.Unlock()
	if startCalled {
		t.Fatal("reality.Start should not be called when profile.Reality is nil")
	}
}

func TestConnect_CloakFails_FallsBackToRealityBeforeNaive(t *testing.T) {
	// In the censorship-resistance cascade REALITY precedes NaiveProxy, so a
	// cloak failure falls through to reality and naive is never reached.
	profile := realityProfile()
	profile.Naive = &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"}

	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{}
	realityMgr := &fakeRealityManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceWithReality(t, cloakMgr, naiveMgr, realityMgr, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if !cloakMgr.startCalled {
		t.Fatal("expected cloak.Start to be attempted first")
	}
	status := svc.Status(context.Background())
	if status.ActiveTransport != "reality" {
		t.Fatalf("ActiveTransport = %q, want reality", status.ActiveTransport)
	}
	realityMgr.mu.Lock()
	realityStarted := realityMgr.startCalled
	realityMgr.mu.Unlock()
	if !realityStarted {
		t.Fatal("expected reality.Start to be attempted after cloak failed")
	}
	naiveMgr.mu.Lock()
	naiveStarted := naiveMgr.startCalled
	naiveMgr.mu.Unlock()
	if naiveStarted {
		t.Fatal("naive should not be reached — reality precedes it in the cascade")
	}
}

// TestConnect_AutoCascadeAttemptsTransportsInCensorshipOrder pins the auto-mode
// order: cloak, then reality, then hysteria2, then naive (snowflake is gated
// off). With every transport configured but none able to handshake, the
// aggregated failure records the order they were attempted in.
func TestConnect_AutoCascadeAttemptsTransportsInCensorshipOrder(t *testing.T) {
	profile := state.Profile{
		ID:        "p1",
		Name:      "p1",
		Cloak:     state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Naive:     &state.NaiveProfile{RemoteHost: "n.example.com", RemotePort: 443, Username: "u", Password: "p"},
		Reality:   &state.RealityProfile{RemoteHost: "r.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"},
		Hysteria2: &state.Hysteria2Profile{RemoteHost: "h.example.com", RemotePort: 8443, Password: "p", ObfsPassword: "o"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
	wgMgr := &fakeWGManager{noHandshake: true} // every transport starts but no tunnel handshakes
	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{}, &fakeHysteria2Manager{}, &fakeSnowflakeManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail when nothing handshakes")
	}
	msg := err.Error()
	lastIdx := -1
	for _, kind := range []string{"cloak", "reality", "hysteria2", "naive"} {
		idx := strings.Index(msg, kind+":")
		if idx < 0 {
			t.Fatalf("error missing %s attempt: %v", kind, msg)
		}
		if idx < lastIdx {
			t.Fatalf("%s attempted out of cascade order in: %v", kind, msg)
		}
		lastIdx = idx
	}
	if strings.Contains(msg, "snowflake:") {
		t.Fatalf("snowflake is gated and must not be attempted in auto mode: %v", msg)
	}
}

func hysteria2Profile() state.Profile {
	return state.Profile{
		ID:        "p1",
		Name:      "p1",
		Cloak:     state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Hysteria2: &state.Hysteria2Profile{RemoteHost: "hysteria2.example.com", RemotePort: 8443, Password: "p", ObfsPassword: "o"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
}

func TestConnect_CloakNaiveRealityFail_FallsBackToHysteria2(t *testing.T) {
	profile := hysteria2Profile()
	profile.Naive = &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"}
	profile.Reality = &state.RealityProfile{RemoteHost: "reality.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"}

	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{startErr: errors.New("naive boom")}
	realityMgr := &fakeRealityManager{startErr: errors.New("reality boom")}
	hysteria2Mgr := &fakeHysteria2Manager{}
	snowflakeMgr := &fakeSnowflakeManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hysteria2Mgr, snowflakeMgr, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	hysteria2Mgr.mu.Lock()
	startCalled := hysteria2Mgr.startCalled
	hysteria2Mgr.mu.Unlock()
	if !startCalled {
		t.Fatal("expected hysteria2.Start to be attempted after cloak, naive, and reality failed")
	}

	status := svc.Status(context.Background())
	if status.ActiveTransport != "hysteria2" {
		t.Fatalf("ActiveTransport = %q, want hysteria2", status.ActiveTransport)
	}
}

func snowflakeProfile() state.Profile {
	return state.Profile{
		ID:    "p1",
		Name:  "p1",
		Cloak: state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Snowflake: &state.SnowflakeProfile{
			BrokerURL:         "https://broker.example.com/",
			BridgeFingerprint: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA",
		},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
}

func TestConnect_CloakNaiveRealityHysteria2Fail_DoesNotFallBackToSnowflake_WhenGated(t *testing.T) {
	profile := snowflakeProfile()
	profile.Naive = &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"}
	profile.Reality = &state.RealityProfile{RemoteHost: "reality.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"}
	profile.Hysteria2 = &state.Hysteria2Profile{RemoteHost: "hysteria2.example.com", RemotePort: 8443, Password: "p", ObfsPassword: "o"}

	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{startErr: errors.New("naive boom")}
	realityMgr := &fakeRealityManager{startErr: errors.New("reality boom")}
	hysteria2Mgr := &fakeHysteria2Manager{startErr: errors.New("hysteria2 boom")}
	snowflakeMgr := &fakeSnowflakeManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hysteria2Mgr, snowflakeMgr, wgMgr, ks, profile)

	// Snowflake is gated off this release (see snowflakeReleaseGated in
	// service.go): AUTO mode must fail once the other transports fail rather
	// than fall back to snowflake.
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err == nil {
		t.Fatal("expected Connect to fail: all non-snowflake transports failed and snowflake is gated")
	}

	snowflakeMgr.mu.Lock()
	startCalled := snowflakeMgr.startCalled
	snowflakeMgr.mu.Unlock()
	if startCalled {
		t.Fatal("snowflake.Start must not be called while snowflake is gated off")
	}

	if status := svc.Status(context.Background()); status.ActiveTransport == "snowflake" {
		t.Fatalf("ActiveTransport = %q, want anything but snowflake", status.ActiveTransport)
	}
}

// TestConnect_PreferredTransportSnowflake_IsGated also covers the former
// no-snowflake-config case: the gate precedes the config check, so an explicit
// preferredTransport="snowflake" errors and starts no transport whether or not
// the profile carries snowflake config.
func TestConnect_PreferredTransportSnowflake_IsGated(t *testing.T) {
	tests := []struct {
		name    string
		profile state.Profile
	}{
		{name: "with snowflake config", profile: snowflakeProfile()},
		{name: "without snowflake config", profile: testProfile()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cloakMgr := &fakeCloakManager{}
			naiveMgr := &fakeNaiveManager{}
			realityMgr := &fakeRealityManager{}
			hysteria2Mgr := &fakeHysteria2Manager{}
			snowflakeMgr := &fakeSnowflakeManager{}
			wgMgr := &fakeWGManager{}
			ks := &fakeKillSwitch{}
			svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hysteria2Mgr, snowflakeMgr, wgMgr, ks, tt.profile)

			if err := svc.Connect(context.Background(), tt.profile.ID, ConnectOptions{PreferredTransport: "snowflake"}); err == nil {
				t.Fatal("expected Connect to fail: snowflake transport is gated off")
			}

			snowflakeMgr.mu.Lock()
			snowflakeStarted := snowflakeMgr.startCalled
			snowflakeMgr.mu.Unlock()
			if cloakMgr.startCalled || naiveMgr.startCalled || realityMgr.startCalled || hysteria2Mgr.startCalled || snowflakeStarted {
				t.Fatal("no transport should start when snowflake is gated and explicitly requested")
			}

			if status := svc.Status(context.Background()); status.ActiveTransport == "snowflake" {
				t.Fatalf("ActiveTransport = %q, want anything but snowflake", status.ActiveTransport)
			}
		})
	}
}

func TestKillSwitchPermits_IncludesRealityHostWhenPresent(t *testing.T) {
	profile := realityProfile()
	permits := killSwitchPermits(profile)
	if !slices.Contains(permits, "reality.example.com") {
		t.Errorf("killSwitchPermits() = %v, want to contain reality host reality.example.com", permits)
	}
}

func TestWithTransportBypassHosts_IncludesRealityHostWhenPresent(t *testing.T) {
	profile := realityProfile()
	wgProfile := withTransportBypassHosts(profile)
	if !slices.Contains(wgProfile.BypassHosts, "reality.example.com") {
		t.Errorf("withTransportBypassHosts().BypassHosts = %v, want to contain reality.example.com", wgProfile.BypassHosts)
	}
}

func snowflakeProfileWithFronting() state.Profile {
	profile := snowflakeProfile()
	profile.Snowflake.FrontDomains = []string{"front.example.com"}
	profile.Snowflake.AmpCacheURL = "https://amp.example.com/cache"
	profile.Snowflake.ICEServers = []string{"stun:stun.example.com:3478"}
	return profile
}

func TestKillSwitchPermits_IncludesSnowflakeHostsWhenPresent(t *testing.T) {
	profile := snowflakeProfileWithFronting()
	permits := killSwitchPermits(profile)
	for _, want := range []string{"broker.example.com", "front.example.com", "amp.example.com", "stun.example.com"} {
		if !slices.Contains(permits, want) {
			t.Errorf("killSwitchPermits() = %v, want to contain %s", permits, want)
		}
	}
}

func TestWithTransportBypassHosts_IncludesSnowflakeHostsWhenPresent(t *testing.T) {
	profile := snowflakeProfileWithFronting()
	wgProfile := withTransportBypassHosts(profile)
	for _, want := range []string{"broker.example.com", "front.example.com", "amp.example.com", "stun.example.com"} {
		if !slices.Contains(wgProfile.BypassHosts, want) {
			t.Errorf("withTransportBypassHosts().BypassHosts = %v, want to contain %s", wgProfile.BypassHosts, want)
		}
	}
}
