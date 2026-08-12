//go:build darwin

package wg

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"strings"
)

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

// darwinRoute is what `route -n get` reports for a destination: the entry the
// kernel would actually use, which is a covering route when the host route we
// installed has gone.
type darwinRoute struct {
	destination string
	gateway     string
	iface       string
}

// parseDarwinRouteGet reads `route -n get <dest>` output. An interface route
// carries no gateway line, so an empty gateway is normal rather than a failure.
func parseDarwinRouteGet(out string) darwinRoute {
	var route darwinRoute
	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		key, value, found := strings.Cut(strings.TrimSpace(scanner.Text()), ":")
		if !found {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "destination":
			route.destination = value
		case "gateway":
			route.gateway = value
		case "interface":
			route.iface = value
		}
	}
	return route
}

func darwinRouteFor(destination string) (darwinRoute, error) {
	out, err := exec.Command("route", "-n", "get", destination).CombinedOutput()
	if err != nil {
		return darwinRoute{}, fmt.Errorf("query route to %s: %w (%s)", destination, err, strings.TrimSpace(string(out)))
	}
	return parseDarwinRouteGet(string(out)), nil
}

// endpointRouteNeedsRepair reports whether the bypass to destination has
// stopped doing its job: replaced by a covering route because the host route
// was dropped, pointing into the tunnel, or hanging off a gateway that moved.
func endpointRouteNeedsRepair(destination string, current darwinRoute, tunnelInterface, gateway string) bool {
	if current.destination != destination {
		return true
	}
	if current.iface != "" && current.iface == tunnelInterface {
		return true
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
func ensureSessionEndpointRoutes(session *tunnelSession, _ map[uint64]struct{}) (bool, error) {
	if session == nil || len(session.endpointRoutes) == 0 {
		return false, nil
	}

	gateway, err := darwinDefaultGatewayV4()
	if err != nil {
		return false, fmt.Errorf("read default gateway: %w", err)
	}

	repaired := false
	var errs []error
	for _, route := range session.endpointRoutes {
		if route.family != "inet" {
			continue
		}

		current, err := darwinRouteFor(route.destination)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !endpointRouteNeedsRepair(route.destination, current, session.interfaceName, gateway) {
			continue
		}

		// A plain add is enough when the host route is simply gone, and it can
		// never rewrite the covering route the way `route change` might. Only a
		// stale host route still in the table needs removing first, and that one
		// already points somewhere useless.
		if current.destination == route.destination {
			if err := exec.Command("route", "-n", "delete", "-host", route.destination).Run(); err != nil {
				errs = append(errs, fmt.Errorf("remove stale endpoint route %s: %w", route.destination, err))
				continue
			}
		}
		out, err := exec.Command("route", "-n", "add", "-host", route.destination, "-gateway", gateway).CombinedOutput()
		if err != nil {
			errs = append(errs, fmt.Errorf("re-pin endpoint route %s via %s: %w (%s)", route.destination, gateway, err, strings.TrimSpace(string(out))))
			continue
		}
		repaired = true
	}

	return repaired, errors.Join(errs...)
}
