//go:build darwin

package wg

import (
	"context"
	"net/netip"
	"testing"
	"time"
)

const (
	tunnelIfIndex   = 12
	physicalIfIndex = 4
)

func addr(s string) netip.Addr { return netip.MustParseAddr(s) }

// physicalDefault is the real default route: 0.0.0.0/0 out of en0 via a
// gateway address.
func physicalDefault() darwinRouteEntry {
	return darwinRouteEntry{
		destination: addr("0.0.0.0"),
		maskBits:    0,
		gateway:     addr("192.168.1.1"),
		ifIndex:     physicalIfIndex,
		viaGateway:  true,
	}
}

// tunnelHalfDefault is what the tunnel installs: 0.0.0.0/1 scoped to the utun
// with no gateway. It shares a destination with the real default, so only the
// mask and the missing gateway tell them apart.
func tunnelHalfDefault() darwinRouteEntry {
	return darwinRouteEntry{
		destination: addr("0.0.0.0"),
		maskBits:    1,
		ifIndex:     tunnelIfIndex,
	}
}

// TestDarwinDefaultGateway_IgnoresTheTunnelsHalfDefault is the case that makes
// the table scan necessary: asking the kernel which route serves 0.0.0.0 would
// answer with the tunnel's 0.0.0.0/1, not the gateway the bypass needs.
func TestDarwinDefaultGateway_IgnoresTheTunnelsHalfDefault(t *testing.T) {
	entries := []darwinRouteEntry{tunnelHalfDefault(), physicalDefault()}

	gateway, ok := darwinDefaultGateway(entries, tunnelIfIndex)
	if !ok {
		t.Fatal("no default gateway found with a physical default present")
	}
	if gateway != addr("192.168.1.1") {
		t.Errorf("gateway = %s, want 192.168.1.1", gateway)
	}
}

func TestDarwinDefaultGateway_Rejects(t *testing.T) {
	tests := []struct {
		name    string
		entries []darwinRouteEntry
	}{
		{
			name:    "only the tunnel's half-default",
			entries: []darwinRouteEntry{tunnelHalfDefault()},
		},
		{
			// A default route on the tunnel itself: pinning to it is the loop.
			name: "default route belongs to the tunnel",
			entries: []darwinRouteEntry{{
				destination: addr("0.0.0.0"), gateway: addr("10.0.0.1"),
				ifIndex: tunnelIfIndex, viaGateway: true,
			}},
		},
		{
			// PPP/cellular style: no address to pin to.
			name: "interface-scoped default has no gateway",
			entries: []darwinRouteEntry{{
				destination: addr("0.0.0.0"), ifIndex: physicalIfIndex,
			}},
		},
		{
			name:    "no default at all",
			entries: []darwinRouteEntry{{destination: addr("10.0.0.0"), maskBits: 8, ifIndex: physicalIfIndex}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := darwinDefaultGateway(tc.entries, tunnelIfIndex); ok {
				t.Error("reported a usable gateway where there is none")
			}
		})
	}
}

// TestDarwinDefaultGateway_TieBreakIsStable proves repeated elections agree, so
// the guard cannot re-pin back and forth between two defaults every tick.
func TestDarwinDefaultGateway_TieBreakIsStable(t *testing.T) {
	high := darwinRouteEntry{destination: addr("0.0.0.0"), gateway: addr("10.0.0.1"), ifIndex: 9, viaGateway: true}
	low := darwinRouteEntry{destination: addr("0.0.0.0"), gateway: addr("192.168.1.1"), ifIndex: 4, viaGateway: true}

	forward, _ := darwinDefaultGateway([]darwinRouteEntry{high, low}, tunnelIfIndex)
	reversed, _ := darwinDefaultGateway([]darwinRouteEntry{low, high}, tunnelIfIndex)
	if forward != reversed {
		t.Errorf("election depends on table order: %s vs %s", forward, reversed)
	}
}

