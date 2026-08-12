//go:build windows

package platform

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		return &windowsKillSwitch{}
	}
}

type windowsKillSwitch struct {
	mu             sync.Mutex
	active         bool
	engine         *wfpEngine // dynamic session — closing handle removes all filters
	tunnelFilterId uint64     // WFP filter ID for the tunnel interface permit

	// Per-arm permit filter IDs so a re-arm can retire the previous set instead
	// of stacking. Without this every node visited stays permitted until Clear.
	endpointFilterIds []uint64
	lanFilterIds      []uint64
}

func (ks *windowsKillSwitch) Enable(ctx context.Context, endpointHosts []string, allowLAN bool, locked bool) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ips, err := resolveEndpointHosts(ctx, endpointHosts)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}

	// Re-entry: swap the permit set in one transaction — new permits in before
	// old ones out, so the lock is never wider than old ∪ new nor too narrow.
	if ks.active && ks.engine != nil {
		prev, _ := loadKillSwitchState()
		if stringSlicesEqual(prev.EndpointIPs, ips) && prev.AllowLAN == allowLAN {
			// Filters already match, but a caller re-arming an existing lock as a
			// Lockdown lock still has to be recorded — see persistLockedUpgrade.
			return persistLockedUpgrade(prev, locked)
		}

		if err := ks.engine.beginTransaction(); err != nil {
			return fmt.Errorf("kill switch re-enable: %w", err)
		}
		endpointIds := make([]uint64, 0, len(ips))
		for _, ip := range ips {
			id, err := ks.engine.addPermitEndpointIP(ip)
			if err != nil {
				ks.engine.abortTransaction()
				return fmt.Errorf("kill switch re-enable: permit %s: %w", ip, err)
			}
			endpointIds = append(endpointIds, id)
		}
		lanIds := make([]uint64, 0, len(LANAllowPrefixes))
		if allowLAN {
			for _, cidr := range LANAllowPrefixes {
				id, err := ks.engine.addPermitIPv4Subnet(cidr)
				if err != nil {
					ks.engine.abortTransaction()
					return fmt.Errorf("kill switch re-enable: permit LAN %s: %w", cidr, err)
				}
				lanIds = append(lanIds, id)
			}
		}
		// Old permits out last, same transaction.
		var staleDeletes []string
		for _, id := range ks.endpointFilterIds {
			if err := ks.engine.deleteFilter(id); err != nil {
				staleDeletes = append(staleDeletes, fmt.Sprintf("%d: %v", id, err))
			}
		}
		for _, id := range ks.lanFilterIds {
			if err := ks.engine.deleteFilter(id); err != nil {
				staleDeletes = append(staleDeletes, fmt.Sprintf("%d: %v", id, err))
			}
		}
		if err := ks.engine.commitTransaction(); err != nil {
			// Otherwise every later re-arm fails with "transaction in progress".
			ks.engine.abortTransaction()
			return fmt.Errorf("kill switch re-enable: %w", err)
		}
		ks.endpointFilterIds = endpointIds
		ks.lanFilterIds = lanIds
		if len(staleDeletes) > 0 {
			// Lock is intact, but a departing server's permit survives until Clear.
			KillSwitchWarn("kill switch re-arm could not retire stale permits (%s)", strings.Join(staleDeletes, "; "))
		}

		prev.Active = true // the load above may have failed; rules are live
		prev.EndpointIPs = ips
		prev.AllowLAN = allowLAN
		prev.Locked = locked
		_ = saveKillSwitchState(prev)
		return nil
	}

	// Persist state for crash recovery. No PreviousPolicy — WFP doesn't
	// modify the Windows Firewall outbound policy.
	st := KillSwitchState{
		Active:      true,
		AllowLAN:    allowLAN,
		EndpointIPs: ips,
		Locked:      locked,
	}
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch enable: save state: %w", err)
	}

	engine, err := wfpOpen()
	if err != nil {
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	if err := engine.beginTransaction(); err != nil {
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	if err := engine.addSublayer(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	if _, err := engine.addBlockAllOutbound(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	if _, err := engine.addPermitLoopback(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	// Permit the loopback subnet by address too — the IS_LOOPBACK flag above is
	// not reliably set for fresh inter-process TCP connects, and the local
	// daemon API (127.0.0.1:8787) must never be blocked.
	if _, err := engine.addPermitLoopbackSubnet(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	endpointIds := make([]uint64, 0, len(ips))
	for _, ip := range ips {
		id, err := engine.addPermitEndpointIP(ip)
		if err != nil {
			engine.abortTransaction()
			engine.close()
			_ = removeKillSwitchState()
			return fmt.Errorf("kill switch enable: %w", err)
		}
		endpointIds = append(endpointIds, id)
	}

	// DHCP best-effort — ALE layer may not see broadcast DHCP traffic.
	_, _ = engine.addPermitDHCP()

	// Permit LAN ranges so captive portals / gateway probes / mDNS work on
	// restrictive WiFi. Only applied when the user opts in — fail-open would
	// defeat the kill switch.
	lanIds := make([]uint64, 0, len(LANAllowPrefixes))
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			id, err := engine.addPermitIPv4Subnet(cidr)
			if err != nil {
				engine.abortTransaction()
				engine.close()
				_ = removeKillSwitchState()
				return fmt.Errorf("kill switch enable: permit LAN %s: %w", cidr, err)
			}
			lanIds = append(lanIds, id)
		}
	}

	// Block all IPv6 traffic except loopback to prevent IPv6 leaks.
	if _, err := engine.addBlockAllOutboundV6(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}
	if _, err := engine.addBlockAllInboundV6(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}
	if _, err := engine.addPermitLoopbackV6(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	// localhost resolves to ::1 first on Windows, so IPv6 loopback needs the
	// same treatment as IPv4: ::1 address permits alongside the IS_LOOPBACK
	// flag (unreliable for fresh connects), and permits at the inbound layer —
	// the inbound V6 block otherwise drops the server side of every [::1]
	// connection, making localhost websites unreachable while connected.
	if _, err := engine.addPermitLoopbackSubnetV6(fwpmLayerAleAuthConnectV6, "PangeaVPN Allow Loopback Subnet IPv6"); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}
	if _, err := engine.addPermitLoopbackInboundV6(); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}
	if _, err := engine.addPermitLoopbackSubnetV6(fwpmLayerAleAuthRecvAcceptV6, "PangeaVPN Allow Loopback Subnet Inbound IPv6"); err != nil {
		engine.abortTransaction()
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	if err := engine.commitTransaction(); err != nil {
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	ks.engine = engine
	ks.active = true
	ks.endpointFilterIds = endpointIds
	ks.lanFilterIds = lanIds
	return nil
}

func (ks *windowsKillSwitch) Update(ctx context.Context, tunnel TunnelRef) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active || ks.engine == nil {
		return fmt.Errorf("kill switch not active")
	}

	luid, err := resolveTunnelLUID(tunnel)
	if err != nil {
		return fmt.Errorf("kill switch update: %w", err)
	}

	// Remove previous tunnel permit if we're being called again (e.g. reconnect).
	if ks.tunnelFilterId != 0 {
		_ = ks.engine.deleteFilter(ks.tunnelFilterId)
		ks.tunnelFilterId = 0
	}

	// App traffic hits the WFP ALE_AUTH_CONNECT layer before reaching the TUN
	// adapter. Without a permit scoped to the tunnel interface LUID, the
	// block-all-outbound rule drops every packet at socket level — explaining
	// "general failure" on ping even though the WireGuard handshake succeeds.
	filterId, err := ks.engine.addPermitTunnelInterface(luid)
	if err != nil {
		return fmt.Errorf("kill switch update: permit tunnel interface: %w", err)
	}
	ks.tunnelFilterId = filterId

	st, _ := loadKillSwitchState()
	st.TunnelInterface = strings.TrimSpace(tunnel.Name)
	_ = saveKillSwitchState(st)

	return nil
}

// resolveTunnelLUID prefers the LUID the caller already holds for the live
// device, falling back to a name lookup only when it has none.
//
// The fallback is what this exists to avoid: after a rebuild
// GetAdaptersAddresses can still return the dying adapter's index, and a permit
// naming an interface that no longer exists leaves block-all-outbound dropping
// every application socket while WireGuard goes on handshaking.
func resolveTunnelLUID(tunnel TunnelRef) (uint64, error) {
	if tunnel.WindowsLUID != 0 {
		return tunnel.WindowsLUID, nil
	}

	name := strings.TrimSpace(tunnel.Name)
	if name == "" {
		return 0, fmt.Errorf("tunnel has neither a LUID nor an interface name")
	}
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return 0, fmt.Errorf("resolve interface %q: %w", name, err)
	}
	luid, err := winipcfg.LUIDFromIndex(uint32(iface.Index))
	if err != nil {
		return 0, fmt.Errorf("LUID for %q: %w", name, err)
	}
	return uint64(luid), nil
}

func (ks *windowsKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	// Dynamic session: closing the engine handle removes all filters + sublayer.
	if ks.engine != nil {
		ks.engine.close()
		ks.engine = nil
	}

	_ = removeKillSwitchState()
	ks.active = false
	ks.endpointFilterIds = nil
	ks.lanFilterIds = nil
	ks.tunnelFilterId = 0
	return nil
}

func (ks *windowsKillSwitch) Active() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.active
}
