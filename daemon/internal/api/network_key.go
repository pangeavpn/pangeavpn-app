package api

import (
	"context"
	"errors"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// currentNetworkKey fingerprints the physical network so the daemon can
// remember which transport last worked here; "" means try every transport.
func currentNetworkKey() string {
	return currentNetworkKeyExcluding(nil)
}

// currentNetworkKeyExcluding lets a caller name additional interfaces to treat
// as the VPN's own, for a tunnel name that isTunnelInterfaceName won't match.
func currentNetworkKeyExcluding(extraTunnelNames []string) string {
	// The route table names the physical uplink even while our own tunnel
	// holds the default route, so a switch keys the same network as a connect.
	primary, gateway, err := platform.PhysicalDefaultRoute()
	if errors.Is(err, platform.ErrNoDefaultRoute) {
		return ""
	}
	if err != nil {
		primary, gateway = defaultRouteInterfaceName(), defaultGatewayIP()
	}
	osIfaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	ifaces := make([]keyIface, 0, len(osIfaces))
	for _, iface := range osIfaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		ifaces = append(ifaces, keyIface{name: iface.Name, up: iface.Flags&net.FlagUp != 0, addrs: addrs})
	}
	return composeNetworkKey(ifaces, primary, gateway, extraTunnelNames)
}

// keyIface is the slice of interface state composeNetworkKey needs, decoupled
// from net.Interface so the composition logic is testable without live NICs.
type keyIface struct {
	name  string
	up    bool
	addrs []net.Addr
}

// composeNetworkKey keys on the default-route interface, falling back to all
// up non-tunnel interfaces when our own tunnel is the one carrying that route.
func composeNetworkKey(ifaces []keyIface, primary, gateway string, extraTunnelNames []string) string {
	if primary == "" {
		return ""
	}
	primaryIsTunnel := isTunnelInterfaceName(primary, extraTunnelNames)

	var parts []string
	for _, iface := range ifaces {
		if !primaryIsTunnel && iface.name != primary {
			continue
		}
		if !iface.up {
			continue
		}
		if isTunnelInterfaceName(iface.name, extraTunnelNames) {
			continue
		}
		for _, addr := range iface.addrs {
			ip := ipFromAddr(addr)
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
				continue
			}
			parts = append(parts, iface.name+":"+networkToken(ip, gateway))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	sort.Strings(parts)
	return strings.Join(parts, "|")
}

// isTunnelInterfaceName reports whether name looks like a VPN/tunnel
// interface, excluded so our own tunnel's bring-up/tear-down never changes the key.
func isTunnelInterfaceName(name string, extraTunnelNames []string) bool {
	lower := strings.ToLower(name)
	for _, extra := range extraTunnelNames {
		if lower == strings.ToLower(strings.TrimSpace(extra)) {
			return true
		}
	}
	for _, prefix := range []string{"tun", "utun", "wg", "pangea", "tailscale", "ppp", "ipsec", "docker", "veth", "br-", "vmnet", "vboxnet"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}

// networkToken renders an address into the stable part of the key: with a
// gateway it uses the /24 or /64 network plus that gateway, else the full address.
func networkToken(ip net.IP, gateway string) string {
	if v4 := ip.To4(); v4 != nil {
		if gateway == "" {
			return v4.String()
		}
		return v4.Mask(net.CIDRMask(24, 32)).String() + "/24@" + gateway
	}
	prefix := ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
	if gateway == "" {
		return prefix
	}
	return prefix + "@" + gateway
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

// defaultRouteInterfaceName is the fallback where the platform has no route
// table reader; a UDP "connect" only resolves the local route, never dials out.
func defaultRouteInterfaceName() string {
	ip := defaultRouteLocalIP()
	if ip == nil {
		return ""
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			if got := ipFromAddr(addr); got != nil && got.Equal(ip) {
				return iface.Name
			}
		}
	}
	return ""
}

// defaultRouteLocalIP resolves via routing table only; the RFC 5737/3849
// addresses below are never actually dialed.
func defaultRouteLocalIP() net.IP {
	if conn, err := net.DialTimeout("udp4", "203.0.113.1:9", 200*time.Millisecond); err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return addr.IP
		}
	}
	if conn, err := net.DialTimeout("udp6", "[2001:db8::1]:9", 200*time.Millisecond); err == nil {
		defer conn.Close()
		if addr, ok := conn.LocalAddr().(*net.UDPAddr); ok {
			return addr.IP
		}
	}
	return nil
}

var (
	darwinGatewayRe = regexp.MustCompile(`gateway:\s*(\S+)`)
	linuxGatewayRe  = regexp.MustCompile(`via\s+(\S+)`)
)

// defaultGatewayIP best-effort resolves the gateway; any failure is silent
// and just drops the gateway from the key.
func defaultGatewayIP() string {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	switch runtime.GOOS {
	case "darwin":
		out := runCommand(ctx, "route", "-n", "get", "default")
		if m := darwinGatewayRe.FindStringSubmatch(out); m != nil {
			return m[1]
		}
	default:
		out := runCommand(ctx, "ip", "route", "show", "default")
		if m := linuxGatewayRe.FindStringSubmatch(out); m != nil {
			return m[1]
		}
	}
	return ""
}

func runCommand(ctx context.Context, name string, args ...string) string {
	cmd := exec.CommandContext(ctx, name, args...)
	platform.ConfigureBackgroundProcess(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return string(out)
}
