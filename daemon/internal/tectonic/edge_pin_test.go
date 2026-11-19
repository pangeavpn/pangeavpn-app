package tectonic

import (
	"context"
	"errors"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func cdnTestProfile(t *testing.T, edge string) state.TectonicProfile {
	t.Helper()
	node := &testNode{addr: "203.0.113.70:443", secret: make([]byte, 32), pin: "pin"}
	profile := node.profile(t, time.Now().Add(time.Hour))
	profile.CDN = &state.TectonicCDNRoute{Host: "edge.cdn.example", Path: "/ws", Edge: edge}
	return profile
}

func TestPinEdge_PointsLanesAtThePinnedEdgeAndBindsThem(t *testing.T) {
	m := newTestManager()
	var asked string
	bound := false
	m.SetEdgePinner(func(_ context.Context, host string) (EdgePin, error) {
		asked = host
		return EdgePin{
			Server:  "104.21.24.200",
			Control: func(string, string, syscall.RawConn) error { bound = true; return nil },
			Release: func() {},
		}, nil
	})
	for _, tc := range []struct{ edge, wantEdge, wantHost string }{
		{"", "104.21.24.200:443", "edge.cdn.example"},
		{"edge.cdn.example:8443", "104.21.24.200:8443", "edge.cdn.example"},
	} {
		profile := cdnTestProfile(t, tc.edge)
		cfg, err := m.config(profile)
		if err != nil {
			t.Fatal(err)
		}
		release, err := m.pinEdge(context.Background(), &cfg, profile)
		if err != nil {
			t.Fatal(err)
		}
		release()
		if asked != tc.wantHost || cfg.Profile.CDN.Edge != tc.wantEdge || cfg.Control == nil {
			t.Fatalf("edge %q: asked %q, dial %q, control set %v", tc.edge, asked, cfg.Profile.CDN.Edge, cfg.Control != nil)
		}
		bound = false
		_ = cfg.Control("tcp", "203.0.113.70:443", nil)
		if bound {
			t.Fatal("a direct lane to the node was bound; it keeps following the node's own route")
		}
		_ = cfg.Control("tcp", tc.wantEdge, nil)
		if !bound {
			t.Fatal("a CDN lane's control does not apply the pin")
		}
	}
}

func TestPinEdge_RejectsAPinThatBindsNothing(t *testing.T) {
	m := newTestManager()
	released := 0
	m.SetEdgePinner(func(context.Context, string) (EdgePin, error) {
		return EdgePin{Server: "104.21.24.200", Release: func() { released++ }}, nil
	})
	profile := cdnTestProfile(t, "")
	cfg, err := m.config(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.pinEdge(context.Background(), &cfg, profile); err != nil || released != 1 || cfg.Profile.CDN != nil || cfg.Control != nil {
		t.Fatalf("pinEdge() = %v, %d releases, cdn kept %v; want the empty pin released and the CDN lanes dropped", err, released, cfg.Profile.CDN != nil)
	}
}

// A failed pin drops only the CDN lanes, never dials them unpinned, and keeps the direct ones.
func TestPinEdge_FailsClosedAndLeavesDirectProfilesAlone(t *testing.T) {
	m := newTestManager()
	m.SetEdgePinner(func(context.Context, string) (EdgePin, error) { return EdgePin{}, errors.New("no physical interface") })

	profile := cdnTestProfile(t, "")
	cfg, err := m.config(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.pinEdge(context.Background(), &cfg, profile); err != nil || cfg.Profile.CDN != nil || cfg.Control != nil {
		t.Fatalf("pinEdge() = %v, cdn kept %v; want the CDN lanes dropped and the session kept", err, cfg.Profile.CDN != nil)
	}

	profile.CDN = nil
	cfg, err = m.config(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.pinEdge(context.Background(), &cfg, profile); err != nil || cfg.Control != nil {
		t.Fatalf("a profile with no CDN route was pinned: err=%v control=%v", err, cfg.Control != nil)
	}
}

// On macOS the pin opens the edge socket itself; only CDN lanes may use it.
func TestPinEdge_RoutesOnlyCDNLanesThroughThePinsDialer(t *testing.T) {
	m := newTestManager()
	var used []string
	m.SetEdgePinner(func(context.Context, string) (EdgePin, error) {
		return EdgePin{Server: "104.21.24.200", Dial: func(_ context.Context, _, address string) (net.Conn, error) {
			used = append(used, address)
			return nil, errors.New("test dialer")
		}}, nil
	})
	profile := cdnTestProfile(t, "")
	cfg, err := m.config(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.pinEdge(context.Background(), &cfg, profile); err != nil || cfg.DialContext == nil || cfg.Control != nil {
		t.Fatalf("pinEdge() = %v, dial set %v, control set %v; want the dialer installed", err, cfg.DialContext != nil, cfg.Control != nil)
	}
	_, _ = cfg.DialContext(context.Background(), "tcp", "104.21.24.200:443")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _ = cfg.DialContext(ctx, "tcp", "192.0.2.1:443")
	if len(used) != 1 || used[0] != "104.21.24.200:443" {
		t.Fatalf("pin dialer saw %v, want only the CDN edge", used)
	}
}
