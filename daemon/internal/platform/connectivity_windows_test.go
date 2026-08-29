//go:build windows

package platform

import (
	"errors"
	"net/netip"
	"testing"
)

func TestPhysicalDefaultRoute_PrefersTheCheapestGatewayOnAPhysicalInterface(t *testing.T) {
	rows := []windowsRoute{
		{name: "pangeavpn", up: true, gateway: netip.MustParseAddr("0.0.0.0"), metric: 1},
		{name: "Ethernet", up: false, gateway: netip.MustParseAddr("10.0.0.1"), metric: 5},
		{name: "Wi-Fi", up: true, gateway: netip.MustParseAddr("192.168.1.1"), metric: 50},
		{name: "Ethernet 2", up: true, gateway: netip.MustParseAddr("10.0.1.1"), metric: 25},
	}
	iface, gateway, err := physicalDefaultRoute(rows)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if iface != "Ethernet 2" || gateway != "10.0.1.1" {
		t.Fatalf("route = %s via %s, want Ethernet 2 via 10.0.1.1", iface, gateway)
	}
}

func TestPhysicalDefaultRoute_OnlyATunnelDefaultIsNoRoute(t *testing.T) {
	rows := []windowsRoute{
		{name: "pangeavpn", up: true, gateway: netip.MustParseAddr("0.0.0.0"), metric: 1},
		{name: "wg-other", up: true, gateway: netip.MustParseAddr("10.9.0.1"), metric: 1},
	}
	if _, _, err := physicalDefaultRoute(rows); !errors.Is(err, ErrNoDefaultRoute) {
		t.Fatalf("err = %v, want ErrNoDefaultRoute", err)
	}
}
