//go:build darwin

package wg

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/route"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// darwinRouteTimeout bounds one route(8) call made from the health tick.
const darwinRouteTimeout = 5 * time.Second

// Every command here runs as root; PATH-based lookup is avoidable and not worth the risk.
const (
	ifconfigPath     = "/sbin/ifconfig"
	routePath        = "/sbin/route"
	networksetupPath = "/usr/sbin/networksetup"
)

// darwinDNSUnknownMarker flags a service whose original DNS state could not
// be read, so restore leaves it untouched instead of assuming automatic.
const darwinDNSUnknownMarker = "?"

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
			out, err := exec.Command(ifconfigPath, interfaceName,
				"inet", v4.String(), v4.String(),
				"netmask", mask.String(),
			).CombinedOutput()
			if err != nil {
				return fmt.Errorf("add ipv4 address %s on %s: %w (%s)", cidr, interfaceName, err, strings.TrimSpace(string(out)))
			}
		} else {
			ones, _ := ipNet.Mask.Size()
			out, err := exec.Command(ifconfigPath, interfaceName,
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
	out, err := exec.Command(ifconfigPath, interfaceName, "up").CombinedOutput()
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
	out, err := exec.Command(routePath, "-n", "get", "default").CombinedOutput()
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

// isDarwinRouteExists reports whether a route(8) failure means the route was
// already present, so it can still be tracked instead of dropped from added.
func isDarwinRouteExists(output []byte) bool {
	text := strings.ToLower(string(output))
	return strings.Contains(text, "file exists") || strings.Contains(text, "already in table")
}

// addDarwinEndpointRoutes adds host routes for WireGuard endpoint IPs
// through the default gateway so endpoint traffic bypasses the tunnel.
func addDarwinEndpointRoutes(ctx context.Context, endpointHosts []string) ([]routeSpec, error) {
	routes, resolveErr := resolveEndpointRoutes(ctx, endpointHosts)
	if resolveErr != nil {
		return nil, fmt.Errorf("resolve endpoint hosts: %w", resolveErr)
	}
	if len(routes) == 0 {
		return nil, nil
	}

	gw, err := darwinDefaultGatewayV4()
	if err != nil {
		return nil, fmt.Errorf("resolve default gateway for endpoint routes: %w", err)
	}

	added := make([]routeSpec, 0, len(routes))
	var errs []error
	for _, route := range routes {
		if route.family == "inet6" {
			continue // no v6 bypass path yet; disableDarwinIPv6ForSession covers the leak instead
		}

		out, err := exec.Command(routePath, "-n", "add", "-host", route.destination, "-gateway", gw).CombinedOutput()
		if err != nil && !isDarwinRouteExists(out) {
			errs = append(errs, fmt.Errorf("add endpoint route %s via %s: %w (%s)", route.destination, gw, err, strings.TrimSpace(string(out))))
			continue
		}
		added = append(added, route)
	}
	return added, errors.Join(errs...)
}

// removeDarwinEndpointRoutes removes previously added endpoint bypass routes.
func removeDarwinEndpointRoutes(routes []routeSpec) {
	for _, route := range routes {
		if route.family == "inet6" {
			continue
		}
		_ = exec.Command(routePath, "-n", "delete", "-host", route.destination).Run()
	}
}

// allowedIPsHaveIPv6 reports whether any AllowedIPs prefix is IPv6. The
// tunnel interface only ever gets IPv4 addresses, so such a prefix can never
// actually be routed into it.
func allowedIPsHaveIPv6(allowedIPs []string) bool {
	for _, prefix := range allowedIPs {
		if _, family, err := normalizedRoutesForPrefix(prefix); err == nil && family == "inet6" {
			return true
		}
	}
	return false
}

// addDarwinAllowedIPRoutes adds routes for WireGuard allowed-IP prefixes
// through the tunnel interface.
//
// IPv6 prefixes are not installed here since the tunnel has no v6 address to
// route them to; the caller disables IPv6 system-wide instead (see
// disableDarwinIPv6ForSession) so that traffic doesn't silently leak.
func addDarwinAllowedIPRoutes(interfaceName string, allowedIPs []string) error {
	for _, prefix := range allowedIPs {
		routePrefixes, family, err := normalizedRoutesForPrefix(prefix)
		if err != nil {
			return err
		}
		if family == "inet6" {
			continue
		}
		for _, rp := range routePrefixes {
			_, ipNet, parseErr := net.ParseCIDR(rp)
			if parseErr != nil {
				return fmt.Errorf("parse route prefix %s: %w", rp, parseErr)
			}

			mask := net.IP(ipNet.Mask).To4()
			out, err := exec.Command(routePath, "-n", "add",
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

// removeDarwinAllowedIPRoutes removes allowed-IP routes. Deletion is scoped
// to interfaceName so it can never match a physical route with the same
// prefix (e.g. a split-tunnel 192.168.1.0/24 matching the real LAN route).
func removeDarwinAllowedIPRoutes(interfaceName string, allowedIPs []string) {
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
			_ = exec.Command(routePath, "-n", "delete",
				"-net", ipNet.IP.String(),
				"-netmask", mask.String(),
				"-interface", interfaceName,
			).Run()
		}
	}
}

// ---------------------------------------------------------------------------
// IPv6 lockdown via networksetup (non-cgo)
// ---------------------------------------------------------------------------

// darwinIPv6State records a network service's IPv6 mode before it was
// disabled for the session, so it can be restored afterwards.
type darwinIPv6State struct {
	service string
	mode    string
}

// getDarwinIPv6Mode reads a service's current IPv6 setting ("Automatic",
// "Off", "Manual", ...) from networksetup -getinfo. Empty means unreadable.
func getDarwinIPv6Mode(serviceName string) string {
	out, err := exec.Command(networksetupPath, "-getinfo", serviceName).CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "IPv6:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "IPv6:"))
		}
	}
	return ""
}

// disableDarwinIPv6ForSession turns off IPv6 on every active network service
// so v6 traffic can't leave over the physical link while the tunnel is up.
func disableDarwinIPv6ForSession() ([]darwinIPv6State, error) {
	services, err := listDarwinNetworkServices()
	if err != nil {
		return nil, err
	}

	states := make([]darwinIPv6State, 0, len(services))
	for _, svc := range services {
		mode := getDarwinIPv6Mode(svc)
		if mode == "" || strings.EqualFold(mode, "Off") {
			continue
		}
		if _, err := exec.Command(networksetupPath, "-setv6off", svc).CombinedOutput(); err != nil {
			continue
		}
		states = append(states, darwinIPv6State{service: svc, mode: mode})
	}
	return states, nil
}

// restoreDarwinIPv6 re-enables IPv6 on services that were disabled for the
// session. Manual configurations are restored as automatic rather than
// replayed, since the original address/router are not captured.
func restoreDarwinIPv6(states []darwinIPv6State) {
	for _, s := range states {
		if strings.EqualFold(s.mode, "LinkLocal") {
			_ = exec.Command(networksetupPath, "-setv6linklocal", s.service).Run()
			continue
		}
		_ = exec.Command(networksetupPath, "-setv6automatic", s.service).Run()
	}
}

// ---------------------------------------------------------------------------
// DNS management via networksetup (non-cgo)
// ---------------------------------------------------------------------------

// listDarwinNetworkServices returns all non-hardware-port network service names.
func listDarwinNetworkServices() ([]string, error) {
	out, err := exec.Command(networksetupPath, "-listallnetworkservices").CombinedOutput()
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
// Returns nil if DNS is automatic/DHCP, or a single unknown-marker entry if
// the state could not be determined at all (as opposed to being empty).
func getDarwinDNSServers(serviceName string) []string {
	out, err := exec.Command(networksetupPath, "-getdnsservers", serviceName).CombinedOutput()
	if err != nil {
		return []string{darwinDNSUnknownMarker}
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
	if len(servers) == 0 {
		return []string{darwinDNSUnknownMarker}
	}
	return servers
}

// isDarwinDNSUnknown reports whether servers is the unknown-state marker.
func isDarwinDNSUnknown(servers []string) bool {
	return len(servers) == 1 && servers[0] == darwinDNSUnknownMarker
}

// darwinDNSListsEqual reports whether two ordered DNS server lists match.
func darwinDNSListsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// setDarwinDNSServers points serviceName's DNS at dnsServers.
func setDarwinDNSServers(serviceName string, dnsServers []string) error {
	args := append([]string{"-setdnsservers", serviceName}, dnsServers...)
	out, err := exec.Command(networksetupPath, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("set DNS for service %s: %w (%s)", serviceName, err, strings.TrimSpace(string(out)))
	}
	return nil
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

		if err := setDarwinDNSServers(svc, dnsServers); err != nil {
			// Roll back any overrides already applied.
			_ = restoreDarwinDNSServers(overrides)
			return nil, err
		}

		overrides = append(overrides, darwinDNSOverride{
			service:    svc,
			dnsServers: origDNS,
		})
	}

	return overrides, nil
}

// restoreDarwinDNSServers restores original DNS settings for all overridden
// services. A service whose original state could not be read is left alone
// rather than reset to automatic, to avoid destroying a static resolver.
func restoreDarwinDNSServers(overrides []darwinDNSOverride) error {
	if len(overrides) == 0 {
		return nil
	}

	var failures []string
	for _, override := range overrides {
		if isDarwinDNSUnknown(override.dnsServers) {
			continue
		}

		var args []string
		if len(override.dnsServers) == 0 {
			// Restore to automatic/DHCP.
			args = []string{"-setdnsservers", override.service, "empty"}
		} else {
			args = append([]string{"-setdnsservers", override.service}, override.dnsServers...)
		}

		out, err := exec.Command(networksetupPath, args...).CombinedOutput()
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v (%s)", override.service, err, strings.TrimSpace(string(out))))
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("failed restoring DNS on services: %s", strings.Join(failures, ", "))
	}
	return nil
}

// ensureSessionDNS re-applies want to every active network service, covering
// both drift on services already overridden and services that joined after
// bring-up (e.g. Wi-Fi connected while wired was active).
func ensureSessionDNS(session *tunnelSession, want []string) (bool, error) {
	if session == nil || len(want) == 0 {
		return false, nil
	}

	services, err := listDarwinNetworkServices()
	if err != nil {
		return false, err
	}

	covered := make(map[string]bool, len(session.dnsOverrides))
	for _, o := range session.dnsOverrides {
		covered[o.service] = true
	}

	changed := false
	var errs []error
	for _, svc := range services {
		current := getDarwinDNSServers(svc)
		if !covered[svc] {
			if err := setDarwinDNSServers(svc, want); err != nil {
				errs = append(errs, err)
				continue
			}
			session.dnsOverrides = append(session.dnsOverrides, darwinDNSOverride{service: svc, dnsServers: current})
			changed = true
			continue
		}
		if isDarwinDNSUnknown(current) || darwinDNSListsEqual(current, want) {
			continue
		}
		if err := setDarwinDNSServers(svc, want); err != nil {
			errs = append(errs, err)
			continue
		}
		changed = true
	}

	if changed {
		_ = persistDarwinDNSState(session.dnsOverrides)
	}
	return changed, errors.Join(errs...)
}

// ---------------------------------------------------------------------------
// DNS pre-state persistence, so a fresh daemon can restore after a crash
// ---------------------------------------------------------------------------

// darwinDNSStateEntry is the on-disk form of a darwinDNSOverride; the struct
// itself has unexported fields and lives in the shared session type.
type darwinDNSStateEntry struct {
	Service    string   `json:"service"`
	DNSServers []string `json:"dnsServers"`
}

func darwinDNSStateFile() (string, error) {
	dir, err := platform.AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "darwin-dns-session.json"), nil
}

// persistDarwinDNSState writes the DNS pre-state to disk. If the daemon dies
// before Stop runs, a fresh process can find this and restore the host's DNS.
func persistDarwinDNSState(overrides []darwinDNSOverride) error {
	path, err := darwinDNSStateFile()
	if err != nil {
		return err
	}

	entries := make([]darwinDNSStateEntry, 0, len(overrides))
	for _, o := range overrides {
		entries = append(entries, darwinDNSStateEntry{Service: o.service, DNSServers: o.dnsServers})
	}

	data, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal dns state: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write dns state: %w", err)
	}
	return nil
}

// clearDarwinDNSState removes the persisted DNS pre-state after a normal stop
// or a completed startup restore.
func clearDarwinDNSState() {
	path, err := darwinDNSStateFile()
	if err != nil {
		return
	}
	_ = os.Remove(path)
}

// loadDarwinDNSState reads a persisted DNS pre-state, if any is on disk.
func loadDarwinDNSState() ([]darwinDNSOverride, error) {
	path, err := darwinDNSStateFile()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var entries []darwinDNSStateEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, fmt.Errorf("unmarshal dns state: %w", err)
	}

	overrides := make([]darwinDNSOverride, 0, len(entries))
	for _, e := range entries {
		overrides = append(overrides, darwinDNSOverride{service: e.Service, dnsServers: e.DNSServers})
	}
	return overrides, nil
}

// restoreOrphanedDarwinDNSState restores DNS from a previous session's
// pre-state if the daemon starts and finds one on disk (e.g. after a crash
// or an app update replaced the process while a tunnel was connected).
func restoreOrphanedDarwinDNSState() {
	overrides, err := loadDarwinDNSState()
	if err != nil || len(overrides) == 0 {
		return
	}
	_ = restoreDarwinDNSServers(overrides)
	clearDarwinDNSState()
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

	out, err := exec.CommandContext(commandCtx, routePath, append([]string{"-n"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("route %s: %w (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
