//go:build windows

package wg

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"net/netip"
	"slices"
	"strings"
	"time"

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
	for _, prefix := range allowed4 {
		routes4 = append(routes4, &winipcfg.RouteData{
			Destination: prefix,
			NextHop:     netip.IPv4Unspecified(),
			Metric:      0,
		})
	}
	for _, prefix := range allowed6 {
		routes6 = append(routes6, &winipcfg.RouteData{
			Destination: prefix,
			NextHop:     netip.IPv6Unspecified(),
			Metric:      0,
		})
	}

	var errs []error
	if err := luid.SetIPAddressesForFamily(windowsFamilyV4, addresses4); err != nil {
		errs = append(errs, fmt.Errorf("set IPv4 addresses: %w", err))
	}
	if err := luid.SetIPAddressesForFamily(windowsFamilyV6, addresses6); err != nil {
		errs = append(errs, fmt.Errorf("set IPv6 addresses: %w", err))
	}
	// Addresses first: an on-link default route can fail ERROR_NOT_FOUND on an
	// interface with no address configured yet.
	if err := setWindowsRoutesForFamily(luid, windowsFamilyV4, routes4); err != nil {
		errs = append(errs, fmt.Errorf("set IPv4 routes: %w", err))
	}
	if err := setWindowsRoutesForFamily(luid, windowsFamilyV6, routes6); err != nil {
		errs = append(errs, fmt.Errorf("set IPv6 routes: %w", err))
	}
	if err := configureWindowsIPInterface(luid, mtu); err != nil {
		errs = append(errs, err)
	}
	if err := applyWindowsDNSServers(luid, dnsServers); err != nil {
		errs = append(errs, fmt.Errorf("set DNS servers: %w", err))
	}

	return errors.Join(errs...)
}

