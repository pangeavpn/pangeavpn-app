package api

import (
	"context"
	"errors"
	"fmt"
	"net"
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

// fakeShadowsocksManager mirrors fakeHysteria2Manager minus WaitForSession —
// the real manager implements no SessionWaiter (see shadowsocks.Manager.Start).
type fakeShadowsocksManager struct {
	mu             sync.Mutex
	startCalled    bool
	startLocalPort int
	startErr       error
	stopCalled     bool
	running        bool
	stayDown       bool
	boundLocalPort int
}

func (f *fakeShadowsocksManager) Start(ctx context.Context, profile state.ShadowsocksProfile) error {
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

func (f *fakeShadowsocksManager) Stop(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopCalled = true
	f.running = false
	return nil
}

func (f *fakeShadowsocksManager) Status() state.TransportStatus {
	f.mu.Lock()
	defer f.mu.Unlock()
	return state.TransportStatus{Running: f.running}
}

func (f *fakeShadowsocksManager) BoundLocalPort() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.boundLocalPort != 0 {
		return f.boundLocalPort
	}
	return 51824
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

	// lastStartConfig is the config text of the most recent Start, for
	// asserting the peer Endpoint the tunnel was actually brought up against.
	lastStartConfig string

	// DNS guard modeling. The method below is what makes fakeWGManager satisfy
	// wgDNSGuard, so every connected health tick exercises the guard; the
	// defaults report a host still pointing at the tunnel's resolvers.
	dnsGuardCorrected bool
	dnsGuardErr       error
	dnsGuardCalls     int

	// Endpoint route guard modeling, the same shape as the DNS guard: the method
	// below is what makes fakeWGManager satisfy wgRouteGuard, and the defaults
	// report bypass routes still pointing where bring-up left them.
	routeGuardRepaired bool
	routeGuardErr      error
	routeGuardCalls    int

	// tunnelLUID is the adapter the kill switch should be scoped to; luidErr
	// models a manager that cannot report one.
	tunnelLUID uint64
	luidErr    error
}

// EnsureDNS models the host's interface DNS being checked, and corrected when
// something else has taken it over.
func (f *fakeWGManager) EnsureDNS(_ context.Context, _ state.WireGuardProfile) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dnsGuardCalls++
	return f.dnsGuardCorrected, f.dnsGuardErr
}

// EnsureEndpointRoutes models the tunnel's bypass routes being checked, and
// re-pinned when the host's default route has moved or dropped them.
func (f *fakeWGManager) EnsureEndpointRoutes(_ context.Context, _ state.WireGuardProfile) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.routeGuardCalls++
	return f.routeGuardRepaired, f.routeGuardErr
}

func (f *fakeWGManager) ActiveTunnelLUID(_ context.Context, _ state.WireGuardProfile) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.luidErr != nil {
		return 0, f.luidErr
	}
	return f.tunnelLUID, nil
}

func (f *fakeWGManager) Start(_ context.Context, profile state.WireGuardProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.startCount++
	f.lastStartConfig = profile.ConfigText
	if f.startErr != nil {
		return f.startErr
	}
	// A freshly created device has no prior handshake, so a stale one set by a
	// test does not survive a restart.
	f.handshakeUnix = 0
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

func (f *fakeTransportMemory) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.entries = map[string]string{}
	return nil
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
	enableLocked    bool
	updateTunnel    platform.TunnelRef
	enableCount     int
	updateCount     int
	clearCount      int
	enableErr       error
	updateErr       error
	clearErr        error
}

func (f *fakeKillSwitch) Enable(_ context.Context, endpoints []string, allowLAN bool, locked bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.enableCount++
	f.enableEndpoints = append(f.enableEndpoints[:0], endpoints...)
	f.enableAllowLAN = allowLAN
	f.enableLocked = locked
	if f.enableErr != nil {
		return f.enableErr
	}
	f.active = true
	return nil
}

func (f *fakeKillSwitch) Update(_ context.Context, tunnel platform.TunnelRef) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.updateCount++
	f.updateTunnel = tunnel
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
	return newTestServiceFull(t, cloak, naive, reality, &fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, ks, profiles...)
}

