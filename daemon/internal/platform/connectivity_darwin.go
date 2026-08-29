//go:build darwin

package platform

import (
	"net"
	"strings"
	"syscall"

	"golang.org/x/net/route"
)

// HostInternet reports whether a physical interface holds a true default
// route. The tunnel's own 0.0.0.0/1+128.0.0.0/1 pair never matches, so the
// verdict tracks the underlying link even while connected.
func HostInternet() (online bool, known bool) {
	_, _, err := PhysicalDefaultRoute()
	return hostInternetFromRoute(err)
}

// PhysicalDefaultRoute names the physical interface holding the default route
// (IPv4 preferred) and its gateway, "" when the route is on-link.
func PhysicalDefaultRoute() (iface, gateway string, err error) {
	rib, err := route.FetchRIB(syscall.AF_UNSPEC, route.RIBTypeRoute, 0)
	if err != nil {
		return "", "", err
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return "", "", err
	}
	iface, gateway, ok := physicalDefaultRoute(msgs, interfaceNameByIndex)
	if !ok {
		return "", "", ErrNoDefaultRoute
	}
	return iface, gateway, nil
}

func interfaceNameByIndex(index int) string {
	ifi, err := net.InterfaceByIndex(index)
	if err != nil {
		return ""
	}
	return ifi.Name
}

func physicalDefaultRoute(msgs []route.Message, nameOf func(int) string) (iface, gateway string, ok bool) {
	for _, wantV4 := range []bool{true, false} {
		for _, m := range msgs {
			rm, ok := m.(*route.RouteMessage)
			if !ok {
				continue
			}
			if rm.Flags&syscall.RTF_UP == 0 {
				continue
			}
			if !isDefaultRoute(rm.Addrs) || isInet4(rm.Addrs[syscall.RTAX_DST]) != wantV4 {
				continue
			}
			name := nameOf(rm.Index)
			if name == "" || isVirtualInterface(name) {
				continue
			}
			return name, gatewayString(rm.Addrs), true
		}
	}
	return "", "", false
}

func isInet4(addr route.Addr) bool {
	_, ok := addr.(*route.Inet4Addr)
	return ok
}

func gatewayString(addrs []route.Addr) string {
	if len(addrs) <= syscall.RTAX_GATEWAY {
		return ""
	}
	switch a := addrs[syscall.RTAX_GATEWAY].(type) {
	case *route.Inet4Addr:
		return net.IP(a.IP[:]).String()
	case *route.Inet6Addr:
		return net.IP(a.IP[:]).String()
	}
	return ""
}

// isDefaultRoute matches a /0 destination: an all-zero dst with a nil or
// all-zero netmask. The tunnel's /1 halves carry a non-zero mask and miss.
func isDefaultRoute(addrs []route.Addr) bool {
	if len(addrs) <= syscall.RTAX_DST {
		return false
	}
	if !isZeroAddr(addrs[syscall.RTAX_DST]) {
		return false
	}
	if len(addrs) > syscall.RTAX_NETMASK && addrs[syscall.RTAX_NETMASK] != nil {
		return isZeroAddr(addrs[syscall.RTAX_NETMASK])
	}
	return true
}

func isZeroAddr(addr route.Addr) bool {
	switch a := addr.(type) {
	case *route.Inet4Addr:
		return a.IP == [4]byte{}
	case *route.Inet6Addr:
		return a.IP == [16]byte{}
	default:
		return false
	}
}

func isVirtualInterface(name string) bool {
	for _, prefix := range []string{"utun", "lo", "gif", "stf", "awdl", "llw", "bridge", "ipsec"} {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