// setWindowsRoutesForFamily replaces the family's routes, retrying once on
// ERROR_NOT_FOUND: FlushRoutes reports a row the OS already removed and
// SetRoutesForFamily then returns before installing anything.
func setWindowsRoutesForFamily(luid winipcfg.LUID, family winipcfg.AddressFamily, routes []*winipcfg.RouteData) error {
	err := luid.SetRoutesForFamily(family, routes)
	if err != nil && errors.Is(err, windows.ERROR_NOT_FOUND) {
		err = luid.SetRoutesForFamily(family, routes)
	}
	return err
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

func configureWindowsIPInterface(luid winipcfg.LUID, mtu int) error {
	var errs []error

	if err := tuneWindowsIPInterface(luid, windowsFamilyV4, mtu); err != nil {
		errs = append(errs, fmt.Errorf("configure IPv4 interface settings: %w", err))
	}
	if err := tuneWindowsIPInterface(luid, windowsFamilyV6, mtu); err != nil {
		errs = append(errs, fmt.Errorf("configure IPv6 interface settings: %w", err))
	}

	return errors.Join(errs...)
}

const windowsIPInterfaceRetries = 20
const windowsIPInterfaceRetryDelay = 50 * time.Millisecond

// tuneWindowsIPInterface waits for the family's MIB_IPINTERFACE_ROW to be
// published, then forces the metric to 0. Metric tuning is a route/DNS
// preference optimization, not a correctness gate, so a family whose row never
// appears or whose metric won't stick is best-effort — the handshake proves the
// tunnel, and aborting the whole connect here regressed every Windows connect.
func tuneWindowsIPInterface(luid winipcfg.LUID, family winipcfg.AddressFamily, mtu int) error {
	var row *winipcfg.MibIPInterfaceRow
	var err error
	for attempt := 0; attempt < windowsIPInterfaceRetries; attempt++ {
		row, err = luid.IPInterface(family)
		if err == nil {
			break
		}
		if !errors.Is(err, windows.ERROR_NOT_FOUND) {
			return err
		}
		time.Sleep(windowsIPInterfaceRetryDelay)
	}
	// The family's row may never publish (e.g. IPv6 disabled on this NIC): that
	// is a metric we could not tune, not a bring-up failure.
	if err != nil {
		return nil
	}

	applyWindowsIPInterfaceTuning(row, mtu)
	if err := row.Set(); err != nil {
		return err
	}

	// Best-effort corrective re-apply: a metric that does not read back as 0 is
	// a soft preference issue, never a reason to abort a connect.
	if verify, verr := luid.IPInterface(family); verr == nil && (verify.UseAutomaticMetric || verify.Metric != 0) {
		applyWindowsIPInterfaceTuning(verify, mtu)
		_ = verify.Set()
	}
	return nil
}

func applyWindowsIPInterfaceTuning(row *winipcfg.MibIPInterfaceRow, mtu int) {
	row.RouterDiscoveryBehavior = winipcfg.RouterDiscoveryDisabled
	row.DadTransmits = 0
	row.ManagedAddressConfigurationSupported = false
	row.OtherStatefulConfigurationSupported = false
	if mtu > 0 && mtu <= math.MaxUint32 {
		row.NLMTU = uint32(mtu)
	}
	// Route selection and DNS preference order by interface metric, so an
	// automatic metric leaves the tunnel merely tied, not decisively ahead.
	row.UseAutomaticMetric = false
	row.Metric = 0
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

// ensureSessionDNS re-applies want to the tunnel interface when the host has
// stopped pointing at exactly those resolvers, reporting whether it had to
// correct anything.
//
// Windows hands interface DNS to whoever wrote last and tells nobody. Another
// VPN client's DNS enforcement, or a Windows component re-profiling the adapter,
// can take it over mid-session — and a tunnel carrying traffic perfectly still
// leaves the user with no name resolution when that happens. Nothing inside the
// tunnel can see it, so the health loop has to look.
func ensureSessionDNS(session *tunnelSession, want []string) (bool, error) {
	if session == nil || session.windowsLUID == 0 {
		return false, nil
	}
	luid := winipcfg.LUID(session.windowsLUID)

	wanted4, wanted6, err := parseWindowsDNSAddrs(want)
	if err != nil {
		return false, err
	}
	if len(wanted4) == 0 {
		return false, nil
	}

	current, err := luid.DNS()
	if err != nil {
		return false, fmt.Errorf("read interface DNS: %w", err)
	}
	if windowsDNSMatches(current, wanted4, wanted6) {
		return false, nil
	}
	if err := luid.SetDNS(windowsFamilyV4, wanted4, nil); err != nil {
		return false, fmt.Errorf("re-apply IPv4 DNS: %w", err)
	}
	// The tunnel is IPv4-only: any IPv6 resolvers Windows picked up mid-session
	// must be wiped, not just outnumbered, or it keeps preferring them.
	if err := luid.SetDNS(windowsFamilyV6, wanted6, nil); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
		return false, fmt.Errorf("re-apply IPv6 DNS: %w", err)
	}
	return true, nil
}

// windowsDNSMatches reports whether the interface's resolvers are exactly
// want4 followed by want6, in order — order is preference, so a reordered
// list is a real change, and a stray v6 resolver from elsewhere is too.
func windowsDNSMatches(current []netip.Addr, want4, want6 []netip.Addr) bool {
	got4 := make([]netip.Addr, 0, len(want4))
	got6 := make([]netip.Addr, 0, len(want6))
	for _, addr := range current {
		unmapped := addr.Unmap()
		if unmapped.Is4() {
			got4 = append(got4, unmapped)
		} else {
			got6 = append(got6, unmapped)
		}
	}
	return slices.Equal(got4, want4) && slices.Equal(got6, want6)
}

func addWindowsEndpointRoutes(ctx context.Context, excludeLUIDs map[uint64]struct{}, endpointHosts []string) ([]windowsRouteSpec, error) {
	routes, resolveErr := resolveEndpointRoutes(ctx, endpointHosts)
	if resolveErr != nil {
		return nil, resolveErr
	}
	if len(routes) == 0 {
		return nil, nil
	}

	defaultRoutes, err := windowsDefaultRoutesByFamily(excludeLUIDs)
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

		spec := windowsRouteSpec{
			interfaceLUID: uint64(defaultRoute.interfaceLUID),
			destination:   netip.PrefixFrom(addr, bits).Masked().String(),
			nextHop:       defaultRoute.nextHop.String(),
		}
		if _, err := addWindowsRoute(spec); err != nil {
			errs = append(errs, err)
			continue
		}
		// Adopt a pre-existing route too — most likely left by a crashed prior
		// session — so this session's teardown cleans it up either way.
		added = append(added, spec)
	}

	return added, errors.Join(errs...)
}