func newTestServiceFull(
	t *testing.T,
	cloak *fakeCloakManager,
	naive *fakeNaiveManager,
	reality *fakeRealityManager,
	hysteria2 *fakeHysteria2Manager,
	shadowsocks *fakeShadowsocksManager,
	snowflake *fakeSnowflakeManager,
	wgMgr *fakeWGManager,
	ks *fakeKillSwitch,
	profiles ...state.Profile,
) *Service {
	t.Helper()
	machine := state.NewMachine()
	logs := state.NewLogStore(100)
	config := testConfigStore(t, profiles...)
	svc := NewService(machine, logs, config, cloak, naive, reality, hysteria2, shadowsocks, snowflake, wgMgr, ks)
	// Keep handshake-gated failure paths fast in tests; a live fake tunnel
	// handshakes on the first status poll, so success paths are unaffected.
	svc.handshakeTimeout = 200 * time.Millisecond
	// Never let a unit test spawn the real netsh/powershell repair chain.
	svc.networkRepair = func(context.Context, []string) ([]string, error) { return nil, nil }
	// Pin the fingerprint so no test depends on the host machine's network.
	svc.networkKey = func() string { return "eth0:192.0.2.10" }
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
	if ks.updateTunnel.Name != profile.WireGuard.TunnelName {
		t.Errorf("expected update interface %q, got %q", profile.WireGuard.TunnelName, ks.updateTunnel.Name)
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
	if ks.updateTunnel.Name != wgMgr.interfaceName {
		t.Errorf("expected update interface %q, got %q", wgMgr.interfaceName, ks.updateTunnel.Name)
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

// TestConnect_KillSwitchIsScopedToTheReportedTunnelLUID proves the permit that
// lets application traffic through the lock is scoped to the device the manager
// created, not to a name resolved back through the OS. A rebuild recreates the
// adapter under the same name a second after destroying it, and a name lookup in
// that window can still return the dead one — leaving the permit on an interface
// that no longer exists and every application socket blocked.
func TestConnect_KillSwitchIsScopedToTheReportedTunnelLUID(t *testing.T) {
	profile := testProfile()
	wgMgr := &fakeWGManager{interfaceName: "PangeaVPN Tunnel", tunnelLUID: 1688849860263936}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.updateTunnel.WindowsLUID != wgMgr.tunnelLUID {
		t.Errorf("kill switch tunnel LUID = %d, want %d", ks.updateTunnel.WindowsLUID, wgMgr.tunnelLUID)
	}
	if ks.updateTunnel.Name != wgMgr.interfaceName {
		t.Errorf("kill switch tunnel name = %q, want %q", ks.updateTunnel.Name, wgMgr.interfaceName)
	}
}

// TestConnect_KillSwitchUpdateFailureFailsTheConnect proves a session whose
// permit never landed is not reported as connected. Without it the lock blocks
// every application socket while WireGuard — permitted separately by endpoint
// IP — goes on handshaking, so the tunnel looks healthy from every angle the
// daemon checks while nothing on the machine works.
func TestConnect_KillSwitchUpdateFailureFailsTheConnect(t *testing.T) {
	profile := testProfile()
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{updateErr: errors.New("permit tunnel interface: no such interface")}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err == nil {
		t.Fatal("expected connect to fail when the tunnel permit could not be added")
	}
	if st := svc.Status(context.Background()).State; st != state.StateError {
		t.Errorf("state = %q, want ERROR", st)
	}
	if !ks.Active() {
		t.Error("expected the kill switch to stay armed — a failed connect must leave the device fail-closed")
	}

	// Left running, the tunnel belongs to nobody: no profile was ever recorded
	// for the health loop to recover, and the next Connect is refused outright
	// by ensureNoRunningWireGuard.
	status, err := wgMgr.Status(context.Background(), profile.WireGuard)
	if err != nil {
		t.Fatalf("wireguard status failed: %v", err)
	}
	if status.Running {
		t.Error("expected the tunnel to be torn down after the permit could not be added")
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

func TestShutdown_RetainsKillSwitchWhenPersistedStateIsUnreadable(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	originalLoad := loadKillSwitchState
	loadKillSwitchState = func() (platform.KillSwitchState, error) {
		return platform.KillSwitchState{}, errors.New("state unavailable")
	}
	t.Cleanup(func() { loadKillSwitchState = originalLoad })

	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	if ks.clearCount != 0 {
		t.Fatal("shutdown cleared the kill switch when persisted state was unreadable")
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

// Under a lockdown lock the daemon cannot resolve a transport's remote host —
// the lock blocks DNS — so the client hands over the IPs it resolved while the
// network was open. Without them, only Cloak (whose endpoint is a raw IP) can
// be permitted and every DPI fallback is blocked by our own kill switch.
func TestKillSwitchPermits_IncludesResolvedTransportIPs(t *testing.T) {
	profile := testProfile()
	profile.Naive = &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"}
	profile.TransportEndpointIPs = []string{"203.0.113.40", "203.0.113.41"}

	permits := killSwitchPermits(profile)

	for _, want := range []string{"203.0.113.40", "203.0.113.41"} {
		if !slices.Contains(permits, want) {
			t.Errorf("killSwitchPermits() = %v, want to contain resolved transport IP %s — a hostname permit can't be resolved behind an engaged lock", permits, want)
		}
	}
}

// With the hub's addresses in hand the daemon must not ask a resolver where a
// node is: a system lookup goes out in cleartext and hands our node domains to
// the user's ISP.
func TestKillSwitchPermits_HubIPsReplaceNodeHostnames(t *testing.T) {
	profile := testProfile()
	profile.Cloak.RemoteHost = "192.0.2.50"
	profile.Naive = &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"}
	profile.Reality = &state.RealityProfile{RemoteHost: "reality.example.com", RemotePort: 443, UUID: "u", PublicKey: "k", ShortID: "s"}
	profile.TransportEndpointIPs = []string{"192.0.2.50"}

	permits := killSwitchPermits(profile)

	for _, host := range permits {
		if net.ParseIP(host) == nil {
			t.Errorf("killSwitchPermits() = %v, want IP literals only; %q would need a cleartext DNS lookup", permits, host)
		}
	}
}

func TestWithTransportBypassHosts_HubIPsReplaceNodeHostnames(t *testing.T) {
	profile := testProfile()
	profile.Cloak.RemoteHost = "192.0.2.50"
	profile.Hysteria2 = &state.Hysteria2Profile{RemoteHost: "hy2.example.com", RemotePort: 443, Password: "p", ObfsPassword: "o"}
	profile.TransportEndpointIPs = []string{"192.0.2.50"}

	bypass := withTransportBypassHosts(profile)

	if slices.Contains(bypass.BypassHosts, "hy2.example.com") {
		t.Errorf("withTransportBypassHosts().BypassHosts = %v, want no node hostname — routing it would need a cleartext lookup", bypass.BypassHosts)
	}
}

func TestWithTransportBypassHosts_IncludesResolvedTransportIPs(t *testing.T) {
	profile := testProfile()
	profile.Reality = &state.RealityProfile{RemoteHost: "reality.example.com", RemotePort: 443, UUID: "u", PublicKey: "k", ShortID: "s"}
	profile.TransportEndpointIPs = []string{"203.0.113.40"}

	bypass := withTransportBypassHosts(profile)

	if !slices.Contains(bypass.BypassHosts, "203.0.113.40") {
		t.Errorf("withTransportBypassHosts().BypassHosts = %v, want the resolved transport IP so its route is installed without a lookup", bypass.BypassHosts)
	}
}

func TestKillSwitchPermits_CloakOnlyProfileUnaffected(t *testing.T) {
	profile := testProfile()

	permits := killSwitchPermits(profile)

	if len(permits) != 1 || permits[0] != "vpn.example.com" {
		t.Errorf("killSwitchPermits() = %v, want exactly [vpn.example.com] for a Cloak-only profile", permits)
	}
}

// ---------------------------------------------------------------------------
// PermitHosts — control-plane hole in an engaged lockdown lock
// ---------------------------------------------------------------------------

// stubKillSwitchState points the persisted-state reader at a fixed value for
// the duration of the test. PermitHosts reuses the persisted AllowLAN/Locked
// flags so opening a hole never silently changes what kind of lock is engaged.
func stubKillSwitchState(t *testing.T, st platform.KillSwitchState) {
	t.Helper()
	original := loadKillSwitchState
	loadKillSwitchState = func() (platform.KillSwitchState, error) { return st, nil }
	t.Cleanup(func() { loadKillSwitchState = original })
}

func TestPermitHosts_AddsControlPlaneIPToEngagedLock(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubKillSwitchState(t, platform.KillSwitchState{
		Active:      true,
		AllowLAN:    true,
		EndpointIPs: []string{"198.51.100.9"},
		Locked:      true,
	})

	if err := svc.PermitHosts(context.Background(), []string{"203.0.113.7"}); err != nil {
		t.Fatalf("PermitHosts() error = %v", err)
	}

	want := []string{"198.51.100.9", "203.0.113.7"}
	if !slices.Equal(ks.enableEndpoints, want) {
		t.Errorf("Enable() endpoints = %v, want %v — the hub must be reachable through a lockdown lock or the app can never provision a profile", ks.enableEndpoints, want)
	}
	if !ks.enableAllowLAN {
		t.Error("Enable() allowLAN = false, want the persisted value (true) preserved")
	}
	if !ks.enableLocked {
		t.Error("Enable() locked = false, want the persisted lockdown flag preserved — otherwise the lock stops surviving daemon restarts")
	}
}

// Only IP literals: the lock blocks DNS, so a hostname could not be resolved
// while it is engaged, and resolving one is exactly what the lock prevents.
func TestPermitHosts_IgnoresHostnames(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{"198.51.100.9"}, Locked: true})

	err := svc.PermitHosts(context.Background(), []string{"hub.example.com"})

	if err == nil {
		t.Fatal("PermitHosts() error = nil, want an error when no permittable IP literal is known")
	}
	if ks.enableCount != 0 {
		t.Errorf("Enable() called %d times, want 0 — a hostname must never widen the lock", ks.enableCount)
	}
}

// The app only learns the hub's IP after it has talked to the hub, so on a cold
// start under lockdown it has none to send. The daemon falls back to the hub IP
// the last provisioned profile already carries in its WireGuard bypass hosts.
func TestPermitHosts_FallsBackToStoredBypassHosts(t *testing.T) {
	profile := testProfile()
	profile.Cloak.RemoteHost = "192.0.2.50"
	profile.WireGuard.BypassHosts = []string{"203.0.113.7"}
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true})

	if err := svc.PermitHosts(context.Background(), nil); err != nil {
		t.Fatalf("PermitHosts() error = %v", err)
	}

	if !slices.Equal(ks.enableEndpoints, []string{"203.0.113.7"}) {
		t.Errorf("Enable() endpoints = %v, want [203.0.113.7] from the stored profile's bypass hosts", ks.enableEndpoints)
	}
	if slices.Contains(ks.enableEndpoints, "192.0.2.50") {
		t.Error("Enable() permitted the VPN endpoint; the server's IP is only unblocked by Connect, not by a control-plane permit")
	}
}

func TestPermitHosts_NoopWhenNoLockEngaged(t *testing.T) {
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())

	if err := svc.PermitHosts(context.Background(), []string{"203.0.113.7"}); err != nil {
		t.Fatalf("PermitHosts() error = %v", err)
	}
	if ks.enableCount != 0 {
		t.Errorf("Enable() called %d times, want 0 — with no lock engaged there is nothing to open", ks.enableCount)
	}
}

func TestPermitHosts_SkipsWhenAlreadyPermitted(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{"203.0.113.7"}, Locked: true})

	if err := svc.PermitHosts(context.Background(), []string{"203.0.113.7"}); err != nil {
		t.Fatalf("PermitHosts() error = %v", err)
	}
	if ks.enableCount != 0 {
		t.Errorf("Enable() called %d times, want 0 — the IP is already permitted", ks.enableCount)
	}
}

