package mobile

// Two-hop routes: ports apps/desktop/src/shared/multihop.ts and the hop half
// of provision in apps/desktop/src/main/pangeaApiClient.ts.

import (
	"errors"
	"fmt"
	"regexp"
	"sort"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

var regionSuffix = regexp.MustCompile(`-(\d+)$`)

// regionKeyOf maps eu-west-1 to eu-west, the hosts' region grouping.
func regionKeyOf(serverID string) string {
	return regionSuffix.ReplaceAllString(serverID, "")
}

// hopLoad treats an unreported load as full, so it sorts last.
func hopLoad(server serverInfo) int {
	if server.Load == nil {
		return 100
	}
	return *server.Load
}

// multihopEntries lists relay-capable servers outside the exit's region,
// lightest first and in hub order on ties.
func multihopEntries(servers []serverInfo, exitServerID string) []string {
	exitRegion := regionKeyOf(exitServerID)
	candidates := make([]serverInfo, 0, len(servers))
	for _, server := range servers {
		if server.Multihop && regionKeyOf(server.ID) != exitRegion {
			candidates = append(candidates, server)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return hopLoad(candidates[i]) < hopLoad(candidates[j]) })
	ids := make([]string, 0, len(candidates))
	for _, server := range candidates {
		ids = append(ids, server.ID)
	}
	return ids
}

// routeServers looks both ends up; missing is the first id not found, and
// entry stays nil for a single hop.
func routeServers(srvs []serverInfo, exitID, entryID string) (exit, entry *serverInfo, missing string) {
	if exit = serverWithID(srvs, exitID); exit == nil {
		return nil, nil, exitID
	}
	if entryID == "" {
		return exit, nil, ""
	}
	if entry = serverWithID(srvs, entryID); entry == nil {
		return nil, nil, entryID
	}
	return exit, entry, ""
}

func serverWithID(srvs []serverInfo, id string) *serverInfo {
	for i := range srvs {
		if srvs[i].ID == id {
			return &srvs[i]
		}
	}
	return nil
}

// validateHopEntry runs before registering, so a bad pick never moves the peer.
func validateHopEntry(exitServerID string, entry *serverInfo) error {
	if entry.ID == exitServerID {
		return errors.New("mobile: the multihop entry and exit must be different servers")
	}
	if !entry.Multihop {
		return fmt.Errorf("mobile: %s cannot relay multihop", entry.ID)
	}
	return nil
}

// hopProfile dials the entry and carries WireGuard on to the exit. It fails
// closed: a partial hop would egress at the entry.
func hopProfile(exit, entry *serverInfo, hop *state.HopProfile) (*state.Profile, error) {
	if hop == nil {
		return nil, errors.New("mobile: server did not return the multihop route")
	}
	profile := buildProfile(entry)
	profile.ID = "auto-" + exit.ID + "-via-" + entry.ID
	profile.Name = exit.Name + " via " + entry.Name
	route := *hop
	profile.Hop = &route
	// The hub never routes snowflake through an entry.
	profile.Snowflake = nil
	if err := state.ValidateHop(*profile); err != nil {
		return nil, fmt.Errorf("mobile: %w", err)
	}
	applied := state.ApplyHop(*profile)
	return &applied, nil
}
