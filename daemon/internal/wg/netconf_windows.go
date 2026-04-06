//go:build windows

package wg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var (
	windowsFamilyV4 = winipcfg.AddressFamily(windows.AF_INET)
	windowsFamilyV6 = winipcfg.AddressFamily(windows.AF_INET6)
)

type windowsDefaultRoute struct {
	interfaceLUID winipcfg.LUID
	nextHop       netip.Addr
	metric        uint64
}

func windowsInterfaceLUID(tunDev any, interfaceName string) (uint64, error) {
	if provider, ok := tunDev.(interface{ LUID() uint64 }); ok {
		if luid := provider.LUID(); luid != 0 {
			return luid, nil
		}
	}

	iface, err := net.InterfaceByName(interfaceName)
	if err != nil {
		return 0, fmt.Errorf("lookup windows interface %s: %w", interfaceName, err)
	}
	luid, err := winipcfg.LUIDFromIndex(uint32(iface.Index))
	if err != nil {
		return 0, fmt.Errorf("resolve interface LUID for %s: %w", interfaceName, err)
	}
	return uint64(luid), nil
}

func configureWindowsInterface(luidValue uint64, addresses []string, allowedIPs []string, dnsServers []string, mtu int) error {
	if luidValue == 0 {
		return errors.New("invalid interface LUID")
	}
	luid := winipcfg.LUID(luidValue)

	addresses4, addresses6, err := parseWindowsPrefixes(addresses)
	if err != nil {
		return fmt.Errorf("parse interface addresses: %w", err)
	}

	allowed4, allowed6, err := parseWindowsRoutePrefixes(allowedIPs)
	if err != nil {
		return fmt.Errorf("parse allowed-ips routes: %w", err)
	}

	routes4 := make([]*winipcfg.RouteData, 0, len(allowed4))
	routes6 := make([]*winipcfg.RouteData, 0, len(allowed6))
	hasDefault4 := false
	hasDefault6 := false
	for _, prefix := range allowed4 {
		if prefix.Bits() <= 1 {
			hasDefault4 = true
		}
		routes4 = append(routes4, &winipcfg.RouteData{
			Destination: prefix,
			NextHop:     netip.IPv4Unspecified(),
			Metric:      0,
		})
	}
	for _, prefix := range allowed6 {
		if prefix.Bits() <= 1 {
			hasDefault6 = true
		}
		routes6 = append(routes6, &winipcfg.RouteData{
			Destination: prefix,
			NextHop:     netip.IPv6Unspecified(),
			Metric:      0,
		})
	}

	var errs []error
	if err := luid.SetRoutesForFamily(windowsFamilyV4, routes4); err != nil {
		errs = append(errs, fmt.Errorf("set IPv4 routes: %w", err))
	}
	if err := luid.SetRoutesForFamily(windowsFamilyV6, routes6); err != nil {
		errs = append(errs, fmt.Errorf("set IPv6 routes: %w", err))
	}
	if err := luid.SetIPAddressesForFamily(windowsFamilyV4, addresses4); err != nil {
		errs = append(errs, fmt.Errorf("set IPv4 addresses: %w", err))
	}
	if err := luid.SetIPAddressesForFamily(windowsFamilyV6, addresses6); err != nil {
		errs = append(errs, fmt.Errorf("set IPv6 addresses: %w", err))
	}
	if err := configureWindowsIPInterface(luid, mtu, hasDefault4, hasDefault6); err != nil {
		errs = append(errs, err)
	}
	if err := applyWindowsDNSServers(luid, dnsServers); err != nil {
		errs = append(errs, fmt.Errorf("set DNS servers: %w", err))
	}

	return errors.Join(errs...)
}

func clearWindowsInterfaceConfig(luidValue uint64) error {
	if luidValue == 0 {
		return nil
	}

	luid := winipcfg.LUID(luidValue)
	var errs []error

	if err := luid.SetRoutesForFamily(windowsFamilyV4, []*winipcfg.RouteData{}); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		errs = append(errs, fmt.Errorf("clear IPv4 routes: %w", err))
	}
	if err := luid.SetRoutesForFamily(windowsFamilyV6, []*winipcfg.RouteData{}); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		errs = append(errs, fmt.Errorf("clear IPv6 routes: %w", err))
	}
	if err := luid.SetIPAddressesForFamily(windowsFamilyV4, []netip.Prefix{}); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		errs = append(errs, fmt.Errorf("clear IPv4 addresses: %w", err))
	}
	if err := luid.SetIPAddressesForFamily(windowsFamilyV6, []netip.Prefix{}); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		errs = append(errs, fmt.Errorf("clear IPv6 addresses: %w", err))
	}
	if err := luid.SetDNS(windowsFamilyV4, nil, nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		errs = append(errs, fmt.Errorf("clear IPv4 DNS: %w", err))
	}
	if err := luid.SetDNS(windowsFamilyV6, nil, nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		errs = append(errs, fmt.Errorf("clear IPv6 DNS: %w", err))
	}

	return errors.Join(errs...)
}

