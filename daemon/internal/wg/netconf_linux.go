//go:build linux

package wg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"github.com/godbus/dbus/v5"
	"github.com/vishvananda/netlink"
)

// ---------------------------------------------------------------------------
// Interface configuration via netlink
// ---------------------------------------------------------------------------

// configureLinuxAddresses assigns CIDR addresses to the named interface
// using netlink. The interface must already exist (created by tun.CreateTUN).
func configureLinuxAddresses(interfaceName string, addresses []string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("lookup interface %s: %w", interfaceName, err)
	}

	for _, cidr := range addresses {
		addr, err := netlink.ParseAddr(cidr)
		if err != nil {
			return fmt.Errorf("parse address %s: %w", cidr, err)
		}
		if err := netlink.AddrAdd(link, addr); err != nil && !errors.Is(err, os.ErrExist) {
			return fmt.Errorf("add address %s to %s: %w", cidr, interfaceName, err)
		}
	}
	return nil
}

// bringLinuxInterfaceUp sets the interface to UP state via netlink.
func bringLinuxInterfaceUp(interfaceName string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("lookup interface %s: %w", interfaceName, err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bring up %s: %w", interfaceName, err)
	}
	return nil
}

// setLinuxMTU sets the MTU on the named interface via netlink.
func setLinuxMTU(interfaceName string, mtu int) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("lookup interface %s: %w", interfaceName, err)
	}
	if err := netlink.LinkSetMTU(link, mtu); err != nil {
		return fmt.Errorf("set mtu %d on %s: %w", mtu, interfaceName, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Route management via netlink
// ---------------------------------------------------------------------------

// addLinuxEndpointRoutes adds host routes for WireGuard endpoint IPs through
// the current default gateway so that endpoint traffic bypasses the tunnel.
func addLinuxEndpointRoutes(ctx context.Context, endpointHosts []string) ([]routeSpec, error) {
	routes := resolveEndpointRoutes(ctx, endpointHosts)
	if len(routes) == 0 {
		return nil, nil
	}

	added := make([]routeSpec, 0, len(routes))
	for _, route := range routes {
		gwRoute, err := linuxDefaultGateway(route.family)
		if err != nil {
			continue
		}

		mask := net.CIDRMask(32, 32)
		if route.family == "inet6" {
			mask = net.CIDRMask(128, 128)
		}

		nlRoute := &netlink.Route{
			Dst: &net.IPNet{
				IP:   net.ParseIP(route.destination),
				Mask: mask,
			},
			Gw:        gwRoute.gw,
			LinkIndex: gwRoute.linkIndex,
		}
		if err := netlink.RouteAdd(nlRoute); err != nil && !errors.Is(err, os.ErrExist) {
			continue
		}
		added = append(added, route)
	}
	return added, nil
}

// removeLinuxEndpointRoutes removes previously added endpoint bypass routes.
func removeLinuxEndpointRoutes(routes []routeSpec) {
	for _, route := range routes {
		mask := net.CIDRMask(32, 32)
		if route.family == "inet6" {
			mask = net.CIDRMask(128, 128)
		}
		nlRoute := &netlink.Route{
			Dst: &net.IPNet{
				IP:   net.ParseIP(route.destination),
				Mask: mask,
			},
		}
		_ = netlink.RouteDel(nlRoute)
	}
}

// addLinuxAllowedIPRoutes adds routes for all WireGuard allowed-IP prefixes
// through the tunnel interface.
func addLinuxAllowedIPRoutes(interfaceName string, allowedIPs []string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return fmt.Errorf("lookup interface %s for routes: %w", interfaceName, err)
	}

	for _, prefix := range allowedIPs {
		routePrefixes, _, err := normalizedRoutesForPrefix(prefix)
		if err != nil {
			return err
		}
		for _, rp := range routePrefixes {
			_, dst, parseErr := net.ParseCIDR(rp)
			if parseErr != nil {
				return fmt.Errorf("parse route prefix %s: %w", rp, parseErr)
			}
			nlRoute := &netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       dst,
			}
			if err := netlink.RouteAdd(nlRoute); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("add route %s via %s: %w", rp, interfaceName, err)
			}
		}
	}
	return nil
}

