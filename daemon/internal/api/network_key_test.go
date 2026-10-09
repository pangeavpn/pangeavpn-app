package api

import (
	"net"
	"testing"
)

func v4Addr(ip string) net.Addr {
	return &net.IPNet{IP: net.ParseIP(ip), Mask: net.CIDRMask(24, 32)}
}

func TestComposeNetworkKeyUsesOnlyPrimaryInterface(t *testing.T) {
	ifaces := []keyIface{
		{name: "Wi-Fi", up: true, addrs: []net.Addr{v4Addr("10.3.5.20")}},
		{name: "Ethernet 2", up: true, addrs: []net.Addr{v4Addr("192.168.99.5")}},
	}
	got := composeNetworkKey(ifaces, "Wi-Fi", "10.3.5.1", nil)
	want := "Wi-Fi:10.3.5.0/24@10.3.5.1"
	if got != want {
		t.Fatalf("composeNetworkKey = %q, want %q", got, want)
	}
}

func TestComposeNetworkKeyFallsBackWhenPrimaryIsTunnel(t *testing.T) {
	ifaces := []keyIface{
		{name: "pangeavpn", up: true, addrs: []net.Addr{v4Addr("10.10.1.12")}},
		{name: "Wi-Fi", up: true, addrs: []net.Addr{v4Addr("10.3.5.20")}},
	}
	got := composeNetworkKey(ifaces, "pangeavpn", "10.3.5.1", nil)
	want := "Wi-Fi:10.3.5.0/24@10.3.5.1"
	if got != want {
		t.Fatalf("composeNetworkKey = %q, want %q", got, want)
	}
}

func TestComposeNetworkKeyTunnelPrimaryMatchesPhysicalPrimaryKey(t *testing.T) {
	physical := []keyIface{
		{name: "Wi-Fi", up: true, addrs: []net.Addr{v4Addr("10.3.5.20")}},
	}
	connected := append([]keyIface{
		{name: "pangeavpn", up: true, addrs: []net.Addr{v4Addr("10.10.1.12")}},
	}, physical...)
	before := composeNetworkKey(physical, "Wi-Fi", "10.3.5.1", nil)
	during := composeNetworkKey(connected, "pangeavpn", "10.3.5.1", nil)
	if before == "" || before != during {
		t.Fatalf("key changed across tunnel bring-up: before=%q during=%q", before, during)
	}
}

func TestComposeNetworkKeyEmptyWhenOnlyTunnelsAndVirtualUp(t *testing.T) {
	ifaces := []keyIface{
		{name: "pangeavpn", up: true, addrs: []net.Addr{v4Addr("10.10.1.12")}},
		{name: "vEthernet (Default Switch)", up: true, addrs: []net.Addr{v4Addr("172.17.0.1")}},
		{name: "Wi-Fi", up: false, addrs: []net.Addr{v4Addr("10.3.5.20")}},
	}
	if got := composeNetworkKey(ifaces, "pangeavpn", "", nil); got != "" {
		t.Fatalf("composeNetworkKey = %q, want empty", got)
	}
}

func TestComposeNetworkKeyEmptyWithoutPrimary(t *testing.T) {
	ifaces := []keyIface{
		{name: "Wi-Fi", up: true, addrs: []net.Addr{v4Addr("10.3.5.20")}},
	}
	if got := composeNetworkKey(ifaces, "", "10.3.5.1", nil); got != "" {
		t.Fatalf("composeNetworkKey = %q, want empty", got)
	}
}

func TestComposeNetworkKeyFallbackHonoursExtraTunnelNames(t *testing.T) {
	ifaces := []keyIface{
		{name: "corp-tun", up: true, addrs: []net.Addr{v4Addr("10.10.1.12")}},
		{name: "Wi-Fi", up: true, addrs: []net.Addr{v4Addr("10.3.5.20")}},
	}
	got := composeNetworkKey(ifaces, "corp-tun", "10.3.5.1", []string{"corp-tun"})
	want := "Wi-Fi:10.3.5.0/24@10.3.5.1"
	if got != want {
		t.Fatalf("composeNetworkKey = %q, want %q", got, want)
	}
}

func TestComposeNetworkKeySkipsUnusableAddresses(t *testing.T) {
	ifaces := []keyIface{
		{name: "pangeavpn", up: true, addrs: []net.Addr{v4Addr("10.10.1.12")}},
		{name: "Wi-Fi", up: true, addrs: []net.Addr{
			&net.IPNet{IP: net.ParseIP("fe80::1"), Mask: net.CIDRMask(64, 128)},
			&net.IPNet{IP: net.ParseIP("127.0.0.1"), Mask: net.CIDRMask(8, 32)},
		}},
	}
	if got := composeNetworkKey(ifaces, "pangeavpn", "", nil); got != "" {
		t.Fatalf("composeNetworkKey = %q, want empty", got)
	}
}
