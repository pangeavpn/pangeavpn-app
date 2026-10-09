package egress

import (
	"net/netip"
	"testing"
)

func TestSelectWindowsDefault(t *testing.T) {
	gw := netip.MustParseAddr("192.168.1.1")
	none := netip.Addr{}
	unspecified := netip.IPv4Unspecified()
	eth := winRoute{LUID: 10, Index: 7, Name: "Ethernet", IfType: 6, Up: true, NextHop: gw, Metric: 35}
	wifi := winRoute{LUID: 11, Index: 12, Name: "Wi-Fi", IfType: 71, Up: true, NextHop: gw, Metric: 60}
	wintun := winRoute{LUID: 2, Index: 40, Name: "PangeaVPN", IfType: ifTypePropVirtual, Up: true, NextHop: unspecified, Metric: 0}
	wintunGW := winRoute{LUID: 3, Index: 41, Name: "OtherVPN", IfType: ifTypePropVirtual, Up: true, NextHop: netip.MustParseAddr("10.64.0.1"), Metric: 1}
	wwan := winRoute{LUID: 20, Index: 21, Name: "Cellular", IfType: ifTypeWwanpp, Up: true, NextHop: unspecified, Metric: 70}
	wwanNoHop := winRoute{LUID: 22, Index: 23, Name: "Cellular 2", IfType: ifTypeWwanpp2, Up: true, NextHop: none, Metric: 70}
	ppp := winRoute{LUID: 24, Index: 25, Name: "PPPoE", IfType: ifTypePPP, Up: true, NextHop: unspecified, Metric: 80}
	onLinkEth := winRoute{LUID: 30, Index: 31, Name: "Ethernet 3", IfType: 6, Up: true, NextHop: unspecified, Metric: 1}

	cases := []struct {
		name string
		rows []winRoute
		want uint32
	}{
		{"lowest metric wins", []winRoute{wifi, eth}, 7},
		{"wintun on-link default skipped", []winRoute{wintun, wifi}, 12},
		{"virtual adapter with gateway skipped", []winRoute{wintunGW, wifi}, 12},
		{"ethernet-typed tunnel alias skipped", []winRoute{{LUID: 4, Index: 42, Name: "PangeaVPN", IfType: 6, Up: true, NextHop: netip.MustParseAddr("10.66.0.1"), Metric: 1}, wifi}, 12},
		{"on-link WWAN allowed", []winRoute{wintun, wwan}, 21},
		{"on-link WWAN without next hop allowed", []winRoute{wwanNoHop}, 23},
		{"on-link PPP allowed", []winRoute{ppp}, 25},
		{"on-link ethernet refused", []winRoute{onLinkEth}, 0},
		{"down skipped", []winRoute{{LUID: 1, Index: 2, IfType: 6, NextHop: gw, Metric: 1}, wifi}, 12},
		{"loopback row skipped", []winRoute{{LUID: 1, Index: 2, IfType: 6, Up: true, Loopback: true, NextHop: gw}, wifi}, 12},
		{"software loopback type skipped", []winRoute{{LUID: 1, Index: 1, IfType: ifTypeSoftwareLoopback, Up: true, NextHop: gw}, wifi}, 12},
		{"tunnel type skipped", []winRoute{{LUID: 1, Index: 3, IfType: ifTypeTunnel, Up: true, NextHop: gw}, wifi}, 12},
		{"loopback next hop skipped", []winRoute{{LUID: 1, Index: 3, IfType: 6, Up: true, NextHop: netip.MustParseAddr("127.0.0.1")}, wifi}, 12},
		{"zero index skipped", []winRoute{{LUID: 1, Index: 0, IfType: 6, Up: true, NextHop: gw}, wifi}, 12},
		{"tie breaks on LUID", []winRoute{{LUID: 9, Index: 90, IfType: 6, Up: true, NextHop: gw, Metric: 5}, {LUID: 8, Index: 80, IfType: 6, Up: true, NextHop: gw, Metric: 5}}, 80},
		{"nothing usable", []winRoute{wintun, wintunGW}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := selectWindowsDefault(tc.rows)
			if tc.want == 0 {
				if ok {
					t.Fatalf("selected %+v, want none", got)
				}
				return
			}
			if !ok || got.Index != tc.want {
				t.Fatalf("selected %+v (ok=%v), want index %d", got, ok, tc.want)
			}
		})
	}
}