// removeLinuxAllowedIPRoutes removes routes for allowed-IP prefixes.
func removeLinuxAllowedIPRoutes(interfaceName string, allowedIPs []string) {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return
	}
	for _, prefix := range allowedIPs {
		routePrefixes, _, err := normalizedRoutesForPrefix(prefix)
		if err != nil {
			continue
		}
		for _, rp := range routePrefixes {
			_, dst, parseErr := net.ParseCIDR(rp)
			if parseErr != nil {
				continue
			}
			_ = netlink.RouteDel(&netlink.Route{
				LinkIndex: link.Attrs().Index,
				Dst:       dst,
			})
		}
	}
}

type linuxGatewayInfo struct {
	gw        net.IP
	linkIndex int
}

// linuxDefaultGateway returns the current default gateway IP and link index
// for the given address family using netlink.
func linuxDefaultGateway(family string) (linuxGatewayInfo, error) {
	nlFamily := netlink.FAMILY_V4
	if family == "inet6" {
		nlFamily = netlink.FAMILY_V6
	}

	routes, err := netlink.RouteList(nil, nlFamily)
	if err != nil {
		return linuxGatewayInfo{}, fmt.Errorf("list routes: %w", err)
	}

	for _, route := range routes {
		if route.Dst != nil {
			continue // not a default route
		}
		if route.Gw != nil {
			return linuxGatewayInfo{gw: route.Gw, linkIndex: route.LinkIndex}, nil
		}
		if route.LinkIndex > 0 {
			return linuxGatewayInfo{linkIndex: route.LinkIndex}, nil
		}
	}
	return linuxGatewayInfo{}, errors.New("default gateway not found")
}

// deleteLinuxInterface removes the network interface via netlink.
func deleteLinuxInterface(interfaceName string) error {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return nil // already gone
	}
	return netlink.LinkDel(link)
}

// ---------------------------------------------------------------------------
// DNS management via D-Bus (systemd-resolved) or resolv.conf fallback
// ---------------------------------------------------------------------------

// applyLinuxDNSServers configures DNS servers for the tunnel interface.
// It first attempts systemd-resolved via D-Bus; if that fails, it falls
// back to editing /etc/resolv.conf.
func applyLinuxDNSServers(interfaceName string, dnsServers []string) (*linuxDNSOverride, error) {
	if strings.TrimSpace(interfaceName) == "" {
		return nil, errors.New("interface name required for DNS")
	}

	normalized := uniqueStringsPreserveOrder(dnsServers)
	if len(normalized) == 0 {
		return nil, nil
	}

	override, err := applyLinuxDNSResolved(interfaceName, normalized)
	if err == nil {
		return override, nil
	}

	override, fallbackErr := applyLinuxDNSResolvConf(normalized)
	if fallbackErr == nil {
		return override, nil
	}

	return nil, fmt.Errorf(
		"DNS setup failed via resolved (%v) and resolv.conf (%v)",
		err, fallbackErr,
	)
}

// applyLinuxDNSResolved sets DNS servers on the tunnel interface link via
// systemd-resolved's D-Bus API.
func applyLinuxDNSResolved(interfaceName string, dnsServers []string) (*linuxDNSOverride, error) {
	link, err := netlink.LinkByName(interfaceName)
	if err != nil {
		return nil, fmt.Errorf("lookup interface for resolved: %w", err)
	}
	ifIndex := link.Attrs().Index

	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to system dbus: %w", err)
	}
	defer conn.Close()

	resolved := conn.Object("org.freedesktop.resolve1", "/org/freedesktop/resolve1")

	// Build DNS server array: each entry is (family int32, address []byte).
	type dnsEntry struct {
		Family  int32
		Address []byte
	}
	entries := make([]dnsEntry, 0, len(dnsServers))
	for _, server := range dnsServers {
		ip := net.ParseIP(strings.TrimSpace(server))
		if ip == nil {
			continue
		}
		if v4 := ip.To4(); v4 != nil {
			entries = append(entries, dnsEntry{Family: 2, Address: v4}) // AF_INET
		} else {
			entries = append(entries, dnsEntry{Family: 10, Address: ip.To16()}) // AF_INET6
		}
	}
	if len(entries) == 0 {
		return nil, errors.New("no valid DNS server IPs")
	}

	call := resolved.Call("org.freedesktop.resolve1.Manager.SetLinkDNS", 0,
		int32(ifIndex), entries)
	if call.Err != nil {
		return nil, fmt.Errorf("SetLinkDNS: %w", call.Err)
	}

	// Set routing domain "~." so all queries go through this link.
	type domainEntry struct {
		Domain    string
		RoutingOnly bool
	}
	domains := []domainEntry{{Domain: ".", RoutingOnly: true}}
	call = resolved.Call("org.freedesktop.resolve1.Manager.SetLinkDomains", 0,
		int32(ifIndex), domains)
	if call.Err != nil {
		// Revert DNS on failure.
		_ = resolved.Call("org.freedesktop.resolve1.Manager.RevertLink", 0, int32(ifIndex))
		return nil, fmt.Errorf("SetLinkDomains: %w", call.Err)
	}

	return &linuxDNSOverride{
		mode:          linuxDNSModeResolvedLink,
		interfaceName: interfaceName,
	}, nil
}

