//go:build darwin

package wg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func (m *wireGuardGoManager) Start(ctx context.Context, profile state.WireGuardProfile) error {
	return m.startDarwin(ctx, profile)
}

func (m *wireGuardGoManager) Stop(ctx context.Context, profile state.WireGuardProfile) error {
	return m.stopDarwin(ctx, profile)
}

func (m *wireGuardGoManager) Status(ctx context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
	return m.statusDarwin(ctx, profile)
}

func (m *wireGuardGoManager) startDarwin(ctx context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return errors.New("wireguard tunnelName is required")
	}
	if strings.TrimSpace(profile.ConfigText) == "" {
		return errors.New("wireguard configText is required")
	}
	if os.Geteuid() != 0 {
		return errors.New("wireguard on macOS requires the daemon to run as root")
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

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	if m.hasActiveDevice(tunnelKey) {
		return fmt.Errorf("wireguard tunnel %s is already running", profile.TunnelName)
	}

	// Create in-process TUN device (utun) and WireGuard device.
	dev, tunDev, err := m.createInProcessDevice("utun", parsed.mtu, parsed.wgConfig)
	if err != nil {
		return err
	}

	// Get the actual utun interface name assigned by the kernel.
	interfaceName, nameErr := tunDev.Name()
	if nameErr != nil || interfaceName == "" {
		closeDevice(dev)
		return fmt.Errorf("get utun interface name: %w", nameErr)
	}
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("in-process wireguard device created on %s", interfaceName))

	// Configure addresses via ioctl.
	if err := configureDarwinAddresses(interfaceName, parsed.addresses); err != nil {
		closeDevice(dev)
		return fmt.Errorf("configure addresses: %w", err)
	}

	// Bring the interface up via ioctl.
	if err := bringDarwinInterfaceUp(interfaceName); err != nil {
		closeDevice(dev)
		return fmt.Errorf("bring interface up: %w", err)
	}

	// Add endpoint bypass routes via PF_ROUTE socket.
	endpointRoutes, _ := addDarwinEndpointRoutes(ctx, parsed.endpointHosts)

	// Add allowed-IP routes via PF_ROUTE socket.
	if err := addDarwinAllowedIPRoutes(interfaceName, allowedIPs); err != nil {
		removeDarwinEndpointRoutes(endpointRoutes)
		closeDevice(dev)
		return fmt.Errorf("add allowed-ip routes: %w", err)
	}

	// Configure DNS via SystemConfiguration.
	dnsOverrides := make([]darwinDNSOverride, 0, 4)
	if len(parsed.dnsServers) > 0 {
		overrides, dnsErr := applyDarwinDNSServers(parsed.dnsServers)
		if dnsErr != nil {
			removeDarwinAllowedIPRoutes(allowedIPs)
			removeDarwinEndpointRoutes(endpointRoutes)
			closeDevice(dev)
			return fmt.Errorf("apply DNS: %w", dnsErr)
		}
		dnsOverrides = overrides
		m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("applied macOS DNS servers %s on %d services", strings.Join(parsed.dnsServers, ", "), len(dnsOverrides)))
	}

	m.storeSession(tunnelKey, &tunnelSession{
		interfaceName:  interfaceName,
		device:         dev,
		tunDevice:      tunDev,
		endpointRoutes: endpointRoutes,
		dnsOverrides:   dnsOverrides,
	})

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard started for %s on %s (in-process)", profile.TunnelName, interfaceName))
	return nil
}

func (m *wireGuardGoManager) stopDarwin(_ context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return nil
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	session, hasSession := m.session(tunnelKey)
	if !hasSession || session == nil {
		return nil
	}

	// Restore DNS.
	if len(session.dnsOverrides) > 0 {
		if err := restoreDarwinDNSServers(session.dnsOverrides); err != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("restore macOS DNS failed: %v", err))
		} else {
			m.logs.Add(state.LogInfo, state.SourceWireGuard, "restored macOS DNS settings")
		}
	}

	// Remove endpoint routes.
	removeDarwinEndpointRoutes(session.endpointRoutes)

	// Close the WireGuard device (also closes TUN and removes interface).
	closeDevice(session.device)

	m.removeSession(tunnelKey)
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard stopped for %s (%s)", profile.TunnelName, session.interfaceName))
	return nil
}

func (m *wireGuardGoManager) statusDarwin(_ context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return state.WireGuardStatus{Running: false, Detail: "missing tunnelName"}, nil
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	if m.hasActiveDevice(tunnelKey) {
		session, _ := m.session(tunnelKey)
		rxBytes, txBytes := peerTransferStats(session.device)
		return state.WireGuardStatus{
			Running:  true,
			Detail:   fmt.Sprintf("interface %s running (in-process)", session.interfaceName),
			BytesIn:  rxBytes,
			BytesOut: txBytes,
		}, nil
	}

	return state.WireGuardStatus{
		Running: false,
		Detail:  "not running",
	}, nil
}
