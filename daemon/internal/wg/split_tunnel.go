//go:build darwin || linux || windows

package wg

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

type splitHookRef struct{ hook SplitTunnelHook }

// SetSplitTunnelHook installs the wrapper every TUN created from now on goes through; nil removes it.
// Set it before the first Start: a device that already exists keeps running unwrapped.
func (m *wireGuardGoManager) SetSplitTunnelHook(hook SplitTunnelHook) {
	if hook == nil {
		m.splitHook.Store(nil)
		return
	}
	m.splitHook.Store(&splitHookRef{hook: hook})
}

func (m *wireGuardGoManager) wrapTUN(tunDev tun.Device, info TunnelInfo) tun.Device {
	ref := m.splitHook.Load()
	if ref == nil || ref.hook == nil {
		return tunDev
	}
	if name, err := tunDev.Name(); err == nil && strings.TrimSpace(name) != "" {
		info.Name = strings.TrimSpace(name)
	}
	if wrapped := ref.hook.WrapTUN(tunDev, info); wrapped != nil {
		return wrapped
	}
	return tunDev
}

// tunnelInfoFor describes the tunnel a validated, DNS-merged config brings up.
func tunnelInfoFor(name string, parsed parsedUserlandConfig) TunnelInfo {
	info := TunnelInfo{Name: name, MTU: clampWireGuardDeviceMTU(parsed.mtu)}
	for _, raw := range parsed.addresses {
		if addr, ok := interfaceAddr(raw); ok && addr.Is4() {
			info.Addresses = append(info.Addresses, addr)
		}
	}
	for _, raw := range parsed.dnsServers {
		if addr, err := netip.ParseAddr(strings.TrimSpace(raw)); err == nil && addr.Unmap().Is4() {
			info.DNS = append(info.DNS, addr.Unmap())
		}
	}
	return info
}

func interfaceAddr(raw string) (netip.Addr, bool) {
	trimmed := strings.TrimSpace(raw)
	if prefix, err := netip.ParsePrefix(trimmed); err == nil {
		return prefix.Addr().Unmap(), true
	}
	if addr, err := netip.ParseAddr(trimmed); err == nil {
		return addr.Unmap(), true
	}
	return netip.Addr{}, false
}

func firstIPv4Address(addresses []string) netip.Addr {
	for _, raw := range addresses {
		if addr, ok := interfaceAddr(raw); ok && addr.Is4() {
			return addr
		}
	}
	return netip.Addr{}
}

// tunnelAddrChanged reports whether a wrapped device was built for another tunnel address; the
// wrapper fixes it at creation, so only a rebuild can move it.
func tunnelAddrChanged(tunDev tun.Device, addresses []string) bool {
	wrapped, ok := tunDev.(interface{ TunnelAddr() netip.Addr })
	if !ok {
		return false
	}
	return wrapped.TunnelAddr() != firstIPv4Address(addresses)
}

func updateWrappedTunnelInfo(session *tunnelSession, parsed parsedUserlandConfig) {
	if updater, ok := session.tunDevice.(interface{ UpdateTunnelInfo(TunnelInfo) }); ok {
		updater.UpdateTunnelInfo(tunnelInfoFor(session.interfaceName, parsed))
	}
}

// ApplyAllowedIPs moves the live device and its tunnel routes to profile's AllowedIPs. Keys,
// endpoint and transport stay as they are, so no handshake is needed and nothing reconnects.
func (m *wireGuardGoManager) ApplyAllowedIPs(ctx context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return errors.New("wireguard tunnelName is required")
	}
	parsed, err := parseUserlandConfig(profile.ConfigText)
	if err != nil {
		return err
	}
	parsed.dnsServers = mergeDNSServers(parsed.dnsServers, profile.DNS)
	allowedIPs, err := validateParsedIPv4Only(parsed)
	if err != nil {
		return err
	}
	if len(allowedIPs) == 0 {
		return errors.New("apply allowed ips: config has no IPv4 AllowedIPs")
	}
	uapi, peerKeys, err := allowedIPsUAPI(parsed.wgConfig)
	if err != nil {
		return fmt.Errorf("apply allowed ips: %w", err)
	}

	m.guardMu.Lock()
	defer m.guardMu.Unlock()

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	session, ok := m.session(tunnelKey)
	if !ok || session == nil || session.device == nil {
		return fmt.Errorf("wireguard tunnel %s is not running", profile.TunnelName)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := applyDeviceAllowedIPs(session.device, uapi, peerKeys); err != nil {
		return err
	}
	if err := m.syncAllowedIPRoutes(ctx, tunnelKey, session, parsed, allowedIPs); err != nil {
		return fmt.Errorf("apply allowed ips: route sync: %w", err)
	}
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("applied %d allowed-ip entries in place on %s", len(allowedIPs), session.interfaceName))
	return nil
}

// applyDeviceAllowedIPs refuses a config whose peers are not on the device: update_only would
// silently skip them while the routes still moved.
func applyDeviceAllowedIPs(dev *device.Device, uapi string, peerKeys []string) error {
	current, err := dev.IpcGet()
	if err != nil {
		return fmt.Errorf("apply allowed ips: read device: %w", err)
	}
	live := map[string]struct{}{}
	for line := range strings.SplitSeq(current, "\n") {
		if key, ok := strings.CutPrefix(line, "public_key="); ok {
			live[key] = struct{}{}
		}
	}
	for _, key := range peerKeys {
		if _, ok := live[key]; !ok {
			return errors.New("apply allowed ips: peer is not configured on the live device")
		}
	}
	if err := dev.IpcSet(uapi); err != nil {
		return fmt.Errorf("apply allowed ips: uapi apply failed: %w", err)
	}
	return nil
}

// syncTrackedAllowedIPs diffs the routes (0.0.0.0/0 is installed as two /1s) and adds before removing, route by
// route until ctx ends; tracked only ever lists installed routes, so a retry re-adds whatever was skipped.
func syncTrackedAllowedIPs(ctx context.Context, tracked *[]string, want []string, routes func([]string) []string, add func([]string) error, remove func([]string)) error {
	old := *tracked
	oldRoutes, wantRoutes := routes(old), routes(want)
	toAdd, toRemove := subtractSpecSet(wantRoutes, oldRoutes), subtractSpecSet(oldRoutes, wantRoutes)
	for i, r := range toAdd {
		err := ctx.Err()
		if err == nil {
			err = add([]string{r})
		}
		if err != nil {
			*tracked = mergeSpecSet(old, toAdd[:i])
			return err
		}
	}
	for i, r := range toRemove {
		if err := ctx.Err(); err != nil {
			*tracked = mergeSpecSet(want, toRemove[i:])
			return err
		}
		remove([]string{r})
	}
	*tracked = want
	return nil
}