func TestSelectDarwinDefault(t *testing.T) {
	ifaces := map[int]struct {
		name string
		up   bool
	}{4: {"en0", true}, 5: {"en1", true}, 9: {"utun3", true}, 6: {"en2", false}, 7: {"bridge100", true}}
	lookup := func(i int) (string, bool) { v := ifaces[i]; return v.name, v.up }
	def := func(index int, scoped bool) ribRoute {
		return ribRoute{Index: index, DefaultV4: true, Up: true, Gateway: true, IfScope: scoped}
	}
	cases := []struct {
		name   string
		routes []ribRoute
		want   int
	}{
		{"unscoped preferred over earlier scoped", []ribRoute{def(5, true), def(4, false)}, 4},
		{"utun primary skipped, scoped physical used", []ribRoute{def(9, false), def(4, true), def(5, true)}, 4},
		{"needs gateway flag", []ribRoute{{Index: 4, DefaultV4: true, Up: true}, def(5, true)}, 5},
		{"down interface skipped", []ribRoute{def(6, false), def(5, true)}, 5},
		{"non-default skipped", []ribRoute{{Index: 4, Up: true, Gateway: true}}, 0},
		{"reject skipped", []ribRoute{{Index: 4, DefaultV4: true, Up: true, Gateway: true, RejectOrBlack: true}}, 0},
		{"bridge skipped", []ribRoute{def(7, false)}, 0},
		{"unknown interface skipped", []ribRoute{def(42, false)}, 0},
		{"own mirror never selected", []ribRoute{{Index: 4, DefaultV4: true, Up: true, Gateway: true, IfScope: true, Mirror: true}}, 0},
		{"mirror skipped for a real scoped default", []ribRoute{{Index: 4, DefaultV4: true, Up: true, Gateway: true, IfScope: true, Mirror: true}, def(5, true)}, 5},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index, name, ok := selectDarwinDefault(tc.routes, lookup)
			if tc.want == 0 {
				if ok {
					t.Fatalf("selected %d %q, want none", index, name)
				}
				return
			}
			if !ok || index != tc.want || name != ifaces[tc.want].name {
				t.Fatalf("selected %d %q (ok=%v), want %d", index, name, ok, tc.want)
			}
		})
	}
}

