package egress

import (
	"testing"

	"golang.org/x/net/route"
	"golang.org/x/sys/unix"
)

func ribAddrs(dst, gw, mask route.Addr) []route.Addr {
	addrs := make([]route.Addr, unix.RTAX_MAX)
	addrs[unix.RTAX_DST], addrs[unix.RTAX_GATEWAY], addrs[unix.RTAX_NETMASK] = dst, gw, mask
	return addrs
}

func TestIsDefaultV4(t *testing.T) {
	zero := &route.Inet4Addr{}
	gw := &route.Inet4Addr{IP: [4]byte{192, 168, 1, 1}}
	cases := []struct {
		name  string
		addrs []route.Addr
		want  bool
	}{
		{"no netmask", ribAddrs(zero, gw, nil), true},
		{"zero netmask", ribAddrs(zero, gw, zero), true},
		{"zero v6-form netmask", ribAddrs(zero, gw, &route.Inet6Addr{}), true},
		{"tunnel half", ribAddrs(zero, gw, &route.Inet4Addr{IP: [4]byte{128}}), false},
		{"other destination", ribAddrs(&route.Inet4Addr{IP: [4]byte{128}}, gw, &route.Inet4Addr{IP: [4]byte{128}}), false},
		{"v6 destination", ribAddrs(&route.Inet6Addr{}, gw, nil), false},
		{"link netmask", ribAddrs(zero, gw, &route.LinkAddr{Index: 4}), false},
		{"empty", nil, false},
	}
	for _, tc := range cases {
		if got := isDefaultV4(tc.addrs); got != tc.want {
			t.Fatalf("%s: isDefaultV4 = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestRIBRoutesSelectPrimary(t *testing.T) {
	zero := &route.Inet4Addr{}
	gw := &route.Inet4Addr{IP: [4]byte{192, 168, 1, 1}}
	half := &route.Inet4Addr{IP: [4]byte{128}}
	up := unix.RTF_UP | unix.RTF_STATIC
	msgs := []route.Message{
		&route.InterfaceMessage{Index: 4, Name: "en0"},
		&route.RouteMessage{Index: 9, Flags: up, Addrs: ribAddrs(zero, &route.LinkAddr{Index: 9}, half)},
		&route.RouteMessage{Index: 4, Flags: up | unix.RTF_HOST | unix.RTF_GATEWAY, Addrs: ribAddrs(zero, gw, nil)},
		&route.RouteMessage{Index: 5, Flags: up | unix.RTF_GATEWAY | unix.RTF_IFSCOPE, Addrs: ribAddrs(zero, gw, nil)},
		&route.RouteMessage{Index: 4, Flags: up | unix.RTF_GATEWAY, Addrs: ribAddrs(zero, gw, nil)},
	}
	routes := ribRoutes(msgs)
	if len(routes) != 4 {
		t.Fatalf("got %d routes, want 4: %+v", len(routes), routes)
	}
	if routes[0].DefaultV4 || routes[1].DefaultV4 || !routes[2].DefaultV4 || !routes[2].IfScope || !routes[3].DefaultV4 || routes[3].IfScope {
		t.Fatalf("flag mapping wrong: %+v", routes)
	}
	names := map[int]string{4: "en0", 5: "en5", 9: "utun4"}
	index, name, ok := selectDarwinDefault(routes, func(i int) (string, bool) { return names[i], true })
	if !ok || index != 4 || name != "en0" {
		t.Fatalf("selected %d %q (ok=%v), want en0", index, name, ok)
	}
}
