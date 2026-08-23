//go:build windows

package wg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// RestoreOrphanedState is a no-op on Windows: WFP filters and routes don't
// survive process exit the way darwin/linux network config files do.
func RestoreOrphanedState() {}

func (m *wireGuardGoManager) Start(ctx context.Context, profile state.WireGuardProfile) error {
	return m.startWindows(ctx, profile)
}

func (m *wireGuardGoManager) Stop(ctx context.Context, profile state.WireGuardProfile) error {
	return m.stopWindows(ctx, profile)
}

func (m *wireGuardGoManager) Status(ctx context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
	return m.statusWindows(ctx, profile)
}

func (m *wireGuardGoManager) startWindows(ctx context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return errors.New("wireguard tunnelName is required")
	}
	if strings.TrimSpace(profile.ConfigText) == "" {
		return errors.New("wireguard configText is required")
	}
	if !windows.GetCurrentProcessToken().IsElevated() {
		return errors.New("wireguard on Windows requires administrator privileges")
	}

	parsed, err := parseUserlandConfig(profile.ConfigText)
	if err != nil {
		return err
	}
	parsed.endpointHosts = mergeEndpointHosts(parsed.endpointHosts, profile.BypassHosts)
	parsed.dnsServers = mergeDNSServers(parsed.dnsServers, profile.DNS)
	allowedIPs, err := validateParsedIPv4Only(parsed)
	if err != nil {
		return err
	}
	m.logs.Add(
		state.LogDebug,
		state.SourceWireGuard,
		fmt.Sprintf(
			"wireguard profile summary: tunnel=%s addresses=%s dns=%s endpoints=%s",
			profile.TunnelName,
			formatDebugStringList(parsed.addresses),
			formatDebugStringList(parsed.dnsServers),
			formatDebugStringList(parsed.endpointHosts),
		),
	)
	m.logs.Add(state.LogDebug, state.SourceWireGuard, fmt.Sprintf("wireguard allowed IPv4 route entries: %d", len(allowedIPs)))

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	if m.hasActiveDevice(tunnelKey) {
		if m.trySwitchInPlace(ctx, tunnelKey, parsed, allowedIPs) {
			return nil
		}
		if stopErr := m.stopWindows(ctx, profile); stopErr != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("teardown before device rebuild: %v", stopErr))
		}
	}
	if err := m.reserveSession(tunnelKey); err != nil {
		return err
	}

	requestedName := strings.TrimSpace(profile.TunnelName)
	requestedGUID := requestedWindowsTunnelGUID(requestedName)
	dev, tunDev, err := m.createInProcessDeviceWithFactory(
		requestedName,
		parsed.mtu,
		parsed.wgConfig,
		func(interfaceName string, mtu int) (tun.Device, error) {
			return tun.CreateTUNWithRequestedGUID(interfaceName, requestedGUID, mtu)
		},
	)
	if err != nil {
		m.removeSession(tunnelKey)
		return err
	}

	interfaceName := requestedName
	if name, nameErr := tunDev.Name(); nameErr == nil && strings.TrimSpace(name) != "" {
		interfaceName = strings.TrimSpace(name)
	}
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("in-process wireguard device created on %s", interfaceName))

	tunnelLUID, err := windowsInterfaceLUID(tunDev, interfaceName)
	if err != nil {
		closeDevice(dev)
		m.removeSession(tunnelKey)
		return err
	}

	// Every tunnel on the box is off limits as a next hop for the bypass, not
	// just this one: routing WireGuard through any of them is a loop.
	excludeLUIDs := m.ActiveLUIDs()
	excludeLUIDs[tunnelLUID] = struct{}{}

	endpointRoutes, endpointErr := addWindowsEndpointRoutes(ctx, excludeLUIDs, parsed.endpointHosts)
	if endpointErr != nil {
		_ = removeWindowsEndpointRoutes(endpointRoutes)
		closeDevice(dev)
		m.removeSession(tunnelKey)
		return fmt.Errorf("endpoint bypass route setup: %w", endpointErr)
	}

	if err := configureWindowsInterface(tunnelLUID, parsed.addresses, allowedIPs, parsed.dnsServers, parsed.mtu); err != nil {
		_ = removeWindowsEndpointRoutes(endpointRoutes)
		closeDevice(dev)
		m.removeSession(tunnelKey)
		return fmt.Errorf("configure windows interface: %w", err)
	}
	if len(parsed.dnsServers) > 0 {
		m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("applied Windows DNS servers %s", strings.Join(parsed.dnsServers, ", ")))
	}

	m.storeSession(tunnelKey, &tunnelSession{
		interfaceName: interfaceName,
		device:        dev,
		tunDevice:     tunDev,
		deviceMTU:     clampWireGuardDeviceMTU(parsed.mtu),
		windowsLUID:   tunnelLUID,
		windowsRoutes: endpointRoutes,
	})

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard started for %s on %s (in-process)", profile.TunnelName, interfaceName))
	return nil
}

