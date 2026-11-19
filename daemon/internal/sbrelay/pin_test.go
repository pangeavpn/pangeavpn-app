package sbrelay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// The edge name never resolves here, so only a dial to the pinned address can succeed.
func TestCDNDialsThePinnedAddressAndReleasesOnStop(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM := ca.leaf(t, testCDNHost)
	port := startCDNNode(t, certPEM, keyPEM)
	profile := testCDNProfile(port, ca.pem)
	profile.RemoteHost = "edge.unresolvable.invalid"

	var asked string
	var released atomic.Int32
	m := NewCDN(state.NewLogStore(50))
	m.SetPinner(func(_ context.Context, host string) (Pin, error) {
		asked = host
		// Loopback is never bound (sing skips virtual addresses), so any name passes here.
		return Pin{Server: "127.0.0.1", Interface: "pin-test-nic", Release: func() { released.Add(1) }}, nil
	})

	ctx := context.Background()
	if err := m.Start(ctx, profile); err != nil {
		t.Fatalf("pinned dial failed: %v", err)
	}
	if asked != "edge.unresolvable.invalid" {
		t.Fatalf("pinner asked for %q, want the edge host", asked)
	}
	if released.Load() != 0 {
		t.Fatal("pin released while the session still runs")
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for released.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := released.Load(); got != 1 {
		t.Fatalf("pin released %d times after Stop, want once", got)
	}
}

func TestCDNReleasesThePinWhenTheDialFails(t *testing.T) {
	var released atomic.Int32
	m := NewCDN(state.NewLogStore(50))
	m.SetPinner(func(context.Context, string) (Pin, error) {
		return Pin{Server: "127.0.0.1", Interface: "pin-test-nic", Release: func() { released.Add(1) }}, nil
	})
	// Port 1 on loopback refuses at once.
	if err := m.Start(context.Background(), testCDNProfile(1, "")); err == nil {
		t.Fatal("dial to a closed port succeeded")
	}
	if got := released.Load(); got != 1 {
		t.Fatalf("pin released %d times after a failed dial, want once", got)
	}
}

// A pin that can't be had must stop the dial: unpinned, it would loop into the tunnel.
func TestCDNRefusesToDialUnpinned(t *testing.T) {
	m := NewCDN(state.NewLogStore(50))
	want := errors.New("no physical interface")
	m.SetPinner(func(context.Context, string) (Pin, error) { return Pin{}, want })
	if err := m.Start(context.Background(), testCDNProfile(1, "")); !errors.Is(err, want) {
		t.Fatalf("Start() = %v, want the pin failure", err)
	}
	if m.Status().Running {
		t.Fatal("transport running without a pin")
	}
}

// TEST-NET-1 never answers, so a dial that ignored the bind would wait out the dial timeout;
// binding to a missing interface must instead fail at once, naming it.
func TestCDNAppliesThePinsInterfaceBinding(t *testing.T) {
	m := NewCDN(state.NewLogStore(50))
	m.SetPinner(func(context.Context, string) (Pin, error) {
		return Pin{Server: "192.0.2.1", Interface: "pangea-no-such-interface"}, nil
	})
	start := time.Now()
	err := m.Start(context.Background(), testCDNProfile(443, ""))
	if err == nil {
		m.Stop(context.Background())
		t.Fatal("dial bound to a missing interface succeeded")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("Start failed after %s with %v; an applied bind fails at once", elapsed, err)
	}
}

func TestCDNRejectsAPinThatBindsNothing(t *testing.T) {
	var released atomic.Int32
	m := NewCDN(state.NewLogStore(50))
	m.SetPinner(func(context.Context, string) (Pin, error) {
		return Pin{Server: "127.0.0.1", Release: func() { released.Add(1) }}, nil
	})
	if err := m.Start(context.Background(), testCDNProfile(1, "")); err == nil || !strings.Contains(err.Error(), "binds nothing") {
		t.Fatalf("Start() = %v, want an empty pin refused", err)
	}
	if released.Load() != 1 {
		t.Fatalf("pin released %d times, want once", released.Load())
	}
}

// TUIC dials a dedicated node address with its own route, so it is never pinned.
func TestTUICIsNeverPinned(t *testing.T) {
	sp, err := buildTUIC(state.TUICProfile{RemoteHost: "203.0.113.9", RemotePort: 443, UUID: "u", Password: "p"})
	if err != nil {
		t.Fatal(err)
	}
	if sp.applyPin != nil {
		t.Fatal("TUIC spec asks to be pinned")
	}
}

// On macOS the pin supplies the socket itself (the egress broker): the edge connection must
// go through that dial, via the detour outbound, and never open a socket of sing-box's own.
func TestCDNDialsThroughThePinsOwnDialer(t *testing.T) {
	ca := newTestCA(t)
	certPEM, keyPEM := ca.leaf(t, testCDNHost)
	port := startCDNNode(t, certPEM, keyPEM)
	profile := testCDNProfile(port, ca.pem)
	profile.RemoteHost = "edge.unresolvable.invalid"

	var dials atomic.Int32
	var dialed string
	m := NewCDN(state.NewLogStore(50))
	m.SetPinner(func(context.Context, string) (Pin, error) {
		return Pin{Server: "127.0.0.1", Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
			dials.Add(1)
			dialed = network + " " + address
			var d net.Dialer
			return d.DialContext(ctx, network, address)
		}}, nil
	})
	if err := m.Start(context.Background(), profile); err != nil {
		t.Fatalf("dial through the pin's own dialer failed: %v", err)
	}
	defer m.Stop(context.Background())
	if dials.Load() == 0 || dialed != fmt.Sprintf("tcp 127.0.0.1:%d", port) {
		t.Fatalf("pin dialer used %d times (last %q), want the edge connection on it", dials.Load(), dialed)
	}
}