func configureWindowsIPInterface(luid winipcfg.LUID, mtu int, hasDefault4 bool, hasDefault6 bool) error {
	var errs []error

	if err := tuneWindowsIPInterface(luid, windowsFamilyV4, mtu, hasDefault4); err != nil {
		errs = append(errs, fmt.Errorf("configure IPv4 interface settings: %w", err))
	}
	if err := tuneWindowsIPInterface(luid, windowsFamilyV6, mtu, hasDefault6); err != nil {
		errs = append(errs, fmt.Errorf("configure IPv6 interface settings: %w", err))
	}

	return errors.Join(errs...)
}

func tuneWindowsIPInterface(luid winipcfg.LUID, family winipcfg.AddressFamily, mtu int, forceMetric bool) error {
	row, err := luid.IPInterface(family)
	if err != nil {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return nil
		}
		return err
	}

	row.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
	row.DadTransmits = 0
	row.ManagedAddressConfigurationSupported = false
	row.OtherStatefulConfigurationSupported = false

	if mtu > 0 {
		row.NLMTU = uint32(mtu)
	}
	if forceMetric {
		row.UseAutomaticMetric = false
		row.Metric = 0
	}
	return row.Set()
}

func applyWindowsDNSServers(luid winipcfg.LUID, dnsServers []string) error {
	servers4, servers6, err := parseWindowsDNSAddrs(dnsServers)
	if err != nil {
		return err
	}

	var errs []error
	if len(servers4) > 0 {
		if err := luid.SetDNS(windowsFamilyV4, servers4, nil); err != nil {
			errs = append(errs, fmt.Errorf("set IPv4 DNS: %w", err))
		}
	} else {
		// Ensure stale IPv4 DNS servers are removed when none are requested.
		if err := luid.SetDNS(windowsFamilyV4, nil, nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
			errs = append(errs, fmt.Errorf("clear IPv4 DNS: %w", err))
		}
	}

	if len(servers6) > 0 {
		if err := luid.SetDNS(windowsFamilyV6, servers6, nil); err != nil {
			errs = append(errs, fmt.Errorf("set IPv6 DNS: %w", err))
		}
	} else {
		// In IPv4-only mode this keeps Windows from trying stale IPv6 resolvers.
		if err := luid.SetDNS(windowsFamilyV6, nil, nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
			errs = append(errs, fmt.Errorf("clear IPv6 DNS: %w", err))
		}
	}

	return errors.Join(errs...)
}

func addWindowsEndpointRoutes(ctx context.Context, tunnelLUID uint64, endpointHosts []string) ([]windowsRouteSpec, error) {
	routes := resolveEndpointRoutes(ctx, endpointHosts)
	if len(routes) == 0 {
		return nil, nil
	}

	defaultRoutes, err := windowsDefaultRoutesByFamily(tunnelLUID)
	if err != nil {
		return nil, err
	}

	added := make([]windowsRouteSpec, 0, len(routes))
	var errs []error
	for _, route := range routes {
		defaultRoute, ok := defaultRoutes[route.family]
		if !ok {
			continue
		}

		addr, parseErr := netip.ParseAddr(route.destination)
		if parseErr != nil {
			continue
		}
		bits := 128
		if addr.Is4() {
			bits = 32
		}

		destination := netip.PrefixFrom(addr, bits).Masked()
		routeData := &winipcfg.RouteData{
			Destination: destination,
			NextHop:     defaultRoute.nextHop,
			Metric:      0,
		}
		if err := defaultRoute.interfaceLUID.AddRoutes([]*winipcfg.RouteData{routeData}); err != nil {
			if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
				continue
			}
			errs = append(errs, fmt.Errorf("add endpoint route %s via %s: %w", destination.String(), defaultRoute.nextHop.String(), err))
			continue
		}

		added = append(added, windowsRouteSpec{
			interfaceLUID: uint64(defaultRoute.interfaceLUID),
			destination:   destination.String(),
			nextHop:       defaultRoute.nextHop.String(),
		})
	}

	return added, errors.Join(errs...)
}

