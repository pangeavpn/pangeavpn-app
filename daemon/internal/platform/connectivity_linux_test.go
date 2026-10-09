//go:build linux && !android

package platform

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestHasPhysicalDefaultRoute(t *testing.T) {
	names := map[int]string{1: "lo", 2: "eth0", 3: "pangea0", 4: "wg0", 5: "tun1"}
	nameOf := func(i int) string { return names[i] }
	def := func(oif int) linuxRoute {
		return linuxRoute{family: unix.AF_INET, dstLen: 0, table: unix.RT_TABLE_MAIN, routeTyp: unix.RTN_UNICAST, oif: oif}
	}
	def6 := func(oif int) linuxRoute {
		r := def(oif)
		r.family = unix.AF_INET6
		return r
	}

	cases := []struct {
		name   string
		routes []linuxRoute
		want   bool
	}{
		{"empty table", nil, false},
		{"default via eth0", []linuxRoute{def(2)}, true},
		{"default on tunnel only", []linuxRoute{def(3)}, false},
		{"default on wg only", []linuxRoute{def(4)}, false},
		{"default on tun only", []linuxRoute{def(5)}, false},
		{"default on loopback only", []linuxRoute{def(1)}, false},
		{"unknown interface index", []linuxRoute{def(9)}, false},
		{"tunnel half-default is not a default", []linuxRoute{{family: unix.AF_INET, dstLen: 1, table: unix.RT_TABLE_MAIN, routeTyp: unix.RTN_UNICAST, oif: 2}}, false},
		{"non-main table default ignored", []linuxRoute{{family: unix.AF_INET, dstLen: 0, table: 51820, routeTyp: unix.RTN_UNICAST, oif: 2}}, false},
		{"blackhole default ignored", []linuxRoute{{family: unix.AF_INET, dstLen: 0, table: unix.RT_TABLE_MAIN, routeTyp: unix.RTN_BLACKHOLE, oif: 2}}, false},
		{"unknown family ignored", []linuxRoute{{dstLen: 0, table: unix.RT_TABLE_MAIN, routeTyp: unix.RTN_UNICAST, oif: 2}}, false},
		{"tunnel default plus real default", []linuxRoute{def(3), def(2)}, true},
		{"ipv6 default via eth0", []linuxRoute{def6(2)}, true},
		{"ipv6 default on tunnel only", []linuxRoute{def6(3)}, false},
	}
	for _, c := range cases {
		if _, _, got := physicalDefaultRoute(c.routes, nameOf); got != c.want {
			t.Errorf("%s: physicalDefaultRoute = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHostInternetIsKnownOnLinux(t *testing.T) {
	// CI runners always have a route table; the point is the verdict exists.
	if _, known := HostInternet(); !known {
		t.Fatal("HostInternet() verdict unknown; expected a confident answer on linux")
	}
}
