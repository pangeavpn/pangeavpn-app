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
	// appliedDNS is what this session set on the host's services; a switch to
	// the same servers skips every networksetup call.
	appliedDNS []string
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

func peekDarwinExtra(tunnelKey string) *darwinSessionExtra {
	darwinExtraMu.Lock()
	defer darwinExtraMu.Unlock()
	return darwinExtra[tunnelKey]
}

func takeDarwinExtra(tunnelKey string) *darwinSessionExtra {
	darwinExtraMu.Lock()
	defer darwinExtraMu.Unlock()
	extra := darwinExtra[tunnelKey]
	delete(darwinExtra, tunnelKey)
	return extra
}

// RestoreOrphanedState undoes network state left by a crashed prior daemon.
// Callers invoke it explicitly at startup; it is no longer an import side effect.
func RestoreOrphanedState() {
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
		if m.trySwitchInPlaceDarwin(ctx, tunnelKey, parsed, allowedIPs) {
			return nil
		}
		if stopErr := m.stopDarwin(ctx, profile); stopErr != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("teardown before device rebuild: %v", stopErr))
		}
	}
	if err := m.reserveSession(tunnelKey); err != nil {
		return err
	}

	// Create in-process TUN device (utun) and WireGuard device.
	dev, tunDev, err := m.createInProcessDeviceWithFactory("utun", parsed.mtu, parsed.wgConfig, tun.CreateTUN)
	if err != nil {
		m.removeSession(tunnelKey)
		return err
	}

	// Get the actual utun interface name assigned by the kernel.
	interfaceName, nameErr := tunDev.Name()
	if nameErr != nil || interfaceName == "" {
		closeDevice(dev)
		m.removeSession(tunnelKey)
		return fmt.Errorf("get utun interface name: %w", nameErr)
	}
	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("in-process wireguard device created on %s", interfaceName))

	// Configure addresses via ioctl.
	if err := configureDarwinAddresses(interfaceName, parsed.addresses); err != nil {
		closeDevice(dev)
		m.removeSession(tunnelKey)
		return fmt.Errorf("configure addresses: %w", err)
	}

	// Bring the interface up via ioctl.
	if err := bringDarwinInterfaceUp(interfaceName); err != nil {
		closeDevice(dev)
		m.removeSession(tunnelKey)
		return fmt.Errorf("bring interface up: %w", err)
	}

	// Add endpoint bypass routes via PF_ROUTE socket.
	endpointRoutes, err := addDarwinEndpointRoutes(ctx, parsed.endpointHosts)
	if err != nil {
		closeDevice(dev)
		m.removeSession(tunnelKey)
		return fmt.Errorf("add endpoint bypass routes: %w", err)
	}

	// Add allowed-IP routes via PF_ROUTE socket.
	if err := addDarwinAllowedIPRoutes(interfaceName, allowedIPs); err != nil {
		removeDarwinEndpointRoutes(endpointRoutes)
		closeDevice(dev)
		m.removeSession(tunnelKey)
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
			m.removeSession(tunnelKey)
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
			m.removeSession(tunnelKey)
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
		deviceMTU:      clampWireGuardDeviceMTU(parsed.mtu),
		endpointRoutes: endpointRoutes,
		dnsOverrides:   dnsOverrides,
	})
	storeDarwinExtra(tunnelKey, &darwinSessionExtra{
		allowedIPs: allowedIPs,
		ipv6States: ipv6States,
		appliedDNS: parsed.dnsServers,
	})

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard started for %s on %s (in-process)", profile.TunnelName, interfaceName))
	return nil
}

// PinEndpointRoutes installs bypass routes for profile's endpoints while the
// previous session still owns routing, so a switch's new transport can dial
// out before the device is re-pointed. No-op without a live session. The
// tunnel never installs "default" (only split routes), so the gateway lookup
// still finds the physical one.
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
	if !ok || session == nil {
		return nil
	}

	added, err := addDarwinEndpointRoutes(ctx, parsed.endpointHosts)
	// Track what landed even on partial failure, so teardown cleans it up.
	session.endpointRoutes = mergeSpecSet(session.endpointRoutes, added)
	return err
}