// Engaging a lock must not cut the app off from the hub: the server list, the
// subscription state and provisioning all run through it, and none of them can
// re-resolve anything while the lock blocks DNS.
func TestEngageKillSwitch_PermitsStoredHubIPOnly(t *testing.T) {
	profile := testProfile()
	profile.Cloak.RemoteHost = "192.0.2.50"
	profile.WireGuard.BypassHosts = []string{"203.0.113.7"}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)

	if err := svc.EngageKillSwitch(context.Background(), profile.ID, false); err != nil {
		t.Fatalf("EngageKillSwitch() error = %v", err)
	}

	if !slices.Equal(ks.enableEndpoints, []string{"203.0.113.7"}) {
		t.Errorf("Enable() endpoints = %v, want just the hub IP [203.0.113.7]", ks.enableEndpoints)
	}
	if slices.Contains(ks.enableEndpoints, "192.0.2.50") {
		t.Error("Enable() permitted the VPN endpoint at engage time; a server's IP is only unblocked once that server is selected to connect")
	}
	if !ks.enableLocked {
		t.Error("Enable() locked = false, want a lock that survives daemon restarts")
	}
}

// Nothing provisioned yet means no hub IP is known — the lock still has to
// engage, as a pure block-all.
func TestEngageKillSwitch_BlocksAllWhenNoHubIPKnown(t *testing.T) {
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks)

	if err := svc.EngageKillSwitch(context.Background(), "", false); err != nil {
		t.Fatalf("EngageKillSwitch() error = %v", err)
	}

	if ks.enableCount != 1 {
		t.Fatalf("Enable() called %d times, want 1", ks.enableCount)
	}
	if len(ks.enableEndpoints) != 0 {
		t.Errorf("Enable() endpoints = %v, want none", ks.enableEndpoints)
	}
}