func TestEndpointRouteNeedsRepair(t *testing.T) {
	gateway := addr("192.168.1.1")
	endpoint := addr("203.0.113.9")

	tests := []struct {
		name    string
		current darwinRouteEntry
		found   bool
		repair  bool
	}{
		{
			name:    "intact bypass is left alone",
			current: darwinRouteEntry{destination: endpoint, gateway: gateway, ifIndex: physicalIfIndex, host: true, viaGateway: true},
			found:   true,
			repair:  false,
		},
		{
			// The link flapped and took the host route with it, so the endpoint
			// now falls through to the tunnel's own half-default.
			name:   "host route dropped",
			found:  false,
			repair: true,
		},
		{
			name:    "gateway moved",
			current: darwinRouteEntry{destination: endpoint, gateway: addr("10.20.0.1"), ifIndex: physicalIfIndex, host: true, viaGateway: true},
			found:   true,
			repair:  true,
		},
		{
			name:    "host route points into the tunnel",
			current: darwinRouteEntry{destination: endpoint, gateway: gateway, ifIndex: tunnelIfIndex, host: true, viaGateway: true},
			found:   true,
			repair:  true,
		},
		{
			// A node on the local subnet is reached without a gateway. Re-pinning
			// one would fight the kernel's own entry every tick.
			name:    "on-link endpoint needs no gateway",
			current: darwinRouteEntry{destination: endpoint, ifIndex: physicalIfIndex, host: true},
			found:   true,
			repair:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := endpointRouteNeedsRepair(tc.current, tc.found, tunnelIfIndex, gateway); got != tc.repair {
				t.Errorf("endpointRouteNeedsRepair = %t, want %t", got, tc.repair)
			}
		})
	}
}

// TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp proves a session with nothing
// recorded never reaches the routing table; the guard runs on every tick.
func TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp(t *testing.T) {
	repaired, err := ensureSessionEndpointRoutes(context.Background(), &tunnelSession{interfaceName: "utun4"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired {
		t.Error("repaired = true with no recorded routes")
	}
}

func TestAllowedIPsHaveIPv6(t *testing.T) {
	if allowedIPsHaveIPv6([]string{"0.0.0.0/0", "10.0.0.0/8"}) {
		t.Error("v4-only allowed ips reported as having ipv6")
	}
	if !allowedIPsHaveIPv6([]string{"0.0.0.0/0", "::/0"}) {
		t.Error("::/0 not detected as ipv6")
	}
	if !allowedIPsHaveIPv6([]string{"2001:db8::/32"}) {
		t.Error("v6 prefix not detected as ipv6")
	}
}

func TestDarwinDNSListsEqual(t *testing.T) {
	if !darwinDNSListsEqual([]string{"1.1.1.1", "9.9.9.9"}, []string{"1.1.1.1", "9.9.9.9"}) {
		t.Error("identical lists reported unequal")
	}
	if darwinDNSListsEqual([]string{"1.1.1.1"}, []string{"9.9.9.9"}) {
		t.Error("different lists reported equal")
	}
	if darwinDNSListsEqual([]string{"1.1.1.1"}, nil) {
		t.Error("list vs nil reported equal")
	}
}

func TestIsDarwinDNSUnknown(t *testing.T) {
	if !isDarwinDNSUnknown([]string{darwinDNSUnknownMarker}) {
		t.Error("unknown marker not recognized")
	}
	if isDarwinDNSUnknown(nil) {
		t.Error("nil (automatic) treated as unknown")
	}
	if isDarwinDNSUnknown([]string{"1.1.1.1"}) {
		t.Error("real server list treated as unknown")
	}
}

func TestIsDarwinRouteExists(t *testing.T) {
	if !isDarwinRouteExists([]byte("add host 1.2.3.4: gateway 1.2.3.1 fails File exists")) {
		t.Error("did not recognize a File exists failure")
	}
	if isDarwinRouteExists([]byte("add host 1.2.3.4: network is unreachable")) {
		t.Error("misclassified an unrelated route failure as already-exists")
	}
}

// TestDarwinIPv4Routes_ReadsTheLiveTable exercises the parse against whatever
// the machine actually has, which is the part no fixture can stand in for.
func TestDarwinIPv4Routes_ReadsTheLiveTable(t *testing.T) {
	entries, err := darwinIPv4Routes()
	if err != nil {
		t.Fatalf("read routing table: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no IPv4 routes reported; the parse is not seeing the table")
	}

	var defaults int
	for _, entry := range entries {
		if !entry.host && entry.maskBits == 0 && entry.destination.IsUnspecified() {
			defaults++
		}
		if !entry.destination.Is4() {
			t.Errorf("non-IPv4 destination %s in an AF_INET scan", entry.destination)
		}
	}
	t.Logf("parsed %d IPv4 routes, %d of them default", len(entries), defaults)
}

// A networksetup call that hangs in configd used to hold the wg manager lock
// forever; every exec must now die at the deadline.
func TestDarwinCmdCombined_KillsHungCommands(t *testing.T) {
	prev := darwinExecTimeout
	darwinExecTimeout = 100 * time.Millisecond
	defer func() { darwinExecTimeout = prev }()

	start := time.Now()
	if _, err := darwinCmdCombined("/bin/sleep", "30"); err == nil {
		t.Fatal("hung command returned no error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("hung command survived its deadline (%s)", elapsed)
	}
}
