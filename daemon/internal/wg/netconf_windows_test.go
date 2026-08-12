//go:build windows

package wg

import (
	"net/netip"
	"testing"

	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func TestParseWindowsPrefixes_PreservesInterfaceHostBits(t *testing.T) {
	v4, v6, err := parseWindowsPrefixes([]string{"10.0.0.2/24", "10.0.0.2/24"})
	if err != nil {
		t.Fatalf("parseWindowsPrefixes returned error: %v", err)
	}

	if len(v6) != 0 {
		t.Fatalf("expected no IPv6 prefixes, got %v", v6)
	}
	if len(v4) != 1 {
		t.Fatalf("expected one IPv4 prefix, got %d: %v", len(v4), v4)
	}
	if got, want := v4[0].String(), "10.0.0.2/24"; got != want {
		t.Fatalf("expected interface prefix %q, got %q", want, got)
	}
}

func TestParseWindowsRoutePrefixes_MasksNetworks(t *testing.T) {
	v4, v6, err := parseWindowsRoutePrefixes([]string{"10.0.0.2/24", "10.0.0.0/24"})
	if err != nil {
		t.Fatalf("parseWindowsRoutePrefixes returned error: %v", err)
	}

	if len(v6) != 0 {
		t.Fatalf("expected no IPv6 prefixes, got %v", v6)
	}
	if len(v4) != 1 {
		t.Fatalf("expected one IPv4 prefix, got %d: %v", len(v4), v4)
	}
	if got, want := v4[0].String(), "10.0.0.0/24"; got != want {
		t.Fatalf("expected route prefix %q, got %q", want, got)
	}
}

func TestPlannedEndpointRoute(t *testing.T) {
	defaults := func(luid uint64, nextHop string) map[string]windowsDefaultRoute {
		return map[string]windowsDefaultRoute{
			"inet": {
				interfaceLUID: winipcfg.LUID(luid),
				nextHop:       netip.MustParseAddr(nextHop),
			},
		}
	}
	spec := windowsRouteSpec{interfaceLUID: 7, destination: "203.0.113.9/32", nextHop: "192.168.1.1"}

	tests := []struct {
		name     string
		spec     windowsRouteSpec
		defaults map[string]windowsDefaultRoute
		want     windowsRouteSpec
		ok       bool
	}{
		{
			name:     "unchanged default route plans the same spec",
			spec:     spec,
			defaults: defaults(7, "192.168.1.1"),
			want:     spec,
			ok:       true,
		},
		{
			// DHCP renewal onto a new gateway, or a roam to another subnet.
			name:     "new gateway repoints the bypass",
			spec:     spec,
			defaults: defaults(7, "10.20.0.1"),
			want:     windowsRouteSpec{interfaceLUID: 7, destination: "203.0.113.9/32", nextHop: "10.20.0.1"},
			ok:       true,
		},
		{
			// Wi-Fi to Ethernet, dock/undock, adapter power cycle.
			name:     "new default interface repoints the bypass",
			spec:     spec,
			defaults: defaults(9, "192.168.1.1"),
			want:     windowsRouteSpec{interfaceLUID: 9, destination: "203.0.113.9/32", nextHop: "192.168.1.1"},
			ok:       true,
		},
		{
			// Nothing to pin to yet: mid-roam, or the link is down. Leaving the
			// recorded spec alone lets the next tick fix it once a route exists.
			name:     "no default route leaves the spec alone",
			spec:     spec,
			defaults: map[string]windowsDefaultRoute{},
			ok:       false,
		},
		{
			name:     "unparseable destination is skipped",
			spec:     windowsRouteSpec{interfaceLUID: 7, destination: "not-a-prefix", nextHop: "192.168.1.1"},
			defaults: defaults(7, "192.168.1.1"),
			ok:       false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := plannedEndpointRoute(tc.spec, tc.defaults)
			if ok != tc.ok {
				t.Fatalf("plannedEndpointRoute ok = %t, want %t", ok, tc.ok)
			}
			if ok && got != tc.want {
				t.Errorf("plannedEndpointRoute = %+v, want %+v", got, tc.want)
			}
		})
	}
}

// TestPlannedEndpointRoute_IPv6SpecUsesTheIPv6Default proves the family split
// keys off the destination rather than assuming IPv4.
func TestPlannedEndpointRoute_IPv6SpecUsesTheIPv6Default(t *testing.T) {
	defaults := map[string]windowsDefaultRoute{
		"inet":  {interfaceLUID: 1, nextHop: netip.MustParseAddr("192.168.1.1")},
		"inet6": {interfaceLUID: 2, nextHop: netip.MustParseAddr("fe80::1")},
	}
	spec := windowsRouteSpec{interfaceLUID: 9, destination: "2001:db8::1/128", nextHop: "fe80::9"}

	got, ok := plannedEndpointRoute(spec, defaults)
	if !ok {
		t.Fatal("expected an IPv6 spec to plan against the IPv6 default route")
	}
	want := windowsRouteSpec{interfaceLUID: 2, destination: "2001:db8::1/128", nextHop: "fe80::1"}
	if got != want {
		t.Errorf("plannedEndpointRoute = %+v, want %+v", got, want)
	}
}

func TestWindowsDNSMatches(t *testing.T) {
	addrs := func(values ...string) []netip.Addr {
		out := make([]netip.Addr, 0, len(values))
		for _, v := range values {
			out = append(out, netip.MustParseAddr(v))
		}
		return out
	}

	tests := []struct {
		name    string
		current []netip.Addr
		want    []netip.Addr
		match   bool
	}{
		{
			name:    "exact match needs no correction",
			current: addrs("1.1.1.1", "1.0.0.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   true,
		},
		{
			// The whole point: another writer swapped our resolvers for theirs.
			name:    "taken over by someone else",
			current: addrs("192.168.1.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   false,
		},
		{
			name:    "cleared entirely",
			current: nil,
			want:    addrs("1.1.1.1"),
			match:   false,
		},
		{
			// Order is preference, so a reordered list really is a change.
			name:    "reordered",
			current: addrs("1.0.0.1", "1.1.1.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   false,
		},
		{
			name:    "ours plus an appended resolver",
			current: addrs("1.1.1.1", "1.0.0.1", "192.168.1.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   false,
		},
		{
			// The tunnel is IPv4-only and bring-up clears the v6 list; a v6
			// entry reported alongside ours must not force a pointless rewrite.
			name:    "IPv6 entries are ignored",
			current: append(addrs("1.1.1.1", "1.0.0.1"), netip.MustParseAddr("fe80::1")),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   true,
		},
		{
			// GetAdaptersAddresses can report a v4 address in v4-mapped form.
			name:    "v4-mapped addresses compare as IPv4",
			current: []netip.Addr{netip.MustParseAddr("::ffff:1.1.1.1")},
			want:    addrs("1.1.1.1"),
			match:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsDNSMatches(tc.current, tc.want); got != tc.match {
				t.Errorf("windowsDNSMatches(%v, %v) = %t, want %t", tc.current, tc.want, got, tc.match)
			}
		})
	}
}