// A lock persisted by an older daemon carries no hub permit. Restarting into it
// must not leave the app unable to reach the hub — that is the state a stuck
// user upgrades from.
func TestReconcileStartup_ReAppliedLockdownLockRegainsHubPermit(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.BypassHosts = []string{"203.0.113.7"}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true, EndpointIPs: nil})

	svc.reconcileStartup(context.Background())

	if !slices.Contains(ks.enableEndpoints, "203.0.113.7") {
		t.Errorf("Enable() endpoints = %v, want the hub IP re-permitted on restart", ks.enableEndpoints)
	}
	if !ks.enableLocked {
		t.Error("Enable() locked = false, want the lockdown lock re-applied as an intentional lock")
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

// silentTunnelProfile builds a cloak + naive profile for the silent-tunnel
// health-check tests.
func silentTunnelProfile() state.Profile {
	return state.Profile{
		ID:    "p1",
		Name:  "p1",
		Cloak: state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Naive: &state.NaiveProfile{RemoteHost: "example.com", RemotePort: 443, Username: "u", Password: "p"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
}

// goSilent makes the tunnel look silent: the newest handshake is well past the
// stale window, as after a suspend/resume that killed the transport's session.
func goSilent(wgMgr *fakeWGManager) {
	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	wgMgr.handshakeUnix = time.Now().Add(-(wireGuardHandshakeStaleAfter + time.Minute)).Unix()
}

// TestHealthCheck_SilentTunnelRebuildsSession proves the ongoing health check
// recovers a tunnel that goes silent mid-session: the transport's session is
// dead even though the transport and interface both still report running, so
// only a full transport restart brings traffic back.
func TestHealthCheck_SilentTunnelRebuildsSession(t *testing.T) {
	profile := silentTunnelProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	goSilent(wgMgr)
	svc.runHealthCheck(context.Background())

	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED after the silent tunnel was rebuilt", status.State)
	}
	if status.ActiveTransport != "naive" {
		t.Errorf("ActiveTransport = %q, want naive (the rebuild reuses the session's transport)", status.ActiveTransport)
	}
	naive.mu.Lock()
	naiveStopped := naive.stopCalled
	naive.mu.Unlock()
	if !naiveStopped {
		t.Error("expected the transport to be stopped during the rebuild; a restart is the only thing that re-dials its session")
	}
	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 1 {
		t.Errorf("wireguard restarts during rebuild = %d, want 1", restarts)
	}
	if !ks.Active() {
		t.Error("expected the kill switch to stay armed across the rebuild — the device must be fail-closed while the tunnel is down")
	}
	if ks.clearCount != 0 {
		t.Errorf("kill switch clear count = %d, want 0 across a rebuild", ks.clearCount)
	}
}

// TestHealthCheck_SilentTunnelRebuildFailureMarksError proves a silent tunnel
// that cannot be rebuilt still surfaces as an error rather than sitting there
// reporting connected.
func TestHealthCheck_SilentTunnelRebuildFailureMarksError(t *testing.T) {
	profile := silentTunnelProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// The transport now refuses to come back — e.g. its server is unreachable
	// on the network the host woke up on.
	naive.mu.Lock()
	naive.startErr = errors.New("boom")
	naive.mu.Unlock()

	goSilent(wgMgr)
	svc.runHealthCheck(context.Background())

	if st := svc.Status(context.Background()).State; st != state.StateError {
		t.Fatalf("state = %q, want ERROR after a failed rebuild", st)
	}
	if !ks.Active() {
		t.Error("expected the kill switch to stay armed after a failed rebuild")
	}
}

// recoveryTestService is a connected naive session wired for deterministic
// recovery: no backoff to wait out and a network that always looks usable.
func recoveryTestService(t *testing.T) (*Service, *fakeNaiveManager, *fakeWGManager, *fakeKillSwitch) {
	t.Helper()
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, naive, wgMgr, ks, silentTunnelProfile())
	svc.recoveryDelays = []time.Duration{0}
	svc.networkKey = func() string { return "eth0:192.0.2.10" }

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	return svc, naive, wgMgr, ks
}

// dropSession makes the tunnel go silent with a transport that refuses to come
// back — the laptop that woke up before its network did.
func dropSession(naive *fakeNaiveManager, wgMgr *fakeWGManager) {
	naive.mu.Lock()
	naive.startErr = errors.New("dial udp 192.0.2.1:8488: connect: network is unreachable")
	naive.mu.Unlock()
	goSilent(wgMgr)
}

// restoreNetwork lets the transport dial again and forgets the previous start
// so the next one is observable.
func restoreNetwork(naive *fakeNaiveManager) {
	naive.mu.Lock()
	naive.startErr = nil
	naive.startCalled = false
	naive.mu.Unlock()
}

func transportStarted(naive *fakeNaiveManager) bool {
	naive.mu.Lock()
	defer naive.mu.Unlock()
	return naive.startCalled
}

// TestHealthCheck_RetriesAfterFailedRebuild is the laptop-wake case. The first
// rebuild lands before the host has a route and fails, whichever transport it
// is dialling; the session still has to come back on its own once the network
// does. Before this the health check stopped looking at StateError, so that one
// failure was terminal — with the kill switch left armed, i.e. no network at
// all until the user noticed and clicked something.
func TestHealthCheck_RetriesAfterFailedRebuild(t *testing.T) {
	svc, naive, wgMgr, ks := recoveryTestService(t)

	dropSession(naive, wgMgr)
	svc.runHealthCheck(context.Background())

	status := svc.Status(context.Background())
	if status.State != state.StateError {
		t.Fatalf("state = %q, want ERROR after the first rebuild failed", status.State)
	}
	if !strings.Contains(status.Detail, "retrying in") {
		t.Errorf("detail = %q, want it to say another attempt is coming", status.Detail)
	}
	if !status.Reconnecting {
		t.Error("Reconnecting = false, want the error reported as one the daemon is still working on")
	}

	restoreNetwork(naive)
	svc.runHealthCheck(context.Background())

	status = svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q (%s), want CONNECTED after the retry", status.State, status.Detail)
	}
	if status.Reconnecting {
		t.Error("Reconnecting = true after the session came back")
	}
	if !transportStarted(naive) {
		t.Error("expected the retry to restart the transport")
	}
	if !ks.Active() {
		t.Error("expected the kill switch to stay armed across the failed rebuild and the retry")
	}
}

// TestHealthCheck_RecoveryWaitsOutTheBackoff proves the retries are paced. An
// unreachable server would otherwise be re-dialled on every 3s tick forever.
func TestHealthCheck_RecoveryWaitsOutTheBackoff(t *testing.T) {
	svc, naive, wgMgr, _ := recoveryTestService(t)
	svc.recoveryDelays = []time.Duration{time.Hour}

	dropSession(naive, wgMgr)
	svc.runHealthCheck(context.Background())

	restoreNetwork(naive)
	svc.runHealthCheck(context.Background())

	if transportStarted(naive) {
		t.Error("expected no second attempt while the backoff is still running")
	}
	if st := svc.Status(context.Background()).State; st != state.StateError {
		t.Fatalf("state = %q, want ERROR while waiting for the next attempt", st)
	}
}

