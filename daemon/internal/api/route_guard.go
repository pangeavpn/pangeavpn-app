package api

import (
	"context"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// maxEndpointRouteRepairDeferrals bounds how many ticks a repair may hold the
// recovery path off, so a route that never settles can't suppress it forever.
const maxEndpointRouteRepairDeferrals = 3

// ensureEndpointRoutes re-pins the routes carrying WireGuard to its node (the
// OS can drop or move them), reporting whether to give the repair a tick.
func (s *Service) ensureEndpointRoutes(ctx context.Context, profile state.Profile) bool {
	guard, ok := s.wg.(wgRouteGuard)
	if !ok {
		return false
	}

	repaired, err := guard.EnsureEndpointRoutes(ctx, profile.WireGuard)
	// An error means the routes are unverified even if repaired=true; it
	// neither settles the counter nor earns the caller a skip tick.
	if err != nil {
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not verify the tunnel's endpoint routes: %v", err))
		if repairs := s.recordEndpointRouteRepair(); repairs > maxEndpointRouteRepairDeferrals {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
				"the tunnel's endpoint routes have failed verification %d health checks running; letting the usual recovery run", repairs))
		}
		return false
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
	s.resetEndpointRouteRepairs()
}

// resetEndpointRouteRepairs clears the deferral counter for a new session;
// callers include service.go's resetRecovery, Connect and Disconnect.
func (s *Service) resetEndpointRouteRepairs() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.endpointRouteRepairs = 0
}
