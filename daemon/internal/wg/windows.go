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
		return fmt.Errorf("wireguard tunnel %s is already running", profile.TunnelName)
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
		return fmt.Errorf("endpoint bypass route setup: %w", endpointErr)
	}

	if err := configureWindowsInterface(tunnelLUID, parsed.addresses, allowedIPs, parsed.dnsServers, parsed.mtu); err != nil {
		_ = removeWindowsEndpointRoutes(endpointRoutes)
		closeDevice(dev)
		return fmt.Errorf("configure windows interface: %w", err)
	}
	if len(parsed.dnsServers) > 0 {
		m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("applied Windows DNS servers %s", strings.Join(parsed.dnsServers, ", ")))
	}

	m.storeSession(tunnelKey, &tunnelSession{
		interfaceName: interfaceName,
		device:        dev,
		tunDevice:     tunDev,
		windowsLUID:   tunnelLUID,
		windowsRoutes: endpointRoutes,
	})

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard started for %s on %s (in-process)", profile.TunnelName, interfaceName))
	return nil
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
