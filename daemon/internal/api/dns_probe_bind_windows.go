package api

import (
	"fmt"
	"net"
)

// bindDialerToInterface binds the probe's socket to iface's own IPv4 address:
// Windows has no per-socket SO_BINDTODEVICE, but a local-address bind still
// keeps the query from leaving — or a reply from being accepted — off-tunnel.
func bindDialerToInterface(iface string) (*net.Dialer, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("resolve tunnel interface %q: %w", iface, err)
	}
	addrs, err := ifi.Addrs()
	if err != nil {
		return nil, fmt.Errorf("read addresses for tunnel interface %q: %w", iface, err)
	}
	for _, addr := range addrs {
		ip := ipFromAddr(addr)
		if ip == nil || ip.To4() == nil {
			continue
		}
		return &net.Dialer{LocalAddr: &net.UDPAddr{IP: ip}}, nil
	}
	return nil, fmt.Errorf("tunnel interface %q has no IPv4 address", iface)
}
