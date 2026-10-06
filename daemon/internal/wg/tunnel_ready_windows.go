//go:build windows

package wg

import (
	"net/netip"

	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// tunnelSessionReady reports whether the adapter can actually carry a datagram: every
// address out of duplicate-address-detection with its Local host route, every AllowedIPs route in.
func tunnelSessionReady(session *tunnelSession, profile state.WireGuardProfile) (bool, error) {
	if session.device == nil || session.windowsLUID == 0 {
		return false, nil
	}

	parsed, err := parseUserlandConfig(profile.ConfigText)
	if err != nil {
		return false, err
	}
	allowedIPs, err := validateParsedIPv4Only(parsed)
	if err != nil {
		return false, err
	}
	addresses4, addresses6, err := parseWindowsPrefixes(parsed.addresses)
	if err != nil {
		return false, err
	}
	allowed4, allowed6, err := parseWindowsRoutePrefixes(allowedIPs)
	if err != nil {
		return false, err
	}
	allowed4 = withoutInterfaceHostRoutes(allowed4, addresses4)
	allowed6 = withoutInterfaceHostRoutes(allowed6, addresses6)

	luid := winipcfg.LUID(session.windowsLUID)
	for _, prefix := range append(addresses4, addresses6...) {
		row, err := luid.IPAddress(prefix.Addr())
		if err != nil || row == nil || row.DadState != winipcfg.DadStatePreferred {
			return false, nil
		}
	}
	for family, addresses := range map[winipcfg.AddressFamily][]netip.Prefix{windowsFamilyV4: addresses4, windowsFamilyV6: addresses6} {
		if len(addresses) == 0 {
			continue
		}
		table, err := winipcfg.GetIPForwardTable2(family)
		if err != nil {
			return false, nil
		}
		for _, prefix := range addresses {
			if !hasLocalHostRoute(table, luid, prefix.Addr()) {
				return false, nil
			}
		}
	}
	for _, prefix := range allowed4 {
		if !windowsPrefixRouteIsPresent(luid, prefix, netip.IPv4Unspecified()) {
			return false, nil
		}
	}
	for _, prefix := range allowed6 {
		if !windowsPrefixRouteIsPresent(luid, prefix, netip.IPv6Unspecified()) {
			return false, nil
		}
	}
	return true, nil
}
