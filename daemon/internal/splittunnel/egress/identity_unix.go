//go:build darwin || linux

package egress

import (
	"net"
	"net/netip"
)

func identityFor(index int, name string) (Identity, error) {
	ifc, err := net.InterfaceByIndex(index)
	if err != nil {
		return Identity{}, err
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return Identity{}, err
	}
	var v4 []netip.Addr
	for _, a := range addrs {
		if ipn, ok := a.(*net.IPNet); ok {
			if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
				v4 = append(v4, ip)
			}
		}
	}
	if ifc.Name != "" {
		name = ifc.Name
	}
	return Identity{Index: index, Name: name, Addrs: sortedV4(v4)}, nil
}

func interfaceState(index int) (string, bool) {
	ifc, err := net.InterfaceByIndex(index)
	if err != nil {
		return "", false
	}
	return ifc.Name, ifc.Flags&net.FlagUp != 0
}
