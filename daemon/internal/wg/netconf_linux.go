//go:build linux

package wg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// ---------------------------------------------------------------------------
// Policy routing constants (mirrors wg-quick behaviour)
// ---------------------------------------------------------------------------

const (
	// policyRoutingTable is the custom routing table ID for tunnel routes.
	policyRoutingTable = 51820
	// policyRoutingFwmark is the firewall mark used to let WireGuard's own
	// UDP packets bypass the policy rule and use the real default route.
	policyRoutingFwmark uint32 = 51820
	// policyRulePriority must be lower than the standard main-table rule
	// (32766) so our rules are evaluated first.
	policyRulePriority = 32764
	// suppressRulePriority for the suppress_prefixlength rule on main table.
	suppressRulePriority = 32765
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

// routeOwnership records whether this process created a bypass route's entry
// in each table (vs. finding it via EEXIST); only owned entries get deleted.
type routeOwnership struct {
	ownsMain   bool
	ownsPolicy bool
}

func routeOwnershipKey(route routeSpec) string {
	return route.family + "|" + route.destination
}

// addLinuxEndpointRoutes installs /32 bypass routes for cloak/hub IPs in both
// tables independently, so one table's failure never skips the other.
func addLinuxEndpointRoutes(ctx context.Context, endpointHosts []string) ([]routeSpec, map[string]routeOwnership, error) {
	routes, resolveErr := resolveEndpointRoutes(ctx, endpointHosts)
	if resolveErr != nil {
		return nil, nil, fmt.Errorf("resolve endpoint hosts: %w", resolveErr)
	}
	if len(routes) == 0 {
		return nil, nil, nil
	}

	added := make([]routeSpec, 0, len(routes))
	ownership := make(map[string]routeOwnership, len(routes))
	var errs []error
	for _, route := range routes {
		gwRoute, err := linuxDefaultGateway(route.family)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		mask := net.CIDRMask(32, 32)
		if route.family == "inet6" {
			mask = net.CIDRMask(128, 128)
		}
		dst := &net.IPNet{IP: net.ParseIP(route.destination), Mask: mask}

		mainRoute := &netlink.Route{Dst: dst, Gw: gwRoute.gw, LinkIndex: gwRoute.linkIndex}
		mainErr := netlink.RouteAdd(mainRoute)

		policyRoute := &netlink.Route{Dst: dst, Gw: gwRoute.gw, LinkIndex: gwRoute.linkIndex, Table: policyRoutingTable}
		policyErr := netlink.RouteAdd(policyRoute)

		mainOK := mainErr == nil || errors.Is(mainErr, os.ErrExist)
		policyOK := policyErr == nil || errors.Is(policyErr, os.ErrExist)
		if !mainOK && !policyOK {
			errs = append(errs, mainErr, policyErr)
			continue
		}

		added = append(added, route)
		ownership[routeOwnershipKey(route)] = routeOwnership{ownsMain: mainErr == nil, ownsPolicy: policyErr == nil}
	}
	return added, ownership, errors.Join(errs...)
}

// removeLinuxEndpointRoutes deletes only routes ownership marks as ours; a
// missing entry may belong to the host admin, another VPN, or a leaked session.
func removeLinuxEndpointRoutes(routes []routeSpec, ownership map[string]routeOwnership) {
	for _, route := range routes {
		mask := net.CIDRMask(32, 32)
		if route.family == "inet6" {
			mask = net.CIDRMask(128, 128)
		}
		dst := &net.IPNet{IP: net.ParseIP(route.destination), Mask: mask}

		own := ownership[routeOwnershipKey(route)]
		if own.ownsMain {
			_ = netlink.RouteDel(&netlink.Route{Dst: dst})
		}
		if own.ownsPolicy {
			_ = netlink.RouteDel(&netlink.Route{Dst: dst, Table: policyRoutingTable})
		}
	}
}

// Policy routing mirrors wg-quick's table 51820 + fwmark + suppress rule.

// The table and its ip rules are shared kernel state: two tunnel sessions in
// the same daemon reuse them, so refcounts gate installing/tearing them down.
var (
	linuxPolicyMu       sync.Mutex
	linuxPolicyRefCount int

	linuxPolicyRouteMu   sync.Mutex
	linuxPolicyRouteRefs = map[string]int{}
)

// addLinuxPolicyRouting sets up the custom routing table, routes, and ip rules.
func addLinuxPolicyRouting(interfaceName string, allowedIPs []string) error {
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
				Table:     policyRoutingTable,
			}
			if err := netlink.RouteAdd(nlRoute); err != nil && !errors.Is(err, os.ErrExist) {
				return fmt.Errorf("add route %s to table %d: %w", rp, policyRoutingTable, err)
			}
			incrementLinuxPolicyRouteRef(rp)
		}
	}

	linuxPolicyMu.Lock()
	defer linuxPolicyMu.Unlock()
	if linuxPolicyRefCount == 0 {
		if err := installLinuxPolicyRules(); err != nil {
			return err
		}
	}
	linuxPolicyRefCount++
	return nil
}