// ensureSessionEndpointRoutes re-pins the endpoint bypass routes to the host's
// current default route, reporting whether it had to repair anything.
//
// Windows drops an interface's routes on a media-sense flap, and a roam or DHCP
// renewal moves the gateway out from under them. Either way the node's address
// falls back to matching the tunnel's own AllowedIPs and WireGuard ends up
// routed into the tunnel it is trying to establish.
func ensureSessionEndpointRoutes(_ context.Context, session *tunnelSession, excludeLUIDs map[uint64]struct{}) (bool, error) {
	if session == nil || session.windowsLUID == 0 || len(session.windowsRoutes) == 0 {
		return false, nil
	}

	defaultRoutes, err := windowsDefaultRoutesByFamily(excludeLUIDs)
	if err != nil {
		return false, fmt.Errorf("read default routes: %w", err)
	}

	repaired := false
	var errs []error
	var stale []windowsRouteSpec
	for i := range session.windowsRoutes {
		current := session.windowsRoutes[i]
		want, ok := plannedEndpointRoute(current, defaultRoutes)
		if !ok {
			continue
		}
		if want == current && windowsRouteIsPresent(current) {
			continue
		}

		// New route in before the old one goes out. When the gateway has merely
		// moved the old route is still installed and still the session's only way
		// out, so removing it first would mean a failed add leaves the node with
		// no path at all — the very failure this repairs.
		created, err := addWindowsRoute(want)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if want == current && !created {
			// The route was there all along and the lookup simply could not
			// confirm it. Reporting a repair here would have the health check
			// deferring to a fix that never happened, every tick.
			continue
		}
		if want != current {
			if err := removeWindowsEndpointRoutes([]windowsRouteSpec{current}); err != nil {
				// Removal failed, so the stale route is still on the host. Keep it
				// tracked separately so stopWindows still deletes it later.
				errs = append(errs, err)
				stale = append(stale, current)
			}
		}
		session.windowsRoutes[i] = want
		repaired = true
	}
	if len(stale) > 0 {
		session.windowsRoutes = append(session.windowsRoutes, stale...)
	}

	return repaired, errors.Join(errs...)
}

// plannedEndpointRoute reports where a recorded bypass route should point given
// the host's default routes now. ok is false when there is nothing to pin it to
// — mid-roam, or the link is down — which leaves the recorded spec for a later
// pass rather than tearing up a route with nowhere to put it.
func plannedEndpointRoute(spec windowsRouteSpec, defaultRoutes map[string]windowsDefaultRoute) (windowsRouteSpec, bool) {
	destination, err := netip.ParsePrefix(strings.TrimSpace(spec.destination))
	if err != nil {
		return windowsRouteSpec{}, false
	}

	family := "inet"
	if !destination.Addr().Is4() {
		family = "inet6"
	}
	defaultRoute, ok := defaultRoutes[family]
	if !ok {
		return windowsRouteSpec{}, false
	}

	return windowsRouteSpec{
		interfaceLUID: uint64(defaultRoute.interfaceLUID),
		destination:   spec.destination,
		nextHop:       defaultRoute.nextHop.String(),
	}, true
}