// applyLinuxDNSResolvConf directly edits /etc/resolv.conf as a fallback.
func applyLinuxDNSResolvConf(dnsServers []string) (*linuxDNSOverride, error) {
	const resolvConfPath = "/etc/resolv.conf"

	override := &linuxDNSOverride{
		mode:           linuxDNSModeResolvConf,
		resolvConfPath: resolvConfPath,
		resolvConfMode: 0o644,
	}

	stat, statErr := os.Stat(resolvConfPath)
	switch {
	case statErr == nil:
		override.resolvConfHad = true
		override.resolvConfMode = stat.Mode().Perm()
		existing, readErr := os.ReadFile(resolvConfPath)
		if readErr != nil {
			return nil, fmt.Errorf("read %s: %w", resolvConfPath, readErr)
		}
		override.resolvConfData = existing
	case errors.Is(statErr, os.ErrNotExist):
		override.resolvConfHad = false
	default:
		return nil, fmt.Errorf("stat %s: %w", resolvConfPath, statErr)
	}

	rendered := renderResolvConf(override.resolvConfData, dnsServers)
	if err := os.WriteFile(resolvConfPath, rendered, override.resolvConfMode); err != nil {
		return nil, fmt.Errorf("write %s: %w", resolvConfPath, err)
	}

	return override, nil
}

func renderResolvConf(previous []byte, dnsServers []string) []byte {
	normalizedPrevious := strings.ReplaceAll(string(previous), "\r\n", "\n")
	lines := strings.Split(normalizedPrevious, "\n")

	preserved := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "nameserver ") {
			continue
		}
		if trimmed == "" {
			continue
		}
		preserved = append(preserved, line)
	}

	out := make([]string, 0, len(preserved)+len(dnsServers)+1)
	out = append(out, "# Managed by PangeaVPN")
	for _, server := range dnsServers {
		trimmed := strings.TrimSpace(server)
		if trimmed != "" {
			out = append(out, "nameserver "+trimmed)
		}
	}
	out = append(out, preserved...)
	return []byte(strings.Join(out, "\n") + "\n")
}

// restoreLinuxDNSServers reverses the DNS changes made by applyLinuxDNSServers.
func restoreLinuxDNSServers(override *linuxDNSOverride) error {
	if override == nil {
		return nil
	}

	switch override.mode {
	case linuxDNSModeResolvedLink:
		ifName := strings.TrimSpace(override.interfaceName)
		if ifName == "" {
			return errors.New("interface name required to revert resolved DNS")
		}
		link, err := netlink.LinkByName(ifName)
		if err != nil {
			return nil // interface already removed, resolved auto-reverts
		}
		conn, err := dbus.SystemBus()
		if err != nil {
			return fmt.Errorf("dbus connect for DNS revert: %w", err)
		}
		defer conn.Close()
		resolved := conn.Object("org.freedesktop.resolve1", "/org/freedesktop/resolve1")
		call := resolved.Call("org.freedesktop.resolve1.Manager.RevertLink", 0,
			int32(link.Attrs().Index))
		if call.Err != nil {
			return fmt.Errorf("RevertLink: %w", call.Err)
		}
		return nil

	case linuxDNSModeResolvConf:
		path := strings.TrimSpace(override.resolvConfPath)
		if path == "" {
			path = "/etc/resolv.conf"
		}
		if override.resolvConfHad {
			mode := override.resolvConfMode
			if mode == 0 {
				mode = 0o644
			}
			return os.WriteFile(path, override.resolvConfData, mode)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported DNS mode: %s", override.mode)
	}
}