// removeLinuxPolicyRouting releases this session's share of the shared table
// and rules, tearing them down only once no session still needs them.
func removeLinuxPolicyRouting(_ string, allowedIPs []string) {
	removeLinuxSessionPolicyRoutes(allowedIPs)

	linuxPolicyMu.Lock()
	defer linuxPolicyMu.Unlock()
	if linuxPolicyRefCount > 0 {
		linuxPolicyRefCount--
	}
	if linuxPolicyRefCount > 0 {
		return
	}
	removeLinuxPolicyRules()
	flushPolicyRoutingTable()
}

// removeLinuxSessionPolicyRoutes deletes a prefix only once this is the last
// session still holding a reference to it.
func removeLinuxSessionPolicyRoutes(allowedIPs []string) {
	for _, prefix := range allowedIPs {
		routePrefixes, _, err := normalizedRoutesForPrefix(prefix)
		if err != nil {
			continue
		}
		for _, rp := range routePrefixes {
			if !decrementLinuxPolicyRouteRef(rp) {
				continue
			}
			_, dst, parseErr := net.ParseCIDR(rp)
			if parseErr != nil {
				continue
			}
			_ = netlink.RouteDel(&netlink.Route{Dst: dst, Table: policyRoutingTable})
		}
	}
}

func incrementLinuxPolicyRouteRef(prefix string) {
	linuxPolicyRouteMu.Lock()
	defer linuxPolicyRouteMu.Unlock()
	linuxPolicyRouteRefs[prefix]++
}

// decrementLinuxPolicyRouteRef reports whether the caller just released the
// last reference to prefix, meaning it is now safe to delete from the kernel.
func decrementLinuxPolicyRouteRef(prefix string) bool {
	linuxPolicyRouteMu.Lock()
	defer linuxPolicyRouteMu.Unlock()
	n, held := linuxPolicyRouteRefs[prefix]
	if !held || n <= 0 {
		return false
	}
	if n == 1 {
		delete(linuxPolicyRouteRefs, prefix)
		return true
	}
	linuxPolicyRouteRefs[prefix] = n - 1
	return false
}

// installLinuxPolicyRules adds the shared ip rules, plus an IPv6 blackhole
// since AllowedIPs is v4-only and v6 must not silently bypass the tunnel.
func installLinuxPolicyRules() error {
	fwmask := uint32(policyRoutingFwmark)

	notRule := netlink.NewRule()
	notRule.Invert = true
	notRule.Mark = policyRoutingFwmark
	notRule.Mask = &fwmask
	notRule.Table = policyRoutingTable
	notRule.Priority = policyRulePriority
	notRule.Family = unix.AF_INET
	if err := netlink.RuleAdd(notRule); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("add fwmark ip rule: %w", err)
	}

	suppressRule := netlink.NewRule()
	suppressRule.Table = unix.RT_TABLE_MAIN
	suppressRule.SuppressPrefixlen = 0
	suppressRule.Priority = suppressRulePriority
	suppressRule.Family = unix.AF_INET
	if err := netlink.RuleAdd(suppressRule); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("add suppress ip rule: %w", err)
	}

	blackhole6 := &netlink.Route{
		Table: policyRoutingTable,
		Dst:   &net.IPNet{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
		Type:  unix.RTN_UNREACHABLE,
	}
	if err := netlink.RouteAdd(blackhole6); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("add ipv6 blackhole route: %w", err)
	}

	notRule6 := netlink.NewRule()
	notRule6.Invert = true
	notRule6.Mark = policyRoutingFwmark
	notRule6.Mask = &fwmask
	notRule6.Table = policyRoutingTable
	notRule6.Priority = policyRulePriority
	notRule6.Family = unix.AF_INET6
	if err := netlink.RuleAdd(notRule6); err != nil && !errors.Is(err, os.ErrExist) {
		return fmt.Errorf("add ipv6 fwmark ip rule: %w", err)
	}

	return nil
}

