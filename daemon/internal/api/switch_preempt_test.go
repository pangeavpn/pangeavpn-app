package api

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func newInPlaceTestService(t *testing.T, wgMgr *fakeInPlaceWGManager, ks *fakeKillSwitch, profiles ...state.Profile) *Service {
	t.Helper()
	svc := NewService(state.NewMachine(), state.NewLogStore(100), testConfigStore(t, profiles...), &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{}, &fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, ks)
	stubSessionRecordStore(t)
	svc.handshakeTimeout = 200 * time.Millisecond
	svc.networkRepair = func(context.Context, []string) ([]string, error) { return nil, nil }
	svc.networkKey = func() string { return "eth0:192.0.2.10" }
	svc.hostInternet = func() (bool, bool) { return false, false }
	return svc
}

// blockFirstStart parks the fake's next Start until its context ends; later
// starts behave normally. The channel closes once the parked start is reached.
func blockFirstStart(mgr *fakeCloakManager) <-chan struct{} {
	entered := make(chan struct{})
	var taken atomic.Bool
	mgr.mu.Lock()
	mgr.startHook = func(ctx context.Context) error {
		if !taken.CompareAndSwap(false, true) {
			return nil
		}
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	mgr.mu.Unlock()
	return entered
}

func awaitOrFail(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// A switch provisions through the hub while the old tunnel still owns routing;
// with the hub inside that tunnel, a dead tunnel stranded every switch.
func TestPermitHosts_RoutesHubAroundLiveTunnel(t *testing.T) {
	a, _ := switchProfilePair()
	a.WireGuard.BypassHosts = []string{"203.0.113.7"}
	a.WireGuard.HubInTunnel = true
	wgMgr := &fakeInPlaceWGManager{}
	ks := &fakeKillSwitch{}
	svc := newInPlaceTestService(t, wgMgr, ks, a)

	if err := svc.Connect(context.Background(), a.ID, ConnectOptions{PreferredTransport: "cloak"}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{"10.0.0.1", "203.0.113.7"}})

	if err := svc.PermitHosts(context.Background(), []string{"203.0.113.7"}); err != nil {
		t.Fatalf("PermitHosts: %v", err)
	}

	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	if len(wgMgr.pinnedHosts) != 1 || !slices.Equal(wgMgr.pinnedHosts[0], []string{"203.0.113.7"}) {
		t.Fatalf("pinned routes = %v, want exactly one pin for the hub IP", wgMgr.pinnedHosts)
	}
	if ks.enableCount != 1 {
		t.Errorf("Enable() called %d times, want 1 — the route must be pinned even when the permit already exists", ks.enableCount)
	}
}

func TestPermitHosts_NoRouteWithoutSession(t *testing.T) {
	a := testProfile()
	a.WireGuard.BypassHosts = []string{"203.0.113.7"}
	wgMgr := &fakeInPlaceWGManager{}
	ks := &fakeKillSwitch{active: true}
	svc := newInPlaceTestService(t, wgMgr, ks, a)
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true})

	if err := svc.PermitHosts(context.Background(), []string{"203.0.113.7"}); err != nil {
		t.Fatalf("PermitHosts: %v", err)
	}
	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	if wgMgr.pinCount != 0 {
		t.Errorf("PinEndpointRoutes called %d times with no session, want 0", wgMgr.pinCount)
	}
}

// A failed switch used to leave recovery due on the next tick, and that rebuild
// then held opMu for a whole cascade against the app's own next attempt.
func TestSwitch_FailureDefersRecovery(t *testing.T) {
	a, b := switchProfilePair()
	wgMgr := &fakeWGManager{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, &fakeKillSwitch{}, a, b)
	opts := ConnectOptions{PreferredTransport: "cloak"}
	if err := svc.Connect(context.Background(), a.ID, opts); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	wgMgr.mu.Lock()
	wgMgr.noHandshake = true
	wgMgr.mu.Unlock()
	if err := svc.Switch(context.Background(), b.ID, opts); err == nil {
		t.Fatal("Switch succeeded over a tunnel that never handshakes")
	}
	if svc.recoveryDue() {
		t.Error("recovery due immediately after a failed switch; it must get the same grace as a failed connect")
	}
}

// A switch arriving while a background rebuild holds opMu must interrupt the
// rebuild and land on the new server, not queue behind the whole cascade.
func TestSwitch_PreemptsRunningRebuild(t *testing.T) {
	a, b := switchProfilePair()
	cloakMgr := &fakeCloakManager{}
	svc := newTestService(t, cloakMgr, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, a, b)
	opts := ConnectOptions{PreferredTransport: "cloak"}
	ctx := context.Background()
	if err := svc.Connect(ctx, a.ID, opts); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	entered := blockFirstStart(cloakMgr)
	rebuildDone := make(chan struct{})
	go func() {
		defer close(rebuildDone)
		svc.attemptSessionRebuild(ctx, a, "tunnel stopped carrying traffic")
	}()
	awaitOrFail(t, entered, "the rebuild to reach its transport start")

	switched := make(chan error, 1)
	go func() { switched <- svc.Switch(ctx, b.ID, opts) }()
	select {
	case err := <-switched:
		if err != nil {
			t.Fatalf("Switch during a rebuild: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Switch queued behind the rebuild instead of preempting it")
	}
	awaitOrFail(t, rebuildDone, "the preempted rebuild to unwind")

	if status := svc.Status(ctx); status.State != state.StateConnected {
		t.Fatalf("after switch: state = %s (%s)", status.State, status.Detail)
	}
	if active, _ := svc.getCurrentProfile(); active.ID != b.ID {
		t.Fatalf("current profile = %s, want %s", active.ID, b.ID)
	}
	if svc.recoveryPending() {
		t.Error("the preempted rebuild was booked as a failed attempt")
	}
}

// A rebuild that lost TryLock used to wipe the in-flight switch's cancel, so
// Disconnect could not interrupt the switch and sat behind the whole cascade.
func TestRebuild_LeavesSwitchInterruptible(t *testing.T) {
	a, b := switchProfilePair()
	cloakMgr := &fakeCloakManager{}
	svc := newTestService(t, cloakMgr, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, a, b)
	opts := ConnectOptions{PreferredTransport: "cloak"}
	ctx := context.Background()
	if err := svc.Connect(ctx, a.ID, opts); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	entered := blockFirstStart(cloakMgr)
	switched := make(chan error, 1)
	go func() { switched <- svc.Switch(ctx, b.ID, opts) }()
	awaitOrFail(t, entered, "the switch to reach its transport start")

	if err := svc.rebuildSilentSession(ctx, a); !errors.Is(err, errRebuildBusy) {
		t.Fatalf("rebuild during a switch: %v, want errRebuildBusy", err)
	}

	disconnected := make(chan error, 1)
	go func() { disconnected <- svc.Disconnect(ctx, false) }()
	select {
	case err := <-disconnected:
		if err != nil {
			t.Fatalf("Disconnect: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Disconnect could not interrupt the switch: the rebuild clobbered its cancel")
	}
	if err := <-switched; !errors.Is(err, context.Canceled) {
		t.Errorf("Switch error = %v, want context.Canceled", err)
	}
}