// TestHealthCheck_RecoveryWaitsForUsableNetwork proves a host with no address
// to dial from is waited on rather than retried against: burning attempts (and
// backoff) while the WiFi is still re-associating only delays the reconnect.
func TestHealthCheck_RecoveryWaitsForUsableNetwork(t *testing.T) {
	svc, naive, wgMgr, _ := recoveryTestService(t)

	var networkKeyMu sync.Mutex
	networkKey := ""
	svc.networkKey = func() string {
		networkKeyMu.Lock()
		defer networkKeyMu.Unlock()
		return networkKey
	}

	dropSession(naive, wgMgr)
	svc.runHealthCheck(context.Background())

	restoreNetwork(naive)
	svc.runHealthCheck(context.Background())
	if transportStarted(naive) {
		t.Fatal("expected no reconnect attempt while the host has no usable network")
	}

	networkKeyMu.Lock()
	networkKey = "wlan0:192.0.2.55"
	networkKeyMu.Unlock()

	svc.runHealthCheck(context.Background())
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED once the network is back", st)
	}
}

// TestDisconnect_DoesNotBlockOnNetworkRepair proves the post-disconnect route
// repair cannot hold the user in DISCONNECTING; it runs after the state flip.
func TestDisconnect_DoesNotBlockOnNetworkRepair(t *testing.T) {
	profile := testProfile()
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)

	started := make(chan []string, 1)
	release := make(chan struct{})
	svc.networkRepair = func(ctx context.Context, tunnelNames []string) ([]string, error) {
		started <- tunnelNames
		<-release
		return []string{"repaired"}, nil
	}

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- svc.Disconnect(context.Background(), false) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("disconnect failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("disconnect blocked on the network repair")
	}

	if st, _ := svc.machine.Get(); st != state.StateDisconnected {
		t.Fatalf("state = %q, want DISCONNECTED while repair is still running", st)
	}
	select {
	case names := <-started:
		if len(names) == 0 || names[0] != profile.WireGuard.TunnelName {
			t.Fatalf("repair got tunnel names %v, want [%s]", names, profile.WireGuard.TunnelName)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("network repair was never started")
	}
	close(release)
}

// TestConnect_CancelsPendingNetworkRepair proves a reconnect cannot race a
// still-running repair that would renew adapters under the new tunnel.
func TestConnect_CancelsPendingNetworkRepair(t *testing.T) {
	profile := testProfile()
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)

	cancelled := make(chan struct{})
	svc.networkRepair = func(ctx context.Context, tunnelNames []string) ([]string, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}
	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}

	select {
	case <-cancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("reconnect did not cancel the pending network repair")
	}
}

// TestConnect_TunnelPermittedBeforeDataPathProbe proves the kill switch opens
// for the tunnel adapter before the bring-up probe, or the probe hits the lock.
func TestConnect_TunnelPermittedBeforeDataPathProbe(t *testing.T) {
	profile := testProfile()
	// The gate only probes when the session has resolvers to ask.
	profile.WireGuard.ConfigText = strings.Replace(profile.WireGuard.ConfigText, "[Interface]\n", "[Interface]\nDNS = 10.0.0.53\n", 1)
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)

	probed := make(chan int, 1)
	svc.probeResolver = func(context.Context, string, string) error {
		ks.mu.Lock()
		updates := ks.updateCount
		ks.mu.Unlock()
		probed <- updates
		return nil
	}

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	select {
	case updates := <-probed:
		if updates == 0 {
			t.Fatal("data-path probe ran before the kill switch permitted the tunnel")
		}
	default:
		t.Fatal("data-path probe never ran")
	}
}

// TestStatus_ReportsTransportBeingTriedWhileConnecting proves /status names
// the candidate under trial mid-connect, and clears it once one wins.
func TestStatus_ReportsTransportBeingTriedWhileConnecting(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.ConfigText = strings.Replace(profile.WireGuard.ConfigText, "[Interface]\n", "[Interface]\nDNS = 10.0.0.53\n", 1)
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)

	seen := make(chan string, 1)
	svc.probeResolver = func(context.Context, string, string) error {
		seen <- svc.Status(context.Background()).ConnectingTransport
		return nil
	}

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	select {
	case kind := <-seen:
		if kind != "cloak" {
			t.Errorf("mid-connect ConnectingTransport = %q, want %q", kind, "cloak")
		}
	default:
		t.Fatal("data-path probe never ran")
	}
	status := svc.Status(context.Background())
	if status.ConnectingTransport != "" {
		t.Errorf("post-connect ConnectingTransport = %q, want empty", status.ConnectingTransport)
	}
	if status.ActiveTransport != "cloak" {
		t.Errorf("post-connect ActiveTransport = %q, want %q", status.ActiveTransport, "cloak")
	}
}

// TestConnect_FailedReconnectRestartsNetworkRepair proves a reconnect that
// cancels the repair but then fails cannot strand a broken host network.
func TestConnect_FailedReconnectRestartsNetworkRepair(t *testing.T) {
	profile := testProfile()
	wgMgr := &fakeWGManager{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, &fakeKillSwitch{}, profile)

	var calls int
	var callsMu sync.Mutex
	restarted := make(chan struct{}, 1)
	svc.networkRepair = func(ctx context.Context, tunnelNames []string) ([]string, error) {
		callsMu.Lock()
		calls++
		first := calls == 1
		callsMu.Unlock()
		if first {
			<-ctx.Done()
			return nil, ctx.Err()
		}
		restarted <- struct{}{}
		return nil, nil
	}

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect failed: %v", err)
	}

	wgMgr.mu.Lock()
	wgMgr.startErr = errors.New("wg refuses to start")
	wgMgr.mu.Unlock()
	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err == nil {
		t.Fatal("expected reconnect to fail")
	}

	select {
	case <-restarted:
	case <-time.After(3 * time.Second):
		t.Fatal("failed reconnect did not restart the cancelled network repair")
	}
}