// windowsRouteIsPresent reports whether the route is still in the forwarding
// table. Anything it cannot confirm counts as absent: re-adding a route that is
// in fact there is free, while skipping one that is gone costs the session.
func windowsRouteIsPresent(spec windowsRouteSpec) bool {
	destination, nextHop, err := parseWindowsRouteSpec(spec)
	if err != nil {
		return false
	}
	row, err := winipcfg.LUID(spec.interfaceLUID).Route(destination, nextHop)
	return err == nil && row != nil
}

// addWindowsRoute installs the route and reports whether it created it. An
// route that was already there is not an error, but it is not a change either —
// callers use that to tell a real repair from a no-op.
func addWindowsRoute(spec windowsRouteSpec) (bool, error) {
	destination, nextHop, err := parseWindowsRouteSpec(spec)
	if err != nil {
		return false, err
	}

	routeData := &winipcfg.RouteData{Destination: destination, NextHop: nextHop, Metric: 0}
	if err := winipcfg.LUID(spec.interfaceLUID).AddRoutes([]*winipcfg.RouteData{routeData}); err != nil {
		if errors.Is(err, windows.ERROR_OBJECT_ALREADY_EXISTS) {
			return false, nil
		}
		return false, fmt.Errorf("add endpoint route %s via %s: %w", destination.String(), nextHop.String(), err)
	}
	return true, nil
}

func removeWindowsEndpointRoutes(routes []windowsRouteSpec) error {
	var errs []error
	for _, route := range routes {
		destination, nextHop, err := parseWindowsRouteSpec(route)
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

func parseWindowsRouteSpec(spec windowsRouteSpec) (netip.Prefix, netip.Addr, error) {
	destination, err := netip.ParsePrefix(strings.TrimSpace(spec.destination))
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, fmt.Errorf("invalid endpoint route destination %q: %w", spec.destination, err)
	}
	nextHop, err := netip.ParseAddr(strings.TrimSpace(spec.nextHop))
	if err != nil {
		return netip.Prefix{}, netip.Addr{}, fmt.Errorf("invalid endpoint route next hop %q: %w", spec.nextHop, err)
	}
	return destination, nextHop, nil
}

func windowsDefaultRoutesByFamily(excludeLUIDs map[uint64]struct{}) (map[string]windowsDefaultRoute, error) {
	out := make(map[string]windowsDefaultRoute, 2)

	v4, ok, err := bestWindowsDefaultRoute(windowsFamilyV4, excludeLUIDs)
	if err != nil {
		return nil, err
	}
	if ok {
		out["inet"] = v4
	}

	v6, ok, err := bestWindowsDefaultRoute(windowsFamilyV6, excludeLUIDs)
	if err != nil {
		return nil, err
	}
	if ok {
		out["inet6"] = v6
	}

	return out, nil
}

func bestWindowsDefaultRoute(family winipcfg.AddressFamily, excludeLUIDs map[uint64]struct{}) (windowsDefaultRoute, bool, error) {
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
		if _, excluded := excludeLUIDs[uint64(row.InterfaceLUID)]; excluded || row.Loopback {
			continue
		}

		nextHop := row.NextHop.Addr()
		if !nextHop.IsValid() || nextHop.IsLoopback() || nextHop.IsMulticast() {
			continue
		}
		// An on-link default belongs to a tunnel, not a gateway. Pinning the
		// node's bypass to one would route WireGuard through a tunnel — its own
		// after a rebuild, or another VPN's — which is the loop this avoids.
		if nextHop.IsUnspecified() {
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

		// Ties break on the lower LUID rather than on table order, so repeated
		// elections agree with each other. An answer that alternated between
		// two equal-metric gateways would have the route guard re-pinning the
		// bypass on every health check.
		if !bestFound || metric < best.metric ||
			(metric == best.metric && row.InterfaceLUID < best.interfaceLUID) {
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
