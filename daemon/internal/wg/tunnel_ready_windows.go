//go:build windows

package wg

import (
	"net/netip"

	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// tunnelSessionReady reports whether the adapter can actually carry a datagram:
// every address out of duplicate-address-detection and every AllowedIPs route in.
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

	luid := winipcfg.LUID(session.windowsLUID)
	for _, prefix := range append(addresses4, addresses6...) {
		row, err := luid.IPAddress(prefix.Addr())
		if err != nil || row == nil || row.DadState != winipcfg.DadStatePreferred {
			return false, nil
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