// TestHealthCheck_DisconnectEndsRecovery proves the retry loop is the user's to
// stop: a session they tore down themselves must never be dialled again.
func TestHealthCheck_DisconnectEndsRecovery(t *testing.T) {
	svc, naive, wgMgr, _ := recoveryTestService(t)

	dropSession(naive, wgMgr)
	svc.runHealthCheck(context.Background())

	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect after a failed rebuild: %v", err)
	}

	restoreNetwork(naive)
	svc.runHealthCheck(context.Background())

	if transportStarted(naive) {
		t.Error("expected no reconnect attempt after the user disconnected")
	}
	if st := svc.Status(context.Background()).State; st != state.StateDisconnected {
		t.Fatalf("state = %q, want DISCONNECTED to stay put", st)
	}
}

// TestHealthCheck_RecoveryLeavesALiveTunnelAlone proves recovery only rebuilds
// broken sessions. A refused Connect stamps an error on a session that is still
// carrying traffic; tearing that down to "recover" it would be the bug.
func TestHealthCheck_RecoveryLeavesALiveTunnelAlone(t *testing.T) {
	svc, naive, wgMgr, _ := recoveryTestService(t)

	wgMgr.mu.Lock()
	stopsBefore := wgMgr.stopCount
	wgMgr.mu.Unlock()

	svc.setError("profile p2 is active; disconnect before connecting profile p1")
	restoreNetwork(naive)
	svc.runHealthCheck(context.Background())

	wgMgr.mu.Lock()
	stopsAfter := wgMgr.stopCount
	wgMgr.mu.Unlock()
	if stopsAfter != stopsBefore {
		t.Errorf("wireguard was torn down (%d -> %d stops) for a session that was never broken", stopsBefore, stopsAfter)
	}
	if transportStarted(naive) {
		t.Error("expected no transport restart while the tunnel is still handshaking")
	}
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED restored on a live tunnel", st)
	}
}

// TestHealthCheck_HeldWhileTheHostResumes proves the grace period after a
// detected resume: the tunnel is stale because the machine was asleep, and
// rebuilding into a network that has not come back yet just wastes the session.
func TestHealthCheck_HeldWhileTheHostResumes(t *testing.T) {
	svc, _, wgMgr, _ := recoveryTestService(t)

	wgMgr.mu.Lock()
	stopsBefore := wgMgr.stopCount
	wgMgr.mu.Unlock()

	goSilent(wgMgr)
	svc.holdHealthChecks(time.Minute)
	svc.runHealthCheck(context.Background())

	wgMgr.mu.Lock()
	stopsAfter := wgMgr.stopCount
	wgMgr.mu.Unlock()
	if stopsAfter != stopsBefore {
		t.Errorf("wireguard was torn down (%d -> %d stops) during the resume grace period", stopsBefore, stopsAfter)
	}
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED while health checks are held", st)
	}
}

// TestHealthCheck_SilentTunnelWaitsForNetwork proves a silent tunnel is left
// alone while the host has no network to rebuild on (mid-resume, AP change):
// tearing it down there burns the cascade and reports exhaustion for nothing.
func TestHealthCheck_SilentTunnelWaitsForNetwork(t *testing.T) {
	svc, naive, wgMgr, _ := recoveryTestService(t)
	svc.networkKey = func() string { return "" }

	wgMgr.mu.Lock()
	stopsBefore := wgMgr.stopCount
	wgMgr.mu.Unlock()

	goSilent(wgMgr)
	restoreNetwork(naive)
	svc.runHealthCheck(context.Background())

	wgMgr.mu.Lock()
	stopsAfter := wgMgr.stopCount
	wgMgr.mu.Unlock()
	if stopsAfter != stopsBefore {
		t.Errorf("wireguard was torn down (%d -> %d stops) while the host had no network", stopsBefore, stopsAfter)
	}
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED while waiting for the network to come back", st)
	}

	svc.networkKey = func() string { return "eth0:192.0.2.10" }
	svc.runHealthCheck(context.Background())
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED after the rebuild once the network is back", st)
	}
	if !transportStarted(naive) {
		t.Error("expected the rebuild to run once the network came back")
	}
}

// TestHealthCheck_SilentTunnelRebuildRepointsEndpoint proves the rebuilt tunnel
// is aimed at the port the *new* bridge bound. The live profile's LocalPort was
// mutated to the previous session's bound port, so rebuilding from it would
// leave WireGuard talking to a port nothing listens on any more.
func TestHealthCheck_SilentTunnelRebuildRepointsEndpoint(t *testing.T) {
	profile := silentTunnelProfile()
	profile.Naive.LocalPort = 51821
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{boundLocalPort: 61235}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	// The second bridge binds the same ephemeral port as the first — the case
	// that catches a rebuild seeded from the mutated live profile, because then
	// bound == configured and the rebind sees nothing to rewrite.
	goSilent(wgMgr)
	svc.runHealthCheck(context.Background())

	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED after rebuild", st)
	}
	wgMgr.mu.Lock()
	config := wgMgr.lastStartConfig
	wgMgr.mu.Unlock()
	if !strings.Contains(config, "Endpoint=127.0.0.1:61235") {
		t.Errorf("rebuilt wireguard config = %q, want the bridge port 61235", config)
	}
}

