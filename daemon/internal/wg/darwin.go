//go:build darwin

package wg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// darwinSessionExtra holds per-session darwin state that tunnelSession (a
// type shared across platforms) has no field for.
type darwinSessionExtra struct {
	allowedIPs []string
	ipv6States []darwinIPv6State
}

var (
	darwinExtraMu sync.Mutex
	darwinExtra   = map[string]*darwinSessionExtra{}
)

func storeDarwinExtra(tunnelKey string, extra *darwinSessionExtra) {
	darwinExtraMu.Lock()
	defer darwinExtraMu.Unlock()
	darwinExtra[tunnelKey] = extra
}

func takeDarwinExtra(tunnelKey string) *darwinSessionExtra {
	darwinExtraMu.Lock()
	defer darwinExtraMu.Unlock()
	extra := darwinExtra[tunnelKey]
	delete(darwinExtra, tunnelKey)
	return extra
}

func init() {
	// A previous daemon process may have died mid-session, leaving every
	// network service pinned to a now-unreachable tunnel DNS server.
	restoreOrphanedDarwinDNSState()
}

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
	dev, tunDev, err := m.createInProcessDeviceWithFactory("utun", parsed.mtu, parsed.wgConfig, tun.CreateTUN)
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
	endpointRoutes, err := addDarwinEndpointRoutes(ctx, parsed.endpointHosts)
	if err != nil {
		closeDevice(dev)
		return fmt.Errorf("add endpoint bypass routes: %w", err)
	}

	// Add allowed-IP routes via PF_ROUTE socket.
	if err := addDarwinAllowedIPRoutes(interfaceName, allowedIPs); err != nil {
		removeDarwinEndpointRoutes(endpointRoutes)
		closeDevice(dev)
		return fmt.Errorf("add allowed-ip routes: %w", err)
	}

	// IPv6 AllowedIPs can never actually route into a v4-only tunnel, so
	// disable IPv6 system-wide instead of letting it silently leak.
	var ipv6States []darwinIPv6State
	if allowedIPsHaveIPv6(allowedIPs) {
		states, ipv6Err := disableDarwinIPv6ForSession()
		if ipv6Err != nil {
			removeDarwinAllowedIPRoutes(interfaceName, allowedIPs)
			removeDarwinEndpointRoutes(endpointRoutes)
			closeDevice(dev)
			return fmt.Errorf("disable ipv6 for session: %w", ipv6Err)
		}
		ipv6States = states
		m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("disabled ipv6 on %d services to prevent tunnel bypass", len(ipv6States)))
	}

	// Configure DNS via SystemConfiguration.
	dnsOverrides := make([]darwinDNSOverride, 0, 4)
	if len(parsed.dnsServers) > 0 {
		overrides, dnsErr := applyDarwinDNSServers(parsed.dnsServers)
		if dnsErr != nil {
			restoreDarwinIPv6(ipv6States)
			removeDarwinAllowedIPRoutes(interfaceName, allowedIPs)
			removeDarwinEndpointRoutes(endpointRoutes)
			closeDevice(dev)
			return fmt.Errorf("apply DNS: %w", dnsErr)
		}
		dnsOverrides = overrides
		if persistErr := persistDarwinDNSState(dnsOverrides); persistErr != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("persist DNS pre-state failed: %v", persistErr))
		}
		m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("applied macOS DNS servers %s on %d services", strings.Join(parsed.dnsServers, ", "), len(dnsOverrides)))
	}

	m.storeSession(tunnelKey, &tunnelSession{
		interfaceName:  interfaceName,
		device:         dev,
		tunDevice:      tunDev,
		endpointRoutes: endpointRoutes,
		dnsOverrides:   dnsOverrides,
	})
	storeDarwinExtra(tunnelKey, &darwinSessionExtra{
		allowedIPs: allowedIPs,
		ipv6States: ipv6States,
	})

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard started for %s on %s (in-process)", profile.TunnelName, interfaceName))
	return nil
}

func (m *wireGuardGoManager) stopDarwin(_ context.Context, profile state.WireGuardProfile) error {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return nil
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	session, hasSession := m.takeSession(tunnelKey)
	extra := takeDarwinExtra(tunnelKey)
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
		clearDarwinDNSState()
	}

	// Restore IPv6 on any services it was disabled on for this session.
	if extra != nil && len(extra.ipv6States) > 0 {
		restoreDarwinIPv6(extra.ipv6States)
	}

	// Remove endpoint routes.
	removeDarwinEndpointRoutes(session.endpointRoutes)

	// Remove allowed-IP routes.
	if extra != nil {
		removeDarwinAllowedIPRoutes(session.interfaceName, extra.allowedIPs)
	}

	// Close the WireGuard device (also closes TUN and removes interface).
	closeDevice(session.device)

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard stopped for %s (%s)", profile.TunnelName, session.interfaceName))
	return nil
}

func (m *wireGuardGoManager) statusDarwin(_ context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
	if strings.TrimSpace(profile.TunnelName) == "" {
		return state.WireGuardStatus{Running: false, Detail: "missing tunnelName"}, nil
	}

	tunnelKey := sanitizeTunnelName(profile.TunnelName)
	if session, ok := m.session(tunnelKey); ok && session != nil && session.device != nil {
		rxBytes, txBytes, lastHandshake, statsErr := peerStats(session.device)
		if statsErr != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("read wireguard stats failed: %v", statsErr))
		}
		return state.WireGuardStatus{
			Running:           true,
			Detail:            fmt.Sprintf("interface %s running (in-process)", session.interfaceName),
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
