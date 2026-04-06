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
