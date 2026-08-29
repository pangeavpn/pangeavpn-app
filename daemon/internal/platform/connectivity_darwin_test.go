//go:build darwin

package platform

import (
	"syscall"
	"testing"

	"golang.org/x/net/route"
)

func defaultV4(index int, flags int) *route.RouteMessage {
	return &route.RouteMessage{
		Flags: flags,
		Index: index,
		Addrs: []route.Addr{
			&route.Inet4Addr{},
			&route.Inet4Addr{IP: [4]byte{192, 168, 1, 1}},
			&route.Inet4Addr{},
		},
	}
}

func tunnelHalfV4(index int) *route.RouteMessage {
	return &route.RouteMessage{
		Flags: syscall.RTF_UP,
		Index: index,
		Addrs: []route.Addr{
			&route.Inet4Addr{},
			&route.Inet4Addr{},
			&route.Inet4Addr{IP: [4]byte{128, 0, 0, 0}},
		},
	}
}

func TestHasPhysicalDefaultRoute(t *testing.T) {
	names := map[int]string{1: "lo0", 4: "en0", 9: "utun3"}
	nameOf := func(i int) string { return names[i] }

	cases := []struct {
		name string
		msgs []route.Message
		want bool
	}{
		{"empty table", nil, false},
		{"default via en0", []route.Message{defaultV4(4, syscall.RTF_UP|syscall.RTF_GATEWAY)}, true},
		{"default route down", []route.Message{defaultV4(4, 0)}, false},
		{"default on utun only", []route.Message{defaultV4(9, syscall.RTF_UP)}, false},
		{"default on loopback only", []route.Message{defaultV4(1, syscall.RTF_UP)}, false},
		{"tunnel half-default is not a default", []route.Message{tunnelHalfV4(4)}, false},
		{"unknown interface index", []route.Message{defaultV4(7, syscall.RTF_UP)}, false},
		{"tunnel halves plus real default", []route.Message{tunnelHalfV4(9), defaultV4(4, syscall.RTF_UP)}, true},
		{
			"v6 default via en0",
			[]route.Message{&route.RouteMessage{
				Flags: syscall.RTF_UP,
				Index: 4,
				Addrs: []route.Addr{&route.Inet6Addr{}, &route.Inet6Addr{}, &route.Inet6Addr{}},
			}},
			true,
		},
		{
			"host route to zero-mask-less specific dst",
			[]route.Message{&route.RouteMessage{
				Flags: syscall.RTF_UP,
				Index: 4,
				Addrs: []route.Addr{&route.Inet4Addr{IP: [4]byte{10, 0, 0, 1}}, &route.Inet4Addr{}, nil},
			}},
			false,
		},
	}
	for _, c := range cases {
		if _, _, got := physicalDefaultRoute(c.msgs, nameOf); got != c.want {
			t.Errorf("%s: physicalDefaultRoute = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestHostInternetIsKnownOnDarwin(t *testing.T) {
	// CI runners always have a route table; the point is the verdict exists.
	if _, known := HostInternet(); !known {
		t.Fatal("HostInternet() verdict unknown; expected a confident answer on darwin")
	}
}