func TestPlanDarwinMirror(t *testing.T) {
	ifaces := map[int]struct {
		name string
		up   bool
	}{4: {"en0", true}, 5: {"en5", true}, 9: {"utun3", true}, 6: {"en2", false}}
	lookup := func(i int) (string, bool) { v := ifaces[i]; return v.name, v.up }
	gw := netip.MustParseAddr("192.168.1.1")
	gw2 := netip.MustParseAddr("10.0.0.1")
	primary := func(index int, next netip.Addr) ribRoute {
		return ribRoute{Index: index, DefaultV4: true, Up: true, Gateway: true, NextHop: next}
	}
	scoped := func(index int, next netip.Addr) ribRoute {
		r := primary(index, next)
		r.IfScope = true
		return r
	}
	mirror := func(index int, next netip.Addr) ribRoute {
		r := scoped(index, next)
		r.Mirror = true
		return r
	}
	tunnelHalf := ribRoute{Index: 9, Up: true}
	sd := func(index int, next netip.Addr) scopedDefault {
		return scopedDefault{Index: index, Name: ifaces[index].name, NextHop: next}
	}
	cases := []struct {
		name   string
		routes []ribRoute
		add    *scopedDefault
		remove []scopedDefault
	}{
		{"primary with no scoped copy gets one", []ribRoute{tunnelHalf, primary(4, gw)}, &scopedDefault{Index: 4, Name: "en0", NextHop: gw}, nil},
		{"own mirror already in place", []ribRoute{tunnelHalf, primary(4, gw), mirror(4, gw)}, nil, nil},
		{"configd scoped default already in place", []ribRoute{primary(4, gw), scoped(4, gw)}, nil, nil},
		{"foreign scoped default holds the key", []ribRoute{primary(4, gw), scoped(4, gw2)}, nil, nil},
		{"foreign scoped default holds the key even when down", []ribRoute{primary(4, gw), {Index: 4, DefaultV4: true, IfScope: true}}, nil, nil},
		{"gateway moved: stale mirror replaced", []ribRoute{primary(4, gw2), mirror(4, gw)}, &scopedDefault{Index: 4, Name: "en0", NextHop: gw2}, []scopedDefault{sd(4, gw)}},
		{"primary moved: old mirror dropped", []ribRoute{primary(5, gw2), mirror(4, gw)}, &scopedDefault{Index: 5, Name: "en5", NextHop: gw2}, []scopedDefault{sd(4, gw)}},
		{"no primary: mirror dropped, none added", []ribRoute{tunnelHalf, mirror(4, gw)}, nil, []scopedDefault{sd(4, gw)}},
		{"another network's scoped default is not a copy", []ribRoute{primary(4, gw), scoped(5, gw)}, &scopedDefault{Index: 4, Name: "en0", NextHop: gw}, nil},
		{"virtual primary is never mirrored", []ribRoute{primary(9, gw), scoped(4, gw)}, nil, nil},
		{"down primary is never mirrored", []ribRoute{primary(6, gw)}, nil, nil},
		{"primary without an IPv4 next hop is never mirrored", []ribRoute{primary(4, netip.Addr{})}, nil, nil},
		{"mirror on a vanished interface is left to the kernel", []ribRoute{primary(4, gw), mirror(4, gw), mirror(42, gw)}, nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan := planDarwinMirror(tc.routes, lookup)
			add, remove := plan.Add, plan.Remove
			if (add == nil) != (tc.add == nil) || (add != nil && *add != *tc.add) {
				t.Fatalf("add = %+v, want %+v", add, tc.add)
			}
			if len(remove) != len(tc.remove) {
				t.Fatalf("remove = %+v, want %+v", remove, tc.remove)
			}
			for i := range remove {
				if remove[i] != tc.remove[i] {
					t.Fatalf("remove = %+v, want %+v", remove, tc.remove)
				}
			}
		})
	}
}

func TestSelectLinuxDefault(t *testing.T) {
	ifaces := map[int]struct {
		name string
		up   bool
	}{2: {"eth0", true}, 3: {"wlan0", true}, 4: {"wg0", true}, 5: {"eth1", false}, 1: {"lo", true}}
	lookup := func(i int) (string, bool) { v := ifaces[i]; return v.name, v.up }
	def := func(oif int, prio uint32) linuxRoute {
		return linuxRoute{Table: linuxTableMain, Type: linuxRTNUnicast, Oif: oif, Priority: prio}
	}
	cases := []struct {
		name   string
		routes []linuxRoute
		want   int
	}{
		{"lowest priority wins", []linuxRoute{def(3, 600), def(2, 100)}, 2},
		{"tie breaks on index", []linuxRoute{def(3, 100), def(2, 100)}, 2},
		{"wireguard skipped", []linuxRoute{def(4, 0), def(3, 600)}, 3},
		{"down skipped", []linuxRoute{def(5, 0), def(3, 600)}, 3},
		{"loopback skipped", []linuxRoute{def(1, 0)}, 0},
		{"other table skipped", []linuxRoute{{Table: 51820, Type: linuxRTNUnicast, Oif: 2}}, 0},
		{"non-unicast skipped", []linuxRoute{{Table: linuxTableMain, Type: 6, Oif: 2}}, 0},
		{"non-default skipped", []linuxRoute{{DstLen: 1, Table: linuxTableMain, Type: linuxRTNUnicast, Oif: 2}}, 0},
		{"no interface skipped", []linuxRoute{def(0, 0)}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			index, name, ok := selectLinuxDefault(tc.routes, lookup)
			if tc.want == 0 {
				if ok {
					t.Fatalf("selected %d %q, want none", index, name)
				}
				return
			}
			if !ok || index != tc.want || name != ifaces[tc.want].name {
				t.Fatalf("selected %d %q (ok=%v), want %d", index, name, ok, tc.want)
			}
		})
	}
}
