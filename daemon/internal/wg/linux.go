//go:build linux

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
	return m.startLinux(ctx, profile)
}

func (m *wireGuardGoManager) Stop(ctx context.Context, profile state.WireGuardProfile) error {
	return m.stopLinux(ctx, profile)
}

func (m *wireGuardGoManager) Status(ctx context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
	return m.statusLinux(ctx, profile)
}

func (m *wireGuardGoManager) startLinux(ctx context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return errors.New("wireguard tunnelName is required")
	}
	if strings.TrimSpace(profile.ConfigText) == "" {
		return errors.New("wireguard configText is required")
	}
	if os.Geteuid() != 0 {
		return errors.New("wireguard on Linux requires the daemon to run as root")
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
	interfaceName := linuxInterfaceName(tunnelKey)

	if m.hasActiveDevice(tunnelKey) {
		return fmt.Errorf("wireguard tunnel %s is already running", profile.TunnelName)
	}

	// Create in-process TUN device and WireGuard device.
	dev, tunDev, err := m.createInProcessDevice(interfaceName, parsed.mtu, parsed.wgConfig)
	if err != nil {
		return err
	}

	// Get the actual interface name assigned by the kernel.
	actualName, nameErr := tunDev.Name()
	if nameErr == nil && actualName != "" {
		interfaceName = actualName
	}
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("in-process wireguard device created on %s", interfaceName))

	// Configure addresses via netlink.
	if err := configureLinuxAddresses(interfaceName, parsed.addresses); err != nil {
		closeDevice(dev)
		return fmt.Errorf("configure addresses: %w", err)
	}

	// Bring the interface up via netlink.
	if err := bringLinuxInterfaceUp(interfaceName); err != nil {
		closeDevice(dev)
		return fmt.Errorf("bring interface up: %w", err)
	}

	// Add endpoint bypass routes via netlink.
	endpointRoutes, _ := addLinuxEndpointRoutes(ctx, parsed.endpointHosts)

	// Add allowed-IP routes via netlink.
	if err := addLinuxAllowedIPRoutes(interfaceName, allowedIPs); err != nil {
		removeLinuxEndpointRoutes(endpointRoutes)
		closeDevice(dev)
		return fmt.Errorf("add allowed-ip routes: %w", err)
	}

	// Configure DNS via D-Bus (systemd-resolved) or resolv.conf fallback.
	var linuxDNS *linuxDNSOverride
	if len(parsed.dnsServers) > 0 {
		override, dnsErr := applyLinuxDNSServers(interfaceName, parsed.dnsServers)
		if dnsErr != nil {
			removeLinuxAllowedIPRoutes(interfaceName, allowedIPs)
			removeLinuxEndpointRoutes(endpointRoutes)
			closeDevice(dev)
			return fmt.Errorf("apply DNS: %w", dnsErr)
		}
		linuxDNS = override
		m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("applied DNS servers %s via %s", strings.Join(parsed.dnsServers, ", "), override.mode))
	}

	m.storeSession(tunnelKey, &tunnelSession{
		interfaceName:    interfaceName,
		device:           dev,
		tunDevice:        tunDev,
		endpointRoutes:   endpointRoutes,
		linuxDNSOverride: linuxDNS,
	})

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard started for %s on %s (in-process)", profile.TunnelName, interfaceName))
	return nil
}

func (m *wireGuardGoManager) stopLinux(ctx context.Context, profile state.WireGuardProfile) error {
	_ = ctx
	if strings.TrimSpace(profile.TunnelName) == "" {
		return nil
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	session, hasSession := m.session(tunnelKey)
	if !hasSession || session == nil {
		return nil
	}

	interfaceName := session.interfaceName

	// Restore DNS.
	if session.linuxDNSOverride != nil {
		if err := restoreLinuxDNSServers(session.linuxDNSOverride); err != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("restore DNS failed: %v", err))
		} else {
			m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("restored DNS settings (%s)", session.linuxDNSOverride.mode))
		}
	}

	// Remove endpoint routes.
	removeLinuxEndpointRoutes(session.endpointRoutes)

	// Close the WireGuard device (also closes TUN).
	closeDevice(session.device)

	// The TUN device removal should also remove the interface, but ensure cleanup.
	_ = deleteLinuxInterface(interfaceName)

	m.removeSession(tunnelKey)
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard stopped for %s (%s)", profile.TunnelName, interfaceName))
	return nil
}

func (m *wireGuardGoManager) statusLinux(_ context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
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

func linuxInterfaceName(tunnelKey string) string {
	name := strings.TrimSpace(tunnelKey)
	if name == "" {
		return "wg0"
	}
	if len(name) <= 15 {
		return name
	}
	return name[:15]
}
