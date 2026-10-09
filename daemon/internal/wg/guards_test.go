package wg

import (
	"context"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// The shipped stall: guards held the manager lock across networksetup/route
// execs, so every /status blocked behind a ~6s repair on each health tick.
func TestEnsureDNS_DoesNotHoldManagerLockDuringRepair(t *testing.T) {
	prev := ensureSessionDNSFn
	block := make(chan struct{})
	started := make(chan struct{})
	ensureSessionDNSFn = func(_ *tunnelSession, _ []string) (bool, error) {
		close(started)
		<-block
		return false, nil
	}
	defer func() { ensureSessionDNSFn = prev }()

	m := &wireGuardGoManager{logs: state.NewLogStore(16), sessions: map[string]*tunnelSession{}}
	key := sanitizeTunnelName("pangea0")
	m.storeSession(key, &tunnelSession{interfaceName: "utun7"})

	profile := state.WireGuardProfile{TunnelName: "pangea0", DNS: []string{"1.1.1.1"}}
	go func() { _, _ = m.EnsureDNS(context.Background(), profile) }()
	<-started
	defer close(block)

	done := make(chan struct{})
	go func() { m.session(key); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("manager lock held across the DNS repair; /status would stall")
	}
}

func TestEnsureEndpointRoutes_DoesNotHoldManagerLockDuringRepair(t *testing.T) {
	prev := ensureSessionEndpointRoutesFn
	block := make(chan struct{})
	started := make(chan struct{})
	ensureSessionEndpointRoutesFn = func(_ context.Context, _ *tunnelSession, _ map[uint64]struct{}) (bool, error) {
		close(started)
		<-block
		return false, nil
	}
	defer func() { ensureSessionEndpointRoutesFn = prev }()

	m := &wireGuardGoManager{logs: state.NewLogStore(16), sessions: map[string]*tunnelSession{}}
	key := sanitizeTunnelName("pangea0")
	m.storeSession(key, &tunnelSession{interfaceName: "utun7"})

	profile := state.WireGuardProfile{TunnelName: "pangea0"}
	go func() { _, _ = m.EnsureEndpointRoutes(context.Background(), profile) }()
	<-started
	defer close(block)

	done := make(chan struct{})
	go func() { m.session(key); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("manager lock held across the route repair; /status would stall")
	}
}