// removeLinuxPolicyRules removes the shared ip rules. Must only be called
// once no session still relies on them.
func removeLinuxPolicyRules() {
	fwmask := uint32(policyRoutingFwmark)

	notRule := netlink.NewRule()
	notRule.Invert = true
	notRule.Mark = policyRoutingFwmark
	notRule.Mask = &fwmask
	notRule.Table = policyRoutingTable
	notRule.Priority = policyRulePriority
	notRule.Family = unix.AF_INET
	_ = netlink.RuleDel(notRule)

	suppressRule := netlink.NewRule()
	suppressRule.Table = unix.RT_TABLE_MAIN
	suppressRule.SuppressPrefixlen = 0
	suppressRule.Priority = suppressRulePriority
	suppressRule.Family = unix.AF_INET
	_ = netlink.RuleDel(suppressRule)

	notRule6 := netlink.NewRule()
	notRule6.Invert = true
	notRule6.Mark = policyRoutingFwmark
	notRule6.Mask = &fwmask
	notRule6.Table = policyRoutingTable
	notRule6.Priority = policyRulePriority
	notRule6.Family = unix.AF_INET6
	_ = netlink.RuleDel(notRule6)
}

// flushPolicyRoutingTable deletes every route in table 51820, v4 and v6,
// once the last session releases it.
func flushPolicyRoutingTable() {
	for _, family := range []int{netlink.FAMILY_V4, netlink.FAMILY_V6} {
		filter := &netlink.Route{Table: policyRoutingTable}
		routes, err := netlink.RouteListFiltered(family, filter, netlink.RT_FILTER_TABLE)
		if err != nil {
			continue
		}
		for i := range routes {
			_ = netlink.RouteDel(&routes[i])
		}
	}
	linuxPolicyRouteMu.Lock()
	linuxPolicyRouteRefs = map[string]int{}
	linuxPolicyRouteMu.Unlock()
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
		if !isDefaultDst(route.Dst) {
			continue
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

// isDefaultDst reports whether dst is 0.0.0.0/0 or ::/0; netlink returns
// either nil or an explicit zero CIDR depending on kernel/library version.
func isDefaultDst(dst *net.IPNet) bool {
	if dst == nil {
		return true
	}
	ones, _ := dst.Mask.Size()
	return ones == 0 && dst.IP.IsUnspecified()
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

// linuxResolvSymlinkBackup records whether /etc/resolv.conf was a symlink and
// where it pointed; tracked in linuxExtra since linuxDNSOverride has no field.
type linuxResolvSymlinkBackup struct {
	wasSymlink bool
	target     string
}

// applyLinuxDNSServers tries systemd-resolved via D-Bus first, falling back
// to editing /etc/resolv.conf.
func applyLinuxDNSServers(interfaceName string, dnsServers []string) (*linuxDNSOverride, linuxResolvSymlinkBackup, error) {
	if strings.TrimSpace(interfaceName) == "" {
		return nil, linuxResolvSymlinkBackup{}, errors.New("interface name required for DNS")
	}

	normalized := uniqueStringsPreserveOrder(dnsServers)
	if len(normalized) == 0 {
		return nil, linuxResolvSymlinkBackup{}, nil
	}

	override, err := applyLinuxDNSResolved(interfaceName, normalized)
	if err == nil {
		return override, linuxResolvSymlinkBackup{}, nil
	}

	override, backup, fallbackErr := applyLinuxDNSResolvConf(normalized)
	if fallbackErr == nil {
		return override, backup, nil
	}

	return nil, linuxResolvSymlinkBackup{}, fmt.Errorf(
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
		Domain      string
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

// applyLinuxDNSResolvConf replaces /etc/resolv.conf via rename rather than
// following a generator's symlink (systemd-resolved, resolvconf) through to its target.
func applyLinuxDNSResolvConf(dnsServers []string) (*linuxDNSOverride, linuxResolvSymlinkBackup, error) {
	const resolvConfPath = "/etc/resolv.conf"

	override := &linuxDNSOverride{
		mode:           linuxDNSModeResolvConf,
		resolvConfPath: resolvConfPath,
		resolvConfMode: 0o644,
	}
	var backup linuxResolvSymlinkBackup

	lst, lstatErr := os.Lstat(resolvConfPath)
	switch {
	case lstatErr == nil && lst.Mode()&os.ModeSymlink != 0:
		target, readErr := os.Readlink(resolvConfPath)
		if readErr != nil {
			return nil, backup, fmt.Errorf("readlink %s: %w", resolvConfPath, readErr)
		}
		override.resolvConfHad = true
		backup.wasSymlink = true
		backup.target = target
		if existing, readErr := os.ReadFile(resolvConfPath); readErr == nil {
			override.resolvConfData = existing
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return nil, backup, fmt.Errorf("read %s: %w", resolvConfPath, readErr)
		}
	case lstatErr == nil:
		override.resolvConfHad = true
		override.resolvConfMode = lst.Mode().Perm()
		existing, readErr := os.ReadFile(resolvConfPath)
		if readErr != nil {
			return nil, backup, fmt.Errorf("read %s: %w", resolvConfPath, readErr)
		}
		override.resolvConfData = existing
	case errors.Is(lstatErr, os.ErrNotExist):
		override.resolvConfHad = false
	default:
		return nil, backup, fmt.Errorf("lstat %s: %w", resolvConfPath, lstatErr)
	}

	rendered := renderResolvConf(override.resolvConfData, dnsServers)
	if err := atomicReplaceFile(resolvConfPath, rendered, override.resolvConfMode); err != nil {
		return nil, backup, fmt.Errorf("write %s: %w", resolvConfPath, err)
	}

	return override, backup, nil
}

// atomicReplaceFile renames a temp file over path, replacing the directory
// entry itself rather than following a symlink the way os.WriteFile does.
func atomicReplaceFile(path string, data []byte, mode os.FileMode) error {
	if mode == 0 {
		mode = 0o644
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".resolv.conf.pangea-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()

	_, writeErr := tmp.Write(data)
	closeErr := tmp.Close()
	if writeErr != nil {
		os.Remove(tmpPath)
		return writeErr
	}
	if closeErr != nil {
		os.Remove(tmpPath)
		return closeErr
	}
	if err := os.Chmod(tmpPath, mode); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
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
func restoreLinuxDNSServers(override *linuxDNSOverride, backup linuxResolvSymlinkBackup) error {
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

		if backup.wasSymlink {
			// Put the generator's symlink back rather than writing stale
			// captured content over whatever it has since regenerated.
			_ = os.Remove(path)
			if backup.target == "" {
				return nil
			}
			if err := os.Symlink(backup.target, path); err != nil {
				return fmt.Errorf("restore resolv.conf symlink: %w", err)
			}
			return nil
		}

		if override.resolvConfHad {
			mode := override.resolvConfMode
			if mode == 0 {
				mode = 0o644
			}
			return atomicReplaceFile(path, override.resolvConfData, mode)
		}

		// Nothing existed before. Lstat first: if a generator has since put
		// its own symlink back, it is not ours to remove.
		info, statErr := os.Lstat(path)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return nil
			}
			return fmt.Errorf("lstat %s: %w", path, statErr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", path, err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported DNS mode: %s", override.mode)
	}
}

// ensureSessionDNS is Windows-only: Linux DNS state (resolved link, or a
// file we own) holds until something else rewrites it, so nothing to re-assert.
func ensureSessionDNS(_ *tunnelSession, _ []string) (bool, error) {
	return false, nil
}

// linuxBypassGateway picks a non-tunnel default gateway, accepting a
// link-scope one with no gateway address to match linuxDefaultGateway.
func linuxBypassGateway(tunnelIndex int) (linuxGatewayInfo, bool, error) {
	routes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return linuxGatewayInfo{}, false, fmt.Errorf("list routes: %w", err)
	}

	var best linuxGatewayInfo
	found := false
	for _, route := range routes {
		if !isDefaultDst(route.Dst) || route.LinkIndex == tunnelIndex || route.LinkIndex <= 0 {
			continue
		}
		if route.Gw != nil && route.Gw.IsUnspecified() {
			continue
		}

		candidate := linuxGatewayInfo{linkIndex: route.LinkIndex}
		if route.Gw != nil {
			candidate.gw = route.Gw
		}
		if !found || candidate.linkIndex < best.linkIndex {
			best = candidate
			found = true
		}
	}
	return best, found, nil
}

// gatewaysEqual compares two possibly-nil gateway addresses; nil means a
// link-scope default with no gateway of its own.
func gatewaysEqual(a, b net.IP) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.Equal(b)
}

// ensureLinuxBypassRoute points one table's host route at gateway. It uses
// RouteReplace (one netlink message) so there is no delete/add gap.
func ensureLinuxBypassRoute(destination *net.IPNet, gateway linuxGatewayInfo, table int) (bool, error) {
	filter := &netlink.Route{Dst: destination, Table: table}
	existing, err := netlink.RouteListFiltered(netlink.FAMILY_V4, filter, netlink.RT_FILTER_DST|netlink.RT_FILTER_TABLE)
	if err != nil {
		return false, fmt.Errorf("list route %s in table %d: %w", destination.String(), table, err)
	}
	for _, route := range existing {
		if route.LinkIndex == gateway.linkIndex && gatewaysEqual(route.Gw, gateway.gw) {
			return false, nil
		}
	}

	replacement := &netlink.Route{Dst: destination, Gw: gateway.gw, LinkIndex: gateway.linkIndex, Table: table}
	if err := netlink.RouteReplace(replacement); err != nil {
		return false, fmt.Errorf("re-pin route %s in table %d: %w", destination.String(), table, err)
	}
	return true, nil
}

// ensureSessionEndpointRoutes re-pins the endpoint bypass routes to the host's
// current default gateway in both tables, reporting whether it repaired anything.
func ensureSessionEndpointRoutes(_ context.Context, session *tunnelSession, _ map[uint64]struct{}) (bool, error) {
	if session == nil || len(session.endpointRoutes) == 0 {
		return false, nil
	}

	tunnelIndex := 0
	if link, err := netlink.LinkByName(session.interfaceName); err == nil {
		tunnelIndex = link.Attrs().Index
	}
	// No off-tunnel gateway to pin to right now: mid-roam, or the link is down.
	// A quiet skip leaves it for a later tick instead of logging every 3s.
	gateway, ok, err := linuxBypassGateway(tunnelIndex)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	repaired := false
	var errs []error
	for _, endpointRoute := range session.endpointRoutes {
		if endpointRoute.family != "inet" {
			continue
		}
		ip := net.ParseIP(endpointRoute.destination)
		if ip == nil {
			continue
		}
		destination := &net.IPNet{IP: ip, Mask: net.CIDRMask(32, 32)}

		for _, table := range []int{unix.RT_TABLE_MAIN, policyRoutingTable} {
			changed, err := ensureLinuxBypassRoute(destination, gateway, table)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			repaired = repaired || changed
		}
	}

	return repaired, errors.Join(errs...)
}

// Crash recovery: persist pre-state so a fresh daemon can undo it.

type linuxPersistedRoute struct {
	Family      string `json:"family"`
	Destination string `json:"destination"`
	OwnsMain    bool   `json:"ownsMain"`
	OwnsPolicy  bool   `json:"ownsPolicy"`
}

type linuxPersistedDNS struct {
	Mode                    string `json:"mode"`
	InterfaceName           string `json:"interfaceName"`
	ResolvConfPath          string `json:"resolvConfPath"`
	ResolvConfData          []byte `json:"resolvConfData"`
	ResolvConfMode          uint32 `json:"resolvConfMode"`
	ResolvConfHad           bool   `json:"resolvConfHad"`
	ResolvConfWasSymlink    bool   `json:"resolvConfWasSymlink"`
	ResolvConfSymlinkTarget string `json:"resolvConfSymlinkTarget"`
}

func newLinuxPersistedDNS(override *linuxDNSOverride, backup linuxResolvSymlinkBackup) *linuxPersistedDNS {
	if override == nil {
		return nil
	}
	return &linuxPersistedDNS{
		Mode:                    string(override.mode),
		InterfaceName:           override.interfaceName,
		ResolvConfPath:          override.resolvConfPath,
		ResolvConfData:          override.resolvConfData,
		ResolvConfMode:          uint32(override.resolvConfMode),
		ResolvConfHad:           override.resolvConfHad,
		ResolvConfWasSymlink:    backup.wasSymlink,
		ResolvConfSymlinkTarget: backup.target,
	}
}

func (d *linuxPersistedDNS) toOverride() (*linuxDNSOverride, linuxResolvSymlinkBackup) {
	override := &linuxDNSOverride{
		mode:           linuxDNSMode(d.Mode),
		interfaceName:  d.InterfaceName,
		resolvConfPath: d.ResolvConfPath,
		resolvConfData: d.ResolvConfData,
		resolvConfMode: os.FileMode(d.ResolvConfMode),
		resolvConfHad:  d.ResolvConfHad,
	}
	backup := linuxResolvSymlinkBackup{wasSymlink: d.ResolvConfWasSymlink, target: d.ResolvConfSymlinkTarget}
	return override, backup
}

type linuxPersistedSession struct {
	InterfaceName  string                `json:"interfaceName"`
	AllowedIPs     []string              `json:"allowedIPs"`
	EndpointRoutes []linuxPersistedRoute `json:"endpointRoutes"`
	DNS            *linuxPersistedDNS    `json:"dns,omitempty"`
}

func newLinuxPersistedSession(interfaceName string, allowedIPs []string, endpointRoutes []routeSpec, ownership map[string]routeOwnership, dnsOverride *linuxDNSOverride, backup linuxResolvSymlinkBackup) *linuxPersistedSession {
	routes := make([]linuxPersistedRoute, 0, len(endpointRoutes))
	for _, r := range endpointRoutes {
		own := ownership[routeOwnershipKey(r)]
		routes = append(routes, linuxPersistedRoute{
			Family: r.family, Destination: r.destination,
			OwnsMain: own.ownsMain, OwnsPolicy: own.ownsPolicy,
		})
	}
	return &linuxPersistedSession{
		InterfaceName:  interfaceName,
		AllowedIPs:     allowedIPs,
		EndpointRoutes: routes,
		DNS:            newLinuxPersistedDNS(dnsOverride, backup),
	}
}

func linuxSessionStateDir() (string, error) {
	dir, err := platform.AppSupportDir()
	if err != nil {
		return "", err
	}
	stateDir := filepath.Join(dir, "wg-linux-sessions")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return "", fmt.Errorf("create linux session state dir: %w", err)
	}
	return stateDir, nil
}

func linuxSessionStateFile(tunnelKey string) (string, error) {
	dir, err := linuxSessionStateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, tunnelKey+".json"), nil
}

// persistLinuxSessionState writes the network pre-state to disk so a fresh
// daemon can undo it if this process dies before Stop runs.
func persistLinuxSessionState(tunnelKey string, session *linuxPersistedSession) error {
	path, err := linuxSessionStateFile(tunnelKey)
	if err != nil {
		return err
	}
	data, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal linux session state: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// clearLinuxSessionState removes the persisted pre-state after a normal stop
// or a completed startup restore.
func clearLinuxSessionState(tunnelKey string) {
	path, err := linuxSessionStateFile(tunnelKey)
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

// restoreOrphanedLinuxNetworkState undoes network changes left behind by a
// daemon process that died before Stop ran, using state persisted at Start.
func restoreOrphanedLinuxNetworkState() {
	dir, err := linuxSessionStateDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) == 0 {
		return
	}

	recovered := false
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			continue
		}
		var persisted linuxPersistedSession
		if err := json.Unmarshal(data, &persisted); err != nil {
			_ = os.Remove(path)
			continue
		}
		recovered = true

		if persisted.DNS != nil {
			override, backup := persisted.DNS.toOverride()
			_ = restoreLinuxDNSServers(override, backup)
		}

		routes := make([]routeSpec, 0, len(persisted.EndpointRoutes))
		ownership := make(map[string]routeOwnership, len(persisted.EndpointRoutes))
		for _, r := range persisted.EndpointRoutes {
			route := routeSpec{family: r.Family, destination: r.Destination}
			routes = append(routes, route)
			ownership[routeOwnershipKey(route)] = routeOwnership{ownsMain: r.OwnsMain, ownsPolicy: r.OwnsPolicy}
		}
		removeLinuxEndpointRoutes(routes, ownership)

		_ = os.Remove(path)
	}

	// A fresh process owns no live sessions, so a full teardown of the
	// shared rules/table is always correct here, independent of ref counts.
	if recovered {
		removeLinuxPolicyRules()
		flushPolicyRoutingTable()
	}
}