// TestHealthCheck_SilentTunnelRebuildKeepsAllowLAN proves the rebuild restores
// the session the user asked for: AllowLAN shapes both the WireGuard
// AllowedIPs and the kill switch, so losing it would silently tighten the
// tunnel on recovery.
func TestHealthCheck_SilentTunnelRebuildKeepsAllowLAN(t *testing.T) {
	profile := silentTunnelProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, cloak, naive, wgMgr, ks, profile)

	opts := ConnectOptions{PreferredTransport: "naive", AllowLAN: true, Lockdown: true}
	if err := svc.Connect(context.Background(), "p1", opts); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	goSilent(wgMgr)
	svc.runHealthCheck(context.Background())

	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED after rebuild", st)
	}
	if got := svc.getSessionOpts(); got != opts {
		t.Errorf("session options after rebuild = %+v, want %+v", got, opts)
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
// attempted before the rest of the cascade: naive is remembered and works, so
// neither reality (normally first) nor cloak is ever started.
func TestConnect_TriesRememberedTransportFirst(t *testing.T) {
	profile := transportMemoryProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	reality := &fakeRealityManager{}
	wgMgr := &fakeWGManager{} // live tunnel: whichever transport is tried first handshakes
	ks := &fakeKillSwitch{}
	mem := &fakeTransportMemory{entries: map[string]string{"wifi-home": "naive"}}
	svc := newTestServiceWithReality(t, cloak, naive, reality, wgMgr, ks, profile)
	svc.transportMemory = mem
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if svc.Status(context.Background()).ActiveTransport != "naive" {
		t.Fatalf("ActiveTransport = %q, want naive (remembered)", svc.Status(context.Background()).ActiveTransport)
	}
	reality.mu.Lock()
	realityStarted := reality.startCalled
	reality.mu.Unlock()
	if realityStarted || cloak.startCalled {
		t.Error("no cascade transport should start when the remembered transport works first")
	}
	naive.mu.Lock()
	naiveStarted := naive.startCalled
	naive.mu.Unlock()
	if !naiveStarted {
		t.Error("naive (remembered) should have been tried first")
	}
}

// TestConnect_RememberedTransportFailsNow_FallsBackAndRerecords proves a stale
// memory doesn't strand the connection: naive is remembered but no longer
// handshakes, so the daemon falls through the rest of the cascade and updates
// the memory to the new winner.
func TestConnect_RememberedTransportFailsNow_FallsBackAndRerecords(t *testing.T) {
	profile := transportMemoryProfile()
	cloak := &fakeCloakManager{}
	naive := &fakeNaiveManager{}
	reality := &fakeRealityManager{}
	// Reordered cascade is [naive, reality, cloak]; only the 2nd WG bring-up
	// (reality) handshakes, so naive fails and reality becomes the new winner.
	wgMgr := &fakeWGManager{handshakeOnStart: 2}
	ks := &fakeKillSwitch{}
	mem := &fakeTransportMemory{entries: map[string]string{"wifi-home": "naive"}}
	svc := newTestServiceWithReality(t, cloak, naive, reality, wgMgr, ks, profile)
	svc.transportMemory = mem
	svc.networkKey = func() string { return "wifi-home" }

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	active := svc.Status(context.Background()).ActiveTransport
	if active == "naive" {
		t.Fatal("expected to fall back off the stale remembered transport")
	}
	if active != "reality" {
		t.Fatalf("ActiveTransport = %q, want reality (next in reordered cascade)", active)
	}
	if got, _ := mem.Lookup("wifi-home"); got != "reality" {
		t.Fatalf("remembered transport = %q, want reality after re-record", got)
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

func TestConnect_RealityFails_FallsBackToCloakBeforeNaive(t *testing.T) {
	// REALITY leads the censorship-resistance cascade and Cloak backs it up, so
	// a reality failure falls through to cloak and naive is never reached.
	profile := realityProfile()
	profile.Naive = &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"}

	cloakMgr := &fakeCloakManager{}
	naiveMgr := &fakeNaiveManager{}
	realityMgr := &fakeRealityManager{startErr: errors.New("reality boom")}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceWithReality(t, cloakMgr, naiveMgr, realityMgr, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	realityMgr.mu.Lock()
	realityStarted := realityMgr.startCalled
	realityMgr.mu.Unlock()
	if !realityStarted {
		t.Fatal("expected reality.Start to be attempted first")
	}
	status := svc.Status(context.Background())
	if status.ActiveTransport != "cloak" {
		t.Fatalf("ActiveTransport = %q, want cloak", status.ActiveTransport)
	}
	if !cloakMgr.startCalled {
		t.Fatal("expected cloak.Start to be attempted after reality failed")
	}
	naiveMgr.mu.Lock()
	naiveStarted := naiveMgr.startCalled
	naiveMgr.mu.Unlock()
	if naiveStarted {
		t.Fatal("naive should not be reached — cloak precedes it in the cascade")
	}
}

// TestConnect_AutoCascadeAttemptsTransportsInCensorshipOrder pins the auto-mode
// order: reality, cloak, shadowsocks, hysteria2, then naive (snowflake is gated
// off). With every transport configured but none able to handshake, the
// aggregated failure records the order they were attempted in.
func TestConnect_AutoCascadeAttemptsTransportsInCensorshipOrder(t *testing.T) {
	profile := state.Profile{
		ID:          "p1",
		Name:        "p1",
		Cloak:       state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Naive:       &state.NaiveProfile{RemoteHost: "n.example.com", RemotePort: 443, Username: "u", Password: "p"},
		Reality:     &state.RealityProfile{RemoteHost: "r.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"},
		Hysteria2:   &state.Hysteria2Profile{RemoteHost: "h.example.com", RemotePort: 8443, Password: "p", ObfsPassword: "o"},
		Shadowsocks: &state.ShadowsocksProfile{RemoteHost: "s.example.com", RemotePort: 8488, Password: "p"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
	wgMgr := &fakeWGManager{noHandshake: true} // every transport starts but no tunnel handshakes
	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{}, &fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, ks, profile)

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})
	if err == nil {
		t.Fatal("expected connect to fail when nothing handshakes")
	}
	if !errors.Is(err, ErrTransportExhausted) {
		t.Fatalf("Connect error = %v, want ErrTransportExhausted", err)
	}
	msg := err.Error()
	lastIdx := -1
	for _, kind := range []string{"reality", "cloak", "shadowsocks", "hysteria2", "naive"} {
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

func TestConnect_ExplicitTransportFailureIsNotAutoExhaustion(t *testing.T) {
	profile := testProfile()
	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	svc := newTestService(t, cloakMgr, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "cloak"})
	if err == nil {
		t.Fatal("expected explicit cloak connect to fail")
	}
	if errors.Is(err, ErrTransportExhausted) {
		t.Fatalf("explicit transport error must not be retryable across servers: %v", err)
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
	svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hysteria2Mgr, &fakeShadowsocksManager{}, snowflakeMgr, wgMgr, ks, profile)

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
	svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hysteria2Mgr, &fakeShadowsocksManager{}, snowflakeMgr, wgMgr, ks, profile)

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
			svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hysteria2Mgr, &fakeShadowsocksManager{}, snowflakeMgr, wgMgr, ks, tt.profile)

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

func shadowsocksProfile() state.Profile {
	return state.Profile{
		ID:          "p1",
		Name:        "p1",
		Cloak:       state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Shadowsocks: &state.ShadowsocksProfile{RemoteHost: "ss.example.com", RemotePort: 8488, Method: "chacha20-ietf-poly1305", Password: "p"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
}

func TestConnect_AllOthersFail_FallsBackToShadowsocks(t *testing.T) {
	profile := shadowsocksProfile()
	profile.Naive = &state.NaiveProfile{RemoteHost: "naive.example.com", RemotePort: 8443, Username: "u", Password: "p"}
	profile.Reality = &state.RealityProfile{RemoteHost: "reality.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"}
	profile.Hysteria2 = &state.Hysteria2Profile{RemoteHost: "hysteria2.example.com", RemotePort: 8443, Password: "p", ObfsPassword: "o"}

	cloakMgr := &fakeCloakManager{startErr: errors.New("cloak boom")}
	naiveMgr := &fakeNaiveManager{startErr: errors.New("naive boom")}
	realityMgr := &fakeRealityManager{startErr: errors.New("reality boom")}
	hysteria2Mgr := &fakeHysteria2Manager{startErr: errors.New("hysteria2 boom")}
	shadowsocksMgr := &fakeShadowsocksManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hysteria2Mgr, shadowsocksMgr, &fakeSnowflakeManager{}, wgMgr, ks, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	shadowsocksMgr.mu.Lock()
	startCalled := shadowsocksMgr.startCalled
	shadowsocksMgr.mu.Unlock()
	if !startCalled {
		t.Fatal("expected shadowsocks.Start after cloak and reality failed")
	}

	status := svc.Status(context.Background())
	if status.ActiveTransport != "shadowsocks" {
		t.Fatalf("ActiveTransport = %q, want shadowsocks", status.ActiveTransport)
	}
	if !status.Shadowsocks.Running {
		t.Fatal("Status().Shadowsocks.Running = false, want true")
	}
}

// Shadowsocks must never pre-empt the transports ahead of it: with cloak
// healthy the cascade stops there and shadowsocks is never touched.
func TestConnect_AutoMode_DoesNotReachShadowsocksWhenCloakWorks(t *testing.T) {
	profile := shadowsocksProfile()
	shadowsocksMgr := &fakeShadowsocksManager{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		&fakeHysteria2Manager{}, shadowsocksMgr, &fakeSnowflakeManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	shadowsocksMgr.mu.Lock()
	startCalled := shadowsocksMgr.startCalled
	shadowsocksMgr.mu.Unlock()
	if startCalled {
		t.Fatal("shadowsocks must sit behind cloak in the cascade, not run alongside it")
	}
}

func TestConnect_PreferredTransportShadowsocks_WithoutConfigErrors(t *testing.T) {
	profile := testProfile()
	shadowsocksMgr := &fakeShadowsocksManager{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		&fakeHysteria2Manager{}, shadowsocksMgr, &fakeSnowflakeManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)

	err := svc.Connect(context.Background(), profile.ID, ConnectOptions{PreferredTransport: "shadowsocks"})
	if err == nil {
		t.Fatal("expected Connect to fail: profile has no shadowsocks configuration")
	}
	if !strings.Contains(err.Error(), "no shadowsocks configuration") {
		t.Fatalf("Connect error = %v, want it to name the missing shadowsocks configuration", err)
	}
	shadowsocksMgr.mu.Lock()
	startCalled := shadowsocksMgr.startCalled
	shadowsocksMgr.mu.Unlock()
	if startCalled {
		t.Fatal("shadowsocks.Start must not run without a profile")
	}
}

// The health check restarts a stopped shadowsocks rather than falling through
// to recoverActiveTransport's unknown-kind error arm.
func TestRecoverActiveTransport_RestartsShadowsocks(t *testing.T) {
	profile := shadowsocksProfile()
	shadowsocksMgr := &fakeShadowsocksManager{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		&fakeHysteria2Manager{}, shadowsocksMgr, &fakeSnowflakeManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "shadowsocks"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	shadowsocksMgr.mu.Lock()
	shadowsocksMgr.running = false
	shadowsocksMgr.startCalled = false
	shadowsocksMgr.mu.Unlock()

	live, _ := svc.getCurrentProfile()
	if err := svc.recoverActiveTransport(context.Background(), live, "shadowsocks"); err != nil {
		t.Fatalf("recoverActiveTransport: %v", err)
	}
	shadowsocksMgr.mu.Lock()
	restarted := shadowsocksMgr.startCalled
	shadowsocksMgr.mu.Unlock()
	if !restarted {
		t.Fatal("expected shadowsocks.Start on recovery")
	}
}

func TestKillSwitchPermits_IncludesShadowsocksHostWhenPresent(t *testing.T) {
	profile := shadowsocksProfile()
	permits := killSwitchPermits(profile)
	if !slices.Contains(permits, "ss.example.com") {
		t.Errorf("killSwitchPermits() = %v, want to contain shadowsocks host ss.example.com", permits)
	}
}

func TestWithTransportBypassHosts_IncludesShadowsocksHostWhenPresent(t *testing.T) {
	profile := shadowsocksProfile()
	wgProfile := withTransportBypassHosts(profile)
	if !slices.Contains(wgProfile.BypassHosts, "ss.example.com") {
		t.Errorf("withTransportBypassHosts().BypassHosts = %v, want to contain ss.example.com", wgProfile.BypassHosts)
	}
}

// Hub-supplied TransportEndpointIPs short-circuit transportPermitHosts, so the
// shadowsocks hostname must not leak into the permit list alongside them.
func TestTransportPermitHosts_HubIPsSupersedeShadowsocksHostname(t *testing.T) {
	profile := shadowsocksProfile()
	profile.TransportEndpointIPs = []string{"203.0.113.7"}
	permits := transportPermitHosts(profile)
	if !slices.Contains(permits, "203.0.113.7") {
		t.Errorf("transportPermitHosts() = %v, want to contain the hub-supplied IP", permits)
	}
	if slices.Contains(permits, "ss.example.com") {
		t.Errorf("transportPermitHosts() = %v, must not resolve a node hostname when the hub sent IPs", permits)
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
