//go:build linux && !android

package platform

import (
	"encoding/binary"
	"net"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// linuxRoute is the slice of an RTM_NEWROUTE record the verdict needs.
type linuxRoute struct {
	family   uint8
	dstLen   uint8
	table    uint32
	routeTyp uint8
	oif      int
	gateway  net.IP
}

// HostInternet reports whether a physical interface holds a main-table
// default route. The tunnel's 0.0.0.0/1+128.0.0.0/1 pair never matches, so
// the verdict tracks the underlying link even while connected.
func HostInternet() (online bool, known bool) {
	_, _, err := PhysicalDefaultRoute()
	return hostInternetFromRoute(err)
}

// PhysicalDefaultRoute names the physical interface holding the main-table
// default route (IPv4 preferred) and its gateway, "" when the route is on-link.
func PhysicalDefaultRoute() (iface, gateway string, err error) {
	tab, err := syscall.NetlinkRIB(unix.RTM_GETROUTE, unix.AF_UNSPEC)
	if err != nil {
		return "", "", err
	}
	msgs, err := syscall.ParseNetlinkMessage(tab)
	if err != nil {
		return "", "", err
	}
	routes := make([]linuxRoute, 0, len(msgs))
	for i := range msgs {
		if r, ok := decodeRoute(&msgs[i]); ok {
			routes = append(routes, r)
		}
	}
	r, name, ok := physicalDefaultRoute(routes, interfaceNameByIndex)
	if !ok {
		return "", "", ErrNoDefaultRoute
	}
	if r.gateway != nil {
		gateway = r.gateway.String()
	}
	return name, gateway, nil
}

func decodeRoute(m *syscall.NetlinkMessage) (linuxRoute, bool) {
	if m.Header.Type != unix.RTM_NEWROUTE || len(m.Data) < unix.SizeofRtMsg {
		return linuxRoute{}, false
	}
	rtm := (*unix.RtMsg)(unsafe.Pointer(&m.Data[0]))
	r := linuxRoute{family: rtm.Family, dstLen: rtm.Dst_len, table: uint32(rtm.Table), routeTyp: rtm.Type}
	attrs, err := syscall.ParseNetlinkRouteAttr(m)
	if err != nil {
		return linuxRoute{}, false
	}
	for _, a := range attrs {
		if len(a.Value) < 4 {
			continue
		}
		switch a.Attr.Type {
		case unix.RTA_OIF:
			r.oif = int(binary.LittleEndian.Uint32(a.Value))
		case unix.RTA_TABLE:
			r.table = binary.LittleEndian.Uint32(a.Value)
		case unix.RTA_GATEWAY:
			r.gateway = net.IP(a.Value)
		}
	}
	return r, true
}

func interfaceNameByIndex(index int) string {
	ifi, err := net.InterfaceByIndex(index)
	if err != nil {
		return ""
	}
	return ifi.Name
}

func physicalDefaultRoute(routes []linuxRoute, nameOf func(int) string) (linuxRoute, string, bool) {
	for _, family := range []uint8{unix.AF_INET, unix.AF_INET6} {
		for _, r := range routes {
			if r.family != family || r.dstLen != 0 || r.routeTyp != unix.RTN_UNICAST || r.table != unix.RT_TABLE_MAIN {
				continue
			}
			name := nameOf(r.oif)
			if name == "" || isVirtualInterface(name) {
				continue
			}
			return r, name, true
		}
	}
	return linuxRoute{}, "", false
}

func isVirtualInterface(name string) bool {
	for _, prefix := range []string{"lo", "pangea", "wg", "tun", "tap"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
