package api

import (
	"context"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// maxEndpointRouteRepairDeferrals bounds how many ticks in a row a repair may
// hold the recovery path off. A route that settles needs one; one that keeps
// changing (two default routes trading places, something else rewriting the
// table) must not be able to suppress the silence detector indefinitely.
const maxEndpointRouteRepairDeferrals = 3

// ensureEndpointRoutes re-pins the routes that carry WireGuard to its node, and
// reports whether the caller should give the repair a tick to take effect.
//
// Those routes are the tunnel's only way out, and they hang off a gateway that
// the OS can drop or move mid-session. When that happens the handshakes are
// routed into the tunnel they are trying to establish and the session goes
// silent with every layer below still reporting healthy, so it has to be
// checked from out here.
func (s *Service) ensureEndpointRoutes(ctx context.Context, profile state.Profile) bool {
	guard, ok := s.wg.(wgRouteGuard)
	if !ok {
		return false
	}

	repaired, err := guard.EnsureEndpointRoutes(ctx, profile.WireGuard)
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not verify the tunnel's endpoint routes: %v", err))
	}
	if !repaired {
		s.recordEndpointRoutesSettled()
		return false
	}

	s.logs.Add(state.LogWarn, state.SourceDaemon, "the route carrying the tunnel to its node was missing or pointed at the wrong gateway; re-pinned it")
	if repairs := s.recordEndpointRouteRepair(); repairs > maxEndpointRouteRepairDeferrals {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
			"the tunnel's endpoint route has needed re-pinning %d health checks running; letting the usual recovery run", repairs))
		return false
	}
	return true
}

// recordEndpointRouteRepair books a repair and reports how many have run back
// to back.
func (s *Service) recordEndpointRouteRepair() int {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.endpointRouteRepairs++
	return s.endpointRouteRepairs
}

func (s *Service) recordEndpointRoutesSettled() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.endpointRouteRepairs = 0
}