// PinEndpointRoutes installs bypass routes for profile's endpoints while the
// previous session still owns the routing table, so a switch's new transport
// can dial out before the device is re-pointed. No-op without a live session.
func (m *wireGuardGoManager) PinEndpointRoutes(ctx context.Context, profile state.WireGuardProfile) error {
	parsed, err := parseUserlandConfig(profile.ConfigText)
	if err != nil {
		return err
	}
	parsed.endpointHosts = mergeEndpointHosts(parsed.endpointHosts, profile.BypassHosts)

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	m.guardMu.Lock()
	defer m.guardMu.Unlock()
	session, ok := m.session(tunnelKey)
	if !ok || session == nil || session.windowsLUID == 0 {
		return nil
	}

	added, err := addWindowsEndpointRoutes(ctx, m.ActiveLUIDs(), parsed.endpointHosts)
	// Track what landed even on partial failure, so teardown cleans it up.
	session.windowsRoutes = mergeWindowsRouteSpecs(session.windowsRoutes, added)
	return err
}

// trySwitchInPlace re-points the live device at a new server: UAPI peer swap
// (replace_peers), endpoint bypass route diff, interface reconfigure. Reports
// false when the caller should rebuild the device instead.
//
// Not transactional: a failure after IpcSet leaves the device on the new peer
// briefly — bounded by the caller's immediate stop-and-rebuild.
func (m *wireGuardGoManager) trySwitchInPlace(ctx context.Context, tunnelKey string, parsed parsedUserlandConfig, allowedIPs []string) bool {
	m.guardMu.Lock()
	defer m.guardMu.Unlock()

	session, ok := m.session(tunnelKey)
	if !ok || session == nil || session.device == nil || session.windowsLUID == 0 {
		return false
	}
	if clampWireGuardDeviceMTU(parsed.mtu) != session.deviceMTU {
		m.logs.Add(state.LogInfo, state.SourceWireGuard, "mtu changed; rebuilding the device instead of reconfiguring in place")
		return false
	}

	uapi, err := wgConfigToUAPI(parsed.wgConfig)
	if err != nil {
		m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: uapi translation failed: %v", err))
		return false
	}
	if err := session.device.IpcSet(uapi); err != nil {
		m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: uapi apply failed: %v", err))
		return false
	}

	newRoutes, routeErr := addWindowsEndpointRoutes(ctx, m.ActiveLUIDs(), parsed.endpointHosts)
	if routeErr != nil {
		// The transport already dialled through routes pinned earlier; a route
		// the repair guard can re-pin later is not worth a device rebuild.
		m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: endpoint route warning: %v", routeErr))
	}
	stale := subtractWindowsRouteSpecs(session.windowsRoutes, newRoutes)
	if err := removeWindowsEndpointRoutes(stale); err != nil {
		m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: stale route cleanup warning: %v", err))
		newRoutes = mergeWindowsRouteSpecs(newRoutes, stale)
	}
	session.windowsRoutes = newRoutes

	if err := configureWindowsInterface(session.windowsLUID, parsed.addresses, allowedIPs, parsed.dnsServers, parsed.mtu); err != nil {
		m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: interface config failed: %v", err))
		return false
	}

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard re-pointed in place on %s", session.interfaceName))
	return true
}

func mergeWindowsRouteSpecs(existing, extra []windowsRouteSpec) []windowsRouteSpec {
	seen := make(map[windowsRouteSpec]struct{}, len(existing)+len(extra))
	merged := make([]windowsRouteSpec, 0, len(existing)+len(extra))
	for _, spec := range append(append([]windowsRouteSpec{}, existing...), extra...) {
		if _, dup := seen[spec]; dup {
			continue
		}
		seen[spec] = struct{}{}
		merged = append(merged, spec)
	}
	return merged
}

func subtractWindowsRouteSpecs(from, remove []windowsRouteSpec) []windowsRouteSpec {
	keep := make(map[windowsRouteSpec]struct{}, len(remove))
	for _, spec := range remove {
		keep[spec] = struct{}{}
	}
	var out []windowsRouteSpec
	for _, spec := range from {
		if _, kept := keep[spec]; !kept {
			out = append(out, spec)
		}
	}
	return out
}

func (m *wireGuardGoManager) stopWindows(_ context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return nil
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	session, hasSession := m.takeSession(tunnelKey)
	if !hasSession || session == nil {
		return nil
	}

	// Wait out any in-flight repair guard before undoing its work.
	m.guardMu.Lock()
	defer m.guardMu.Unlock()

	if len(session.windowsRoutes) > 0 {
		if err := removeWindowsEndpointRoutes(session.windowsRoutes); err != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("endpoint route cleanup warning: %v", err))
		}
	}

	if session.windowsLUID != 0 {
		if err := clearWindowsInterfaceConfig(session.windowsLUID); err != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("windows interface cleanup warning: %v", err))
		}
	}

	closeDevice(session.device)
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard stopped for %s (%s)", profile.TunnelName, session.interfaceName))
	return nil
}

func (m *wireGuardGoManager) statusWindows(_ context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return state.WireGuardStatus{Running: false, Detail: "missing tunnelName"}, nil
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	rxBytes, txBytes, lastHandshake, active, err := m.sessionStats(tunnelKey)
	if err != nil {
		return state.WireGuardStatus{}, fmt.Errorf("read wireguard status: %w", err)
	}
	if active {
		interfaceName := profile.TunnelName
		if session, ok := m.session(tunnelKey); ok && session != nil {
			interfaceName = session.interfaceName
		}
		return state.WireGuardStatus{
			Running:           true,
			Detail:            fmt.Sprintf("interface %s running (in-process)", interfaceName),
			BytesIn:           rxBytes,
			BytesOut:          txBytes,
			LastHandshakeUnix: lastHandshake,
		}, nil
	}

	return state.WireGuardStatus{
		Running: false,
		Detail:  "not running",
	}, nil
}
