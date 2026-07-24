package api

import (
	"net"
	"sort"
	"strings"
)

// currentNetworkKey fingerprints the physical network the host is attached to,
// so the daemon can remember which transport last worked here and try it first
// next time. It mirrors the desktop networkWatcher's approach — a stable
// signature of the non-tunnel interfaces' addresses — but keys IPv6 on the /64
// prefix so privacy-extension address rotation within one network does not
// change the key. Returns "" when nothing usable is found; the caller then
// skips the memory optimization (and the cascade still tries every transport).
func currentNetworkKey() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}

	var parts []string
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if isTunnelInterfaceName(iface.Name) {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			ip := ipFromAddr(addr)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
				continue
			}
			parts = append(parts, iface.Name+":"+networkToken(ip))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// isTunnelInterfaceName reports whether name looks like a VPN/tunnel interface,
// which must be excluded so the daemon's own tunnel bring-up/tear-down does not
// change the network key. Matches the desktop networkWatcher's prefixes.
func isTunnelInterfaceName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "tun") ||
		strings.HasPrefix(lower, "utun") ||
		strings.HasPrefix(lower, "wg") ||
		strings.HasPrefix(lower, "pangea")
}

// networkToken renders an address into the stable part of the key: the full
// IPv4 address (a DHCP lease is stable for the network), or the IPv6 /64
// network prefix (host bits rotate under privacy extensions).
func networkToken(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func ipFromAddr(addr net.Addr) net.IP {
	switch v := addr.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	default:
		return nil
	}
}
