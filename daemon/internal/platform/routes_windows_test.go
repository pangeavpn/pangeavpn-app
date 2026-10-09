//go:build windows

package platform

import (
	"net/netip"
	"testing"
)

func TestSelectDefaultGateway_SkipsTunnelsAndPicksLowestMetric(t *testing.T) {
	rows := []defaultRouteInfo{
		{aliasLower: "pangeavpn", nextHop: netip.MustParseAddr("10.9.0.1"), metric: 1},
		{aliasLower: "ethernet", nextHop: netip.MustParseAddr("0.0.0.0"), metric: 2},
		{aliasLower: "wi-fi", nextHop: netip.MustParseAddr("192.168.1.1"), metric: 50},
		{aliasLower: "ethernet 2", nextHop: netip.MustParseAddr("10.0.1.1"), metric: 25},
	}
	set := map[string]bool{"pangeavpn": true}

	gw, metric, ok := selectDefaultGateway(rows, set)
	if !ok {
		t.Fatal("no gateway selected")
	}
	if gw != "10.0.1.1" || metric != 25 {
		t.Fatalf("gateway = %s (metric %d), want 10.0.1.1 (25)", gw, metric)
	}
}

func TestSelectDefaultGateway_NoneWhenOnlyTunnelOrUnusable(t *testing.T) {
	rows := []defaultRouteInfo{
		{aliasLower: "pangeavpn", nextHop: netip.MustParseAddr("10.9.0.1"), metric: 1},
		{aliasLower: "ethernet", nextHop: netip.MustParseAddr("0.0.0.0"), metric: 2},
		{aliasLower: "", nextHop: netip.MustParseAddr("192.168.1.1"), metric: 3},
	}
	set := map[string]bool{"pangeavpn": true}

	if _, _, ok := selectDefaultGateway(rows, set); ok {
		t.Fatal("selected a gateway when only a tunnel, an on-link, and an unnamed route existed")
	}
}
