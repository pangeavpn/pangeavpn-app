//go:build windows

package platform

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"

	"golang.org/x/sys/windows"
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
	engine         *wfpEngine // static session — block filters are persistent and outlive this handle
	tunnelFilterId uint64     // WFP filter ID for the tunnel interface permit

	// Per-arm permit filter IDs so a re-arm can retire the previous set instead
	// of stacking. Without this every node visited stays permitted until Clear.
	endpointFilterIds []uint64
	lanFilterIds      []uint64

	// What endpointFilterIds/lanFilterIds actually enforce. The re-arm fast
	// path compares against these, not disk, which can be missing/corrupt.
	lastEndpointIPs []string
	lastAllowLAN    bool

	// Tunnel permit IDs a previous Update failed to delete; Clear retries them
	// so a reassigned LUID can't inherit a stale permit.
	staleTunnelFilterIds []uint64
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
		prev, err := loadKillSwitchState()
		if err != nil {
			KillSwitchWarn("kill switch re-arm: state reload failed, reconstructing from live filters: %v", err)
		}

		// Against live engine state, not disk: a corrupt/missing state file
		// must never read as "filters already match" unverified.
		if stringSlicesEqual(ks.lastEndpointIPs, ips) && ks.lastAllowLAN == allowLAN {
			// Filters already match, but a caller re-arming an existing lock as a
			// Lockdown lock still has to be recorded — see persistLockedUpgrade.
			prev.Active = true
			prev.EndpointIPs = ips
			prev.AllowLAN = allowLAN
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
		ks.lastEndpointIPs = ips
		ks.lastAllowLAN = allowLAN
		if len(staleDeletes) > 0 {
			// Lock is intact, but a departing server's permit survives until Clear.
			KillSwitchWarn("kill switch re-arm could not retire stale permits (%s)", strings.Join(staleDeletes, "; "))
		}

		prev.Active = true // the load above may have failed; rules are live
		prev.EndpointIPs = ips
		prev.AllowLAN = allowLAN
		// Raise-only, same as persistLockedUpgrade: a plain reconnect (locked
		// false) must never clear a Lockdown marker a crash could still need.
		prev.Locked = prev.Locked || locked
		if err := saveKillSwitchState(prev); err != nil {
			KillSwitchWarn("kill switch re-arm: save state failed: %v", err)
		}
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

	// Symmetric with the IPv6 blocks below — otherwise inbound on the
	// physical NIC is still accepted while "locked".
	if _, err := engine.addBlockAllInbound(); err != nil {
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
	if _, err := engine.addPermitLoopbackInboundV4(); err != nil {
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
	if _, err := engine.addPermitLoopbackSubnetInboundV4(); err != nil {
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

	// DHCP best-effort, only when the user opted into LAN access, scoped to
	// the ranges already trusted for "Allow LAN" — see addPermitDHCP.
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			if cidr == "224.0.0.0/4" {
				continue // multicast — not a DHCP server/relay address
			}
			_, _ = engine.addPermitDHCP(cidr)
		}
	}

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
	if _, err := engine.addPermitLoopbackSubnetV6(fwpmLayerAleAuthConnectV6, pangeaPermitLoopbackNetV6FilterKey, "PangeaVPN Allow Loopback Subnet IPv6"); err != nil {
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
	if _, err := engine.addPermitLoopbackSubnetV6(fwpmLayerAleAuthRecvAcceptV6, pangeaPermitLoopbackNetInV6FilterKey, "PangeaVPN Allow Loopback Subnet Inbound IPv6"); err != nil {
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
	ks.lastEndpointIPs = ips
	ks.lastAllowLAN = allowLAN
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

	// Retire the previous tunnel permit (e.g. reconnect). Keep the ID on a
	// failed delete: a reassigned LUID could inherit an orphaned permit.
	if ks.tunnelFilterId != 0 {
		if err := ks.engine.deleteFilter(ks.tunnelFilterId); err != nil {
			KillSwitchWarn("kill switch update could not retire previous tunnel permit %d: %v", ks.tunnelFilterId, err)
			ks.staleTunnelFilterIds = append(ks.staleTunnelFilterIds, ks.tunnelFilterId)
		}
		ks.tunnelFilterId = 0
	}
	ks.retireStaleTunnelFilters()

	// App traffic hits the WFP ALE_AUTH_CONNECT layer before reaching the TUN
	// adapter. Without a permit scoped to the tunnel interface LUID, the
	// block-all-outbound rule drops every packet at socket level — explaining
	// "general failure" on ping even though the WireGuard handshake succeeds.
	filterId, err := ks.engine.addPermitTunnelInterface(luid)
	if err != nil {
		return fmt.Errorf("kill switch update: permit tunnel interface: %w", err)
	}
	ks.tunnelFilterId = filterId

	// A failed reload must not clobber Active/Locked with the zero value, so
	// skip the save rather than risk reporting a live lock as cleared.
	if _, err := updateTunnelInterfaceState(strings.TrimSpace(tunnel.Name)); err != nil {
		KillSwitchWarn("kill switch update: state reload failed, leaving persisted state untouched: %v", err)
	}

	return nil
}

// retireStaleTunnelFilters retries deleting tunnel permits that a previous
// call could not remove. Called with ks.mu held.
func (ks *windowsKillSwitch) retireStaleTunnelFilters() {
	remaining := ks.staleTunnelFilterIds[:0]
	for _, id := range ks.staleTunnelFilterIds {
		if err := ks.engine.deleteFilter(id); err != nil {
			remaining = append(remaining, id)
		}
	}
	ks.staleTunnelFilterIds = remaining
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

// pangeaPersistentFilterKeys are the well-known keys of every filter that
// outlives the process. Blocks come first so no window drops loopback.
var pangeaPersistentFilterKeys = []windows.GUID{
	pangeaBlockAllOutboundV4FilterKey,
	pangeaBlockAllInboundV4FilterKey,
	pangeaBlockAllOutboundV6FilterKey,
	pangeaBlockAllInboundV6FilterKey,
	pangeaPermitLoopbackV4FilterKey,
	pangeaPermitLoopbackInboundV4FilterKey,
	pangeaPermitLoopbackNetV4FilterKey,
	pangeaPermitLoopbackNetInV4FilterKey,
	pangeaPermitLoopbackV6FilterKey,
	pangeaPermitLoopbackInboundV6FilterKey,
	pangeaPermitLoopbackNetV6FilterKey,
	pangeaPermitLoopbackNetInV6FilterKey,
}

func (ks *windowsKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var errs []string

	// A restarted daemon inherits no handle to the filters the previous
	// process left live, so open a fresh engine rather than skip the teardown.
	engine, ownsEngine := ks.engine, false
	if engine == nil {
		opened, err := wfpOpen()
		if err != nil {
			return fmt.Errorf("kill switch clear: %w", err)
		}
		engine, ownsEngine = opened, true
	}

	for _, id := range ks.endpointFilterIds {
		if err := engine.deleteFilter(id); err != nil {
			errs = append(errs, fmt.Sprintf("endpoint permit %d: %v", id, err))
		}
	}
	for _, id := range ks.lanFilterIds {
		if err := engine.deleteFilter(id); err != nil {
			errs = append(errs, fmt.Sprintf("lan permit %d: %v", id, err))
		}
	}
	if ks.tunnelFilterId != 0 {
		if err := engine.deleteFilter(ks.tunnelFilterId); err != nil {
			errs = append(errs, fmt.Sprintf("tunnel permit %d: %v", ks.tunnelFilterId, err))
		}
	}
	for _, id := range ks.staleTunnelFilterIds {
		if err := engine.deleteFilter(id); err != nil {
			errs = append(errs, fmt.Sprintf("stale tunnel permit %d: %v", id, err))
		}
	}
	for _, key := range pangeaPersistentFilterKeys {
		if err := engine.deleteFilterByKey(key); err != nil {
			errs = append(errs, fmt.Sprintf("filter %v: %v", key, err))
		}
	}
	if err := engine.deleteSublayerByKey(pangeaVPNSublayerKey); err != nil {
		errs = append(errs, fmt.Sprintf("sublayer: %v", err))
	}

	if err := engine.close(); err != nil {
		errs = append(errs, fmt.Sprintf("engine close: %v", err))
	} else if !ownsEngine {
		ks.engine = nil
	}

	// A partial teardown must not report the lock as cleared: leave Active
	// alone so a retry, or reconciliation after a crash here, still locks.
	if len(errs) > 0 {
		return fmt.Errorf("kill switch clear: %s", strings.Join(errs, "; "))
	}

	ks.active = false
	ks.endpointFilterIds = nil
	ks.lanFilterIds = nil
	ks.lastEndpointIPs = nil
	ks.lastAllowLAN = false
	ks.tunnelFilterId = 0
	ks.staleTunnelFilterIds = nil

	if err := removeKillSwitchState(); err != nil {
		return fmt.Errorf("kill switch clear: remove state: %w", err)
	}
	return nil
}

func (ks *windowsKillSwitch) Active() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.active
}