func removeWindowsEndpointRoutes(routes []windowsRouteSpec) error {
	var errs []error
	for _, route := range routes {
		destination, err := netip.ParsePrefix(strings.TrimSpace(route.destination))
		if err != nil {
			continue
		}
		nextHop, err := netip.ParseAddr(strings.TrimSpace(route.nextHop))
		if err != nil {
			continue
		}

		luid := winipcfg.LUID(route.interfaceLUID)
		if err := luid.DeleteRoute(destination, nextHop); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
			errs = append(errs, fmt.Errorf("remove endpoint route %s via %s: %w", destination.String(), nextHop.String(), err))
		}
	}
	return errors.Join(errs...)
}

func windowsDefaultRoutesByFamily(excludeLUID uint64) (map[string]windowsDefaultRoute, error) {
	out := make(map[string]windowsDefaultRoute, 2)
	exclude := winipcfg.LUID(excludeLUID)

	v4, ok, err := bestWindowsDefaultRoute(windowsFamilyV4, exclude)
	if err != nil {
		return nil, err
	}
	if ok {
		out["inet"] = v4
	}

	v6, ok, err := bestWindowsDefaultRoute(windowsFamilyV6, exclude)
	if err != nil {
		return nil, err
	}
	if ok {
		out["inet6"] = v6
	}

	return out, nil
}

func bestWindowsDefaultRoute(family winipcfg.AddressFamily, excludeLUID winipcfg.LUID) (windowsDefaultRoute, bool, error) {
	table, err := winipcfg.GetIPForwardTable2(family)
	if err != nil {
		return windowsDefaultRoute{}, false, err
	}

	var best windowsDefaultRoute
	bestFound := false

	for i := range table {
		row := table[i]

		prefix := row.DestinationPrefix.Prefix()
		if !prefix.IsValid() || prefix.Bits() != 0 {
			continue
		}
		if row.InterfaceLUID == excludeLUID || row.Loopback {
			continue
		}

		nextHop := row.NextHop.Addr()
		if !nextHop.IsValid() || nextHop.IsLoopback() || nextHop.IsMulticast() {
			continue
		}

		iface, ifaceErr := row.InterfaceLUID.Interface()
		if ifaceErr == nil && iface.OperStatus != winipcfg.IfOperStatusUp {
			continue
		}

		metric := uint64(row.Metric)
		if ipif, ipifErr := row.InterfaceLUID.IPInterface(family); ipifErr == nil {
			metric += uint64(ipif.Metric)
		}

		if !bestFound || metric < best.metric {
			best = windowsDefaultRoute{
				interfaceLUID: row.InterfaceLUID,
				nextHop:       nextHop,
				metric:        metric,
			}
			bestFound = true
		}
	}

	return best, bestFound, nil
}

func parseWindowsPrefixes(values []string) ([]netip.Prefix, []netip.Prefix, error) {
	v4 := make([]netip.Prefix, 0, len(values))
	v6 := make([]netip.Prefix, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}

		prefix, err := netip.ParsePrefix(trimmed)
		if err != nil {
			addr, addrErr := netip.ParseAddr(trimmed)
			if addrErr != nil {
				return nil, nil, fmt.Errorf("invalid prefix %q", trimmed)
			}
			bits := 128
			if addr.Is4() {
				bits = 32
			}
			prefix = netip.PrefixFrom(addr, bits)
		}

		key := prefix.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if prefix.Addr().Is4() {
			v4 = append(v4, prefix)
		} else {
			v6 = append(v6, prefix)
		}
	}

	return v4, v6, nil
}

func parseWindowsRoutePrefixes(values []string) ([]netip.Prefix, []netip.Prefix, error) {
	v4, v6, err := parseWindowsPrefixes(values)
	if err != nil {
		return nil, nil, err
	}

	for i := range v4 {
		v4[i] = v4[i].Masked()
	}
	for i := range v6 {
		v6[i] = v6[i].Masked()
	}

	return uniqueWindowsPrefixes(v4), uniqueWindowsPrefixes(v6), nil
}

func uniqueWindowsPrefixes(prefixes []netip.Prefix) []netip.Prefix {
	if len(prefixes) == 0 {
		return prefixes
	}

	out := make([]netip.Prefix, 0, len(prefixes))
	seen := make(map[string]struct{}, len(prefixes))
	for _, prefix := range prefixes {
		key := prefix.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, prefix)
	}
	return out
}

func parseWindowsDNSAddrs(values []string) ([]netip.Addr, []netip.Addr, error) {
	v4 := make([]netip.Addr, 0, len(values))
	v6 := make([]netip.Addr, 0, len(values))
	seen := make(map[string]struct{}, len(values))

	for _, raw := range values {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		addr, err := netip.ParseAddr(trimmed)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid DNS server %q", trimmed)
		}
		key := addr.String()
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}

		if addr.Is4() {
			v4 = append(v4, addr)
		} else {
			v6 = append(v6, addr)
		}
	}

	return v4, v6, nil
}