// trySwitchInPlaceDarwin re-points the live utun at a new server: UAPI peer
// swap (replace_peers), endpoint and allowed-IP route diffs, and DNS/IPv6
// changes only when the target state actually differs — the common switch
// keeps both and skips every networksetup call. Reports false when the caller
// should rebuild the device instead.
//
// Not transactional: a failure after IpcSet leaves the device on the new peer
// briefly — bounded by the caller's immediate stop-and-rebuild.
func (m *wireGuardGoManager) trySwitchInPlaceDarwin(ctx context.Context, tunnelKey string, parsed parsedUserlandConfig, allowedIPs []string) bool {
	m.guardMu.Lock()
	defer m.guardMu.Unlock()

	session, ok := m.session(tunnelKey)
	extra := peekDarwinExtra(tunnelKey)
	if !ok || session == nil || session.device == nil || extra == nil {
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

	newEndpointRoutes, epErr := addDarwinEndpointRoutes(ctx, parsed.endpointHosts)
	if epErr != nil {
		// A failed resolve must not drop tracking of installed routes: the
		// guard only re-pins what is tracked, so keep the union and no stale removal.
		m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: endpoint route warning: %v", epErr))
		session.endpointRoutes = mergeSpecSet(session.endpointRoutes, newEndpointRoutes)
	} else {
		removeDarwinEndpointRoutes(subtractSpecSet(session.endpointRoutes, newEndpointRoutes))
		session.endpointRoutes = newEndpointRoutes
	}

	// Track the union while routes are in flux, so a failure mid-diff still
	// gets everything cleaned up by the fallback teardown.
	oldAllowedIPs := extra.allowedIPs
	extra.allowedIPs = mergeSpecSet(oldAllowedIPs, allowedIPs)
	if err := addDarwinAllowedIPRoutes(session.interfaceName, subtractSpecSet(allowedIPs, oldAllowedIPs)); err != nil {
		m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: allowed-ip routes failed: %v", err))
		return false
	}
	removeDarwinAllowedIPRoutes(session.interfaceName, subtractSpecSet(oldAllowedIPs, allowedIPs))
	extra.allowedIPs = allowedIPs

	newNeedsV6Lock := allowedIPsHaveIPv6(allowedIPs)
	switch {
	case newNeedsV6Lock && len(extra.ipv6States) == 0:
		states, v6Err := disableDarwinIPv6ForSession()
		if v6Err != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: ipv6 lockdown failed: %v", v6Err))
			return false
		}
		extra.ipv6States = states
	case !newNeedsV6Lock && len(extra.ipv6States) > 0:
		restoreDarwinIPv6(extra.ipv6States)
		extra.ipv6States = nil
	}

	if !darwinDNSListsEqual(extra.appliedDNS, parsed.dnsServers) {
		if !m.reapplySessionDNSLocked(session, parsed.dnsServers) {
			return false
		}
		extra.appliedDNS = parsed.dnsServers
	}

	m.logs.Add(state.LogInfo, state.SourceWireGuard, fmt.Sprintf("wireguard re-pointed in place on %s", session.interfaceName))
	return true
}

// reapplySessionDNSLocked moves the session's DNS override to want. The
// recorded pre-tunnel state is kept: overrides written now would capture the
// tunnel's own servers and restore the wrong thing at teardown.
func (m *wireGuardGoManager) reapplySessionDNSLocked(session *tunnelSession, want []string) bool {
	switch {
	case len(want) == 0 && len(session.dnsOverrides) > 0:
		if err := restoreDarwinDNSServers(session.dnsOverrides); err != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: dns restore failed: %v", err))
			return false
		}
		session.dnsOverrides = nil
		clearDarwinDNSState()
	case len(want) > 0 && len(session.dnsOverrides) == 0:
		overrides, err := applyDarwinDNSServers(want)
		if err != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: dns apply failed: %v", err))
			return false
		}
		session.dnsOverrides = overrides
		if persistErr := persistDarwinDNSState(overrides); persistErr != nil {
			m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("persist DNS pre-state failed: %v", persistErr))
		}
	case len(want) > 0:
		for _, override := range session.dnsOverrides {
			if err := setDarwinDNSServers(override.service, want); err != nil {
				m.logs.Add(state.LogWarn, state.SourceWireGuard, fmt.Sprintf("in-place reconfigure: dns update for %s failed: %v", override.service, err))
				return false
			}
		}
	}
	return true
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

	// Wait out any in-flight repair guard before undoing its work.
	m.guardMu.Lock()
	defer m.guardMu.Unlock()

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
