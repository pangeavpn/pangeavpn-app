//go:build darwin

package wg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/route"
)

// darwinRouteTimeout bounds one route(8) call made from the health tick.
const darwinRouteTimeout = 5 * time.Second

// ---------------------------------------------------------------------------
// Interface configuration via ifconfig (non-cgo)
// ---------------------------------------------------------------------------

// configureDarwinAddresses assigns CIDR addresses to the named interface.
func configureDarwinAddresses(interfaceName string, addresses []string) error {
	for _, cidr := range addresses {
		ip, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return fmt.Errorf("parse address %s: %w", cidr, err)
		}

		if v4 := ip.To4(); v4 != nil {
			mask := net.IP(ipNet.Mask).To4()
			// utun is point-to-point: ifconfig <iface> inet <addr> <addr> netmask <mask>
			out, err := exec.Command("ifconfig", interfaceName,
				"inet", v4.String(), v4.String(),
				"netmask", mask.String(),
			).CombinedOutput()
			if err != nil {
				return fmt.Errorf("add ipv4 address %s on %s: %w (%s)", cidr, interfaceName, err, strings.TrimSpace(string(out)))
			}
		} else {
			ones, _ := ipNet.Mask.Size()
			out, err := exec.Command("ifconfig", interfaceName,
				"inet6", ip.String(),
				"prefixlen", fmt.Sprintf("%d", ones),
			).CombinedOutput()
			if err != nil {
				return fmt.Errorf("add ipv6 address %s on %s: %w (%s)", cidr, interfaceName, err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

// bringDarwinInterfaceUp sets the interface to UP state.
func bringDarwinInterfaceUp(interfaceName string) error {
	out, err := exec.Command("ifconfig", interfaceName, "up").CombinedOutput()
	if err != nil {
		return fmt.Errorf("bring up %s: %w (%s)", interfaceName, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Route management via route(8)
// ---------------------------------------------------------------------------

// darwinDefaultGatewayV4 returns the IPv4 default gateway address.
func darwinDefaultGatewayV4() (string, error) {
	out, err := exec.Command("route", "-n", "get", "default").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("query default gateway: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "gateway:") {
			gw := strings.TrimSpace(strings.TrimPrefix(line, "gateway:"))
			if gw != "" {
				return gw, nil
			}
		}
	}
	return "", errors.New("default ipv4 gateway not found")
}

// addDarwinEndpointRoutes adds host routes for WireGuard endpoint IPs
// through the default gateway so endpoint traffic bypasses the tunnel.
func addDarwinEndpointRoutes(ctx context.Context, endpointHosts []string) ([]routeSpec, error) {
	routes := resolveEndpointRoutes(ctx, endpointHosts)
	if len(routes) == 0 {
		return nil, nil
	}

	gw, gwErr := darwinDefaultGatewayV4()
	added := make([]routeSpec, 0, len(routes))
	for _, route := range routes {
		if route.family == "inet6" {
			continue // IPv6 endpoint routes not supported via this path
		}
		if gwErr != nil {
			continue
		}

		out, err := exec.Command("route", "-n", "add", "-host", route.destination, "-gateway", gw).CombinedOutput()
		if err != nil {
			// Route may already exist; not fatal.
			_ = out
			continue
		}
		added = append(added, route)
	}
	return added, nil
}

// removeDarwinEndpointRoutes removes previously added endpoint bypass routes.
func removeDarwinEndpointRoutes(routes []routeSpec) {
	for _, route := range routes {
		if route.family == "inet6" {
			continue
		}
		_ = exec.Command("route", "-n", "delete", "-host", route.destination).Run()
	}
}

// addDarwinAllowedIPRoutes adds routes for WireGuard allowed-IP prefixes
// through the tunnel interface.
func addDarwinAllowedIPRoutes(interfaceName string, allowedIPs []string) error {
	for _, prefix := range allowedIPs {
		routePrefixes, family, err := normalizedRoutesForPrefix(prefix)
		if err != nil {
			return err
		}
		if family == "inet6" {
			continue // IPv6 tunnel routes not supported via this path yet
		}
		for _, rp := range routePrefixes {
			_, ipNet, parseErr := net.ParseCIDR(rp)
			if parseErr != nil {
				return fmt.Errorf("parse route prefix %s: %w", rp, parseErr)
			}

			mask := net.IP(ipNet.Mask).To4()
			out, err := exec.Command("route", "-n", "add",
				"-net", ipNet.IP.String(),
				"-netmask", mask.String(),
				"-interface", interfaceName,
			).CombinedOutput()
			if err != nil {
				return fmt.Errorf("add route %s via %s: %w (%s)", rp, interfaceName, err, strings.TrimSpace(string(out)))
			}
		}
	}
	return nil
}

// removeDarwinAllowedIPRoutes removes allowed-IP routes.
func removeDarwinAllowedIPRoutes(allowedIPs []string) {
	for _, prefix := range allowedIPs {
		routePrefixes, family, err := normalizedRoutesForPrefix(prefix)
		if err != nil || family == "inet6" {
			continue
		}
		for _, rp := range routePrefixes {
			_, ipNet, parseErr := net.ParseCIDR(rp)
			if parseErr != nil {
				continue
			}
			mask := net.IP(ipNet.Mask).To4()
			_ = exec.Command("route", "-n", "delete",
				"-net", ipNet.IP.String(),
				"-netmask", mask.String(),
			).Run()
		}
	}
}

// ---------------------------------------------------------------------------
// DNS management via networksetup (non-cgo)
// ---------------------------------------------------------------------------

// listDarwinNetworkServices returns all non-hardware-port network service names.
func listDarwinNetworkServices() ([]string, error) {
	out, err := exec.Command("networksetup", "-listallnetworkservices").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("list network services: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	var services []string
	scanner := bufio.NewScanner(bytes.NewReader(out))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip the header line and disabled services (prefixed with *).
		if line == "" || strings.HasPrefix(line, "An asterisk") || strings.HasPrefix(line, "*") {
			continue
		}
		services = append(services, line)
	}
	return services, nil
}

// getDarwinDNSServers returns the current DNS servers for a network service.
// Returns nil if DNS is set to automatic/DHCP.
func getDarwinDNSServers(serviceName string) []string {
	out, err := exec.Command("networksetup", "-getdnsservers", serviceName).CombinedOutput()
	if err != nil {
		return nil
	}

	trimmed := strings.TrimSpace(string(out))
	// "There aren't any DNS Servers set on ..." means DHCP/automatic.
	if strings.Contains(strings.ToLower(trimmed), "aren't any") || trimmed == "" {
		return nil
	}

	var servers []string
	for _, line := range strings.Split(trimmed, "\n") {
		server := strings.TrimSpace(line)
		if server != "" && net.ParseIP(server) != nil {
			servers = append(servers, server)
		}
	}
	return servers
}

// applyDarwinDNSServers sets DNS servers on all active network services and
// returns override state for later restoration.
func applyDarwinDNSServers(dnsServers []string) ([]darwinDNSOverride, error) {
	if len(dnsServers) == 0 {
		return nil, nil
	}

	services, err := listDarwinNetworkServices()
	if err != nil {
		return nil, err
	}
	if len(services) == 0 {
		return nil, errors.New("no network services available for DNS")
	}

	overrides := make([]darwinDNSOverride, 0, len(services))
	for _, svc := range services {
		origDNS := getDarwinDNSServers(svc)

		args := append([]string{"-setdnsservers", svc}, dnsServers...)
		out, err := exec.Command("networksetup", args...).CombinedOutput()
		if err != nil {
			// Roll back any overrides already applied.
			_ = restoreDarwinDNSServers(overrides)
			return nil, fmt.Errorf("set DNS for service %s: %w (%s)", svc, err, strings.TrimSpace(string(out)))
		}

		overrides = append(overrides, darwinDNSOverride{
			service:    svc,
			dnsServers: origDNS,
		})
	}

	return overrides, nil
}

// restoreDarwinDNSServers restores original DNS settings for all overridden services.
func restoreDarwinDNSServers(overrides []darwinDNSOverride) error {
	if len(overrides) == 0 {
		return nil
	}

	var failures []string
	for _, override := range overrides {
		var args []string
		if len(override.dnsServers) == 0 {
			// Restore to automatic/DHCP.
			args = []string{"-setdnsservers", override.service, "empty"}
		} else {
			args = append([]string{"-setdnsservers", override.service}, override.dnsServers...)
		}

		out, err := exec.Command("networksetup", args...).CombinedOutput()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v (%s)", override.service, err, strings.TrimSpace(string(out))))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("failed restoring DNS on services: %s", strings.Join(failures, ", "))
	}
	return nil
}

// ensureSessionDNS is Windows-only for now: there, interface DNS belongs to
// whoever wrote last, so it has to be re-asserted. macOS holds the per-service
// overrides applied at bring-up until they are restored.
func ensureSessionDNS(_ *tunnelSession, _ []string) (bool, error) {
	return false, nil
}

// darwinRouteEntry is one IPv4 route as the kernel reports it.
type darwinRouteEntry struct {
	destination netip.Addr
	maskBits    int
	gateway     netip.Addr
	ifIndex     int
	host        bool
	viaGateway  bool
}

// darwinIPv4Routes reads the kernel's IPv4 routing table.
//
// The table is scanned rather than asking `route get` which entry would be
// used, because the tunnel's own 0.0.0.0/1 covers the addresses such a lookup
// would be made against — including 0.0.0.0 itself. Matching prefixes here
// keeps the default route and the tunnel's half-default distinguishable.
func darwinIPv4Routes() ([]darwinRouteEntry, error) {
	rib, err := route.FetchRIB(syscall.AF_INET, route.RIBTypeRoute, 0)
	if err != nil {
		return nil, fmt.Errorf("read routing table: %w", err)
	}
	messages, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return nil, fmt.Errorf("parse routing table: %w", err)
	}

	entries := make([]darwinRouteEntry, 0, len(messages))
	for _, message := range messages {
		routeMessage, ok := message.(*route.RouteMessage)
		if !ok || routeMessage.Flags&syscall.RTF_UP == 0 {
			continue
		}
		destination, ok := darwinInet4Addr(routeMessage.Addrs, syscall.RTAX_DST)
		if !ok {
			continue
		}

		entry := darwinRouteEntry{
			destination: destination,
			maskBits:    darwinMaskBits(routeMessage.Addrs),
			ifIndex:     routeMessage.Index,
			host:        routeMessage.Flags&syscall.RTF_HOST != 0,
			viaGateway:  routeMessage.Flags&syscall.RTF_GATEWAY != 0,
		}
		if gateway, ok := darwinInet4Addr(routeMessage.Addrs, syscall.RTAX_GATEWAY); ok {
			entry.gateway = gateway
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func darwinInet4Addr(addrs []route.Addr, index int) (netip.Addr, bool) {
	if index >= len(addrs) {
		return netip.Addr{}, false
	}
	addr, ok := addrs[index].(*route.Inet4Addr)
	if !ok {
		return netip.Addr{}, false
	}
	return netip.AddrFrom4(addr.IP), true
}

// darwinMaskBits reads a prefix length, treating an absent mask as zero. Host
// routes carry no mask and are identified by their flag instead.
func darwinMaskBits(addrs []route.Addr) int {
	mask, ok := darwinInet4Addr(addrs, syscall.RTAX_NETMASK)
	if !ok {
		return 0
	}
	ones, _ := net.IPMask(mask.AsSlice()).Size()
	return ones
}

// darwinDefaultGateway picks the next hop the bypass should hang off: a real
// 0.0.0.0/0 with a usable gateway address, never the tunnel's own.
//
// Interface-scoped defaults are skipped because they have no address to pin to,
// which is also what a tunnel's half-default looks like. Ties break on the
// lower interface index so repeated calls agree and the guard cannot re-pin
// back and forth.
func darwinDefaultGateway(entries []darwinRouteEntry, tunnelIndex int) (netip.Addr, bool) {
	var best netip.Addr
	bestIndex := 0
	found := false

	for _, entry := range entries {
		if entry.host || entry.maskBits != 0 || !entry.destination.IsUnspecified() {
			continue
		}
		if entry.ifIndex == tunnelIndex || !entry.viaGateway {
			continue
		}
		if !entry.gateway.IsValid() || entry.gateway.IsUnspecified() {
			continue
		}
		if !found || entry.ifIndex < bestIndex {
			best, bestIndex, found = entry.gateway, entry.ifIndex, true
		}
	}
	return best, found
}

func darwinHostRoute(entries []darwinRouteEntry, destination netip.Addr) (darwinRouteEntry, bool) {
	for _, entry := range entries {
		if entry.host && entry.destination == destination {
			return entry, true
		}
	}
	return darwinRouteEntry{}, false
}

// endpointRouteNeedsRepair reports whether the bypass has stopped doing its
// job. An on-link entry counts as healthy: the endpoint is directly reachable,
// so it needs no gateway and re-pinning one would fight the kernel's own ARP
// entry every tick.
func endpointRouteNeedsRepair(current darwinRouteEntry, found bool, tunnelIndex int, gateway netip.Addr) bool {
	if !found {
		return true
	}
	if current.ifIndex == tunnelIndex {
		return true
	}
	if !current.viaGateway {
		return false
	}
	return current.gateway != gateway
}

// ensureSessionEndpointRoutes re-pins the endpoint bypass routes to the host's
// current default gateway, reporting whether it had to repair anything.
//
// macOS drops an interface's routes when the link goes down, and a roam or DHCP
// renewal moves the gateway out from under them. Either way the endpoint falls
// back to matching the tunnel's own 0.0.0.0/1 and WireGuard ends up routed into
// the tunnel it is trying to establish.
func ensureSessionEndpointRoutes(ctx context.Context, session *tunnelSession, _ map[uint64]struct{}) (bool, error) {
	if session == nil || len(session.endpointRoutes) == 0 {
		return false, nil
	}

	entries, err := darwinIPv4Routes()
	if err != nil {
		return false, err
	}

	tunnelIndex := 0
	if iface, err := net.InterfaceByName(session.interfaceName); err == nil {
		tunnelIndex = iface.Index
	}
	// No off-tunnel gateway to pin to right now: mid-roam, or the link is down.
	// A quiet skip leaves it for a later tick instead of logging every 3s.
	gateway, ok := darwinDefaultGateway(entries, tunnelIndex)
	if !ok {
		return false, nil
	}

	repaired := false
	var errs []error
	for _, endpointRoute := range session.endpointRoutes {
		if endpointRoute.family != "inet" {
			continue
		}
		destination, err := netip.ParseAddr(endpointRoute.destination)
		if err != nil {
			continue
		}

		current, found := darwinHostRoute(entries, destination)
		if !endpointRouteNeedsRepair(current, found, tunnelIndex, gateway) {
			continue
		}

		// Only an existing host route is removed, and only after it has been
		// found in the table by exact address, so this can never take out the
		// covering route the endpoint would otherwise fall back to.
		if found {
			if err := runDarwinRoute(ctx, "delete", "-host", endpointRoute.destination); err != nil {
				errs = append(errs, err)
				continue
			}
		}
		if err := runDarwinRoute(ctx, "add", "-host", endpointRoute.destination, "-gateway", gateway.String()); err != nil {
			errs = append(errs, err)
			continue
		}
		repaired = true
	}

	return repaired, errors.Join(errs...)
}

// runDarwinRoute runs one route(8) command under a deadline. The guard runs on
// the health tick, so a wedged routing socket would otherwise stall silence
// detection and every other recovery check behind it.
func runDarwinRoute(ctx context.Context, args ...string) error {
	commandCtx, cancel := context.WithTimeout(ctx, darwinRouteTimeout)
	defer cancel()

	out, err := exec.CommandContext(commandCtx, "route", append([]string{"-n"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
