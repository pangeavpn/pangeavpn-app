package api

import (
	"sync"
	"time"
)

// A hub answer keeps a network's silence meaningful for reachBaselineTTL; a
// connect re-proves it once reachBaselineRefresh has passed.
const (
	reachBaselineTTL         = 24 * time.Hour
	reachBaselineRefresh     = 6 * time.Hour
	reachBaselineMaxNetworks = 64
)

// reachBaseline remembers, per network, which probe routes the hub answered on.
type reachBaseline struct {
	mu       sync.Mutex
	now      func() time.Time
	networks map[string]map[string]time.Time
}

func newReachBaseline() *reachBaseline {
	return &reachBaseline{now: time.Now, networks: make(map[string]map[string]time.Time)}
}

func (b *reachBaseline) record(network, route string) {
	if network == "" || route == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	routes, ok := b.networks[network]
	if !ok {
		b.evictStalestLocked()
		routes = make(map[string]time.Time)
		b.networks[network] = routes
	}
	routes[route] = b.now()
}

// proven reports whether the hub answered on route, on this network, recently
// enough that its silence now says something about the network.
func (b *reachBaseline) proven(network, route string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	at, ok := b.networks[network][route]
	return ok && b.now().Sub(at) < reachBaselineTTL
}

func (b *reachBaseline) fresh(network string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, at := range b.networks[network] {
		if b.now().Sub(at) < reachBaselineRefresh {
			return true
		}
	}
	return false
}

func (b *reachBaseline) forget(network string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.networks, network)
}

func (b *reachBaseline) evictStalestLocked() {
	if len(b.networks) < reachBaselineMaxNetworks {
		return
	}
	stalest, stalestAt := "", time.Time{}
	for network, routes := range b.networks {
		latest := latestProof(routes)
		if stalest == "" || latest.Before(stalestAt) {
			stalest, stalestAt = network, latest
		}
	}
	delete(b.networks, stalest)
}

func latestProof(routes map[string]time.Time) time.Time {
	var latest time.Time
	for _, at := range routes {
		if at.After(latest) {
			latest = at
		}
	}
	return latest
}
