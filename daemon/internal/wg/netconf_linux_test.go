//go:build linux

package wg

import (
	"context"
	"net"
	"testing"
)

func TestIsDefaultDst(t *testing.T) {
	tests := []struct {
		name      string
		dst       *net.IPNet
		isDefault bool
	}{
		{name: "nil destination is the default route", dst: nil, isDefault: true},
		{
			name:      "0.0.0.0/0 is the default route",
			dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			isDefault: true,
		},
		{
			// The bypass itself: never mistake it for a default.
			name:      "a host route is not the default",
			dst:       &net.IPNet{IP: net.ParseIP("203.0.113.9"), Mask: net.CIDRMask(32, 32)},
			isDefault: false,
		},
		{
			name:      "a half-default is not the default",
			dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(1, 32)},
			isDefault: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDefaultDst(tc.dst); got != tc.isDefault {
				t.Errorf("isDefaultDst = %t, want %t", got, tc.isDefault)
			}
		})
	}
}

// TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp proves a session with nothing
// recorded never touches netlink; the guard runs on every health tick.
func TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp(t *testing.T) {
	repaired, err := ensureSessionEndpointRoutes(context.Background(), &tunnelSession{interfaceName: "pangea0"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired {
		t.Error("repaired = true with no recorded routes")
	}
}

// TestLinuxBypassGateway_ReadsTheLiveTable exercises the election against
// whatever the machine actually has. It asserts shape rather than a specific
// gateway, since CI runners differ, but a reported gateway must be a usable
// unicast address rather than the unspecified one a tunnel route carries.
func TestLinuxBypassGateway_ReadsTheLiveTable(t *testing.T) {
	gateway, found, err := linuxBypassGateway(0)
	if err != nil {
		t.Fatalf("read default gateway: %v", err)
	}
	if !found {
		t.Skip("no default route with a gateway on this host")
	}
	if gateway.gw == nil || gateway.gw.IsUnspecified() {
		t.Errorf("gateway = %v, want a usable unicast address", gateway.gw)
	}
	if gateway.linkIndex <= 0 {
		t.Errorf("linkIndex = %d, want a real interface", gateway.linkIndex)
	}
}

// TestLinuxBypassGateway_SkipsTheTunnelsOwnDefault proves the election cannot
// pick the tunnel it is meant to bypass. Excluding the live default's own link
// must change the answer rather than return it again.
func TestLinuxBypassGateway_SkipsTheTunnelsOwnDefault(t *testing.T) {
	gateway, found, err := linuxBypassGateway(0)
	if err != nil || !found {
		t.Skip("no default route with a gateway on this host")
	}

	excluded, found, err := linuxBypassGateway(gateway.linkIndex)
	if err != nil {
		t.Fatalf("read default gateway: %v", err)
	}
	if found && excluded.linkIndex == gateway.linkIndex {
		t.Errorf("excluded link %d but the election returned it anyway", gateway.linkIndex)
	}
}
