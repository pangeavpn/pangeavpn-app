package api

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// connectedRouteGuardService is a connected naive session, ready to have its
// health checked tick by tick.
func connectedRouteGuardService(t *testing.T) (*Service, *fakeWGManager, *fakeNaiveManager) {
	t.Helper()
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	svc := newTestService(t, &fakeCloakManager{}, naive, wgMgr, &fakeKillSwitch{}, silentTunnelProfile())

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	return svc, wgMgr, naive
}

// TestHealthCheck_EndpointRoutesCheckedEveryTick proves the route carrying
// WireGuard to its node is verified continuously rather than only at bring-up.
// The OS can drop it on a media-sense flap or move the gateway under it, and
// nothing inside the tunnel can see that.
func TestHealthCheck_EndpointRoutesCheckedEveryTick(t *testing.T) {
	svc, wgMgr, _ := connectedRouteGuardService(t)

	wgMgr.mu.Lock()
	callsAfterConnect := wgMgr.routeGuardCalls
	wgMgr.mu.Unlock()

	for range 4 {
		svc.runHealthCheck(context.Background())
	}

	wgMgr.mu.Lock()
	calls := wgMgr.routeGuardCalls - callsAfterConnect
	wgMgr.mu.Unlock()
	if calls != 4 {
		t.Errorf("endpoint route guard calls = %d across 4 health checks, want 4", calls)
	}
}

// TestHealthCheck_RepairedEndpointRouteIsPreferredOverARebuild proves a silent
// tunnel whose bypass route was lost gets the route back instead of a full
// session rebuild. Rewriting the route restores the same session in one tick;
// the rebuild drops the tunnel, restarts the transport, and re-arms the
// firewall to reach the same place.
func TestHealthCheck_RepairedEndpointRouteIsPreferredOverARebuild(t *testing.T) {
	svc, wgMgr, naive := connectedRouteGuardService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.routeGuardRepaired = true
	wgMgr.mu.Unlock()

	goSilent(wgMgr)
	svc.runHealthCheck(context.Background())

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d, want 0 — a re-pinned route is given the tick to handshake on", restarts)
	}
	naive.mu.Lock()
	stopped := naive.stopCalled
	naive.mu.Unlock()
	if stopped {
		t.Error("transport was restarted; re-pinning the route should not disturb the session")
	}
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Errorf("state = %q, want CONNECTED", st)
	}

	var repairs int
	for _, entry := range svc.Logs(0) {
		if strings.Contains(entry.Msg, "re-pinned it") {
			repairs++
		}
	}
	if repairs != 1 {
		t.Errorf("logged route repairs = %d, want 1", repairs)
	}
}

// TestHealthCheck_StillRebuildsWhenTheRouteWasFine proves the repair path does
// not swallow the silence detector: a tunnel silent for some other reason has
// nothing to re-pin, so the rebuild still runs.
func TestHealthCheck_StillRebuildsWhenTheRouteWasFine(t *testing.T) {
	svc, wgMgr, _ := connectedRouteGuardService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	goSilent(wgMgr)
	svc.runHealthCheck(context.Background())

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 1 {
		t.Errorf("wireguard restarts = %d, want 1 — nothing was repaired, so the tunnel is still silent", restarts)
	}
}

// TestHealthCheck_EndlessRouteRepairsStopDeferringTheRebuild proves a route
// that never settles cannot suppress recovery forever. Deferring the rebuild is
// worth it while a repair is plausibly about to work; a route being re-pinned
// every tick is its own fault, and the session still needs rescuing.
func TestHealthCheck_EndlessRouteRepairsStopDeferringTheRebuild(t *testing.T) {
	svc, wgMgr, _ := connectedRouteGuardService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.routeGuardRepaired = true
	wgMgr.mu.Unlock()

	goSilent(wgMgr)
	for range maxEndpointRouteRepairDeferrals + 1 {
		svc.runHealthCheck(context.Background())
	}

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 1 {
		t.Errorf("wireguard restarts = %d, want 1 — the rebuild runs once the repairs stop looking like progress", restarts)
	}
}

// TestHealthCheck_RouteGuardErrorDoesNotDropTheSession proves a guard that
// cannot read the routing table is reported and moved past. Failing to verify a
// route is not a reason to tear down a working tunnel.
func TestHealthCheck_RouteGuardErrorDoesNotDropTheSession(t *testing.T) {
	svc, wgMgr, _ := connectedRouteGuardService(t)

	wgMgr.mu.Lock()
	wgMgr.routeGuardErr = errors.New("read default routes: access denied")
	wgMgr.mu.Unlock()

	svc.runHealthCheck(context.Background())

	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED — a guard error is not a dead tunnel", st)
	}
	var warned bool
	for _, entry := range svc.Logs(0) {
		if strings.Contains(entry.Msg, "could not verify the tunnel's endpoint routes") {
			warned = true
		}
	}
	if !warned {
		t.Error("expected the guard error to be logged")
	}
}
