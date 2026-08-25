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
	dstLen   uint8
	table    uint32
	routeTyp uint8
	oif      int
}

// HostInternet reports whether a physical interface holds a main-table
// default route. The tunnel's 0.0.0.0/1+128.0.0.0/1 pair never matches, so
// the verdict tracks the underlying link even while connected.
func HostInternet() (online bool, known bool) {
	tab, err := syscall.NetlinkRIB(unix.RTM_GETROUTE, unix.AF_UNSPEC)
	if err != nil {
		return false, false
	}
	msgs, err := syscall.ParseNetlinkMessage(tab)
	if err != nil {
		return false, false
	}
	routes := make([]linuxRoute, 0, len(msgs))
	for i := range msgs {
		if r, ok := decodeRoute(&msgs[i]); ok {
			routes = append(routes, r)
		}
	}
	return hasPhysicalDefaultRoute(routes, interfaceNameByIndex), true
}

func decodeRoute(m *syscall.NetlinkMessage) (linuxRoute, bool) {
	if m.Header.Type != unix.RTM_NEWROUTE || len(m.Data) < unix.SizeofRtMsg {
		return linuxRoute{}, false
	}
	rtm := (*unix.RtMsg)(unsafe.Pointer(&m.Data[0]))
	r := linuxRoute{dstLen: rtm.Dst_len, table: uint32(rtm.Table), routeTyp: rtm.Type}
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

func hasPhysicalDefaultRoute(routes []linuxRoute, nameOf func(int) string) bool {
	for _, r := range routes {
		if r.dstLen != 0 || r.routeTyp != unix.RTN_UNICAST || r.table != unix.RT_TABLE_MAIN {
			continue
		}
		name := nameOf(r.oif)
		if name == "" || isVirtualInterface(name) {
			continue
		}
		return true
	}
	return false
}

func isVirtualInterface(name string) bool {
	for _, prefix := range []string{"lo", "pangea", "wg", "tun", "tap"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
