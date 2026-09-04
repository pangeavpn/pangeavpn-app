//go:build windows && (amd64 || arm64)

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
	probe          *wfpEngine // read-only handle Active() asks about the persistent block
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

	// Forward-layer permits mirroring the ALE ones: the tunnel by interface
	// index, the LAN ranges under Allow LAN.
	forwardTunnelFilterId uint64
	forwardLANFilterIds   []uint64
	// forwardLock is cleared when a forward permit fails and the forward blocks
	// are dropped again, so guests are never worse off than with no forward filtering.
	forwardLock bool
	// bootTimeFailed stops retrying the best-effort boot-time twins every arm.
	bootTimeFailed bool
}

func (ks *windowsKillSwitch) Enable(ctx context.Context, endpointHosts []string, allowLAN bool, locked bool) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ips, err := resolveEndpointHosts(ctx, endpointHosts)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}
	ips = v4Permits(ips)

	// A re-arm trusts its handle only while the persistent block is still
	// installed; otherwise the lock is rebuilt from scratch below.
	if ks.active && ks.engine != nil {
		if live, err := ks.engine.filterExistsByKey(pangeaBlockAllOutboundV4FilterKey); err != nil || !live {
			KillSwitchWarn("kill switch re-arm: persistent block missing (live=%v, err=%v); rebuilding the lock", live, err)
			_ = ks.engine.close()
			ks.engine = nil
			ks.active = false
		}
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
		ks.swapForwardLANPermits(allowLAN)

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
	endpointIds, lanIds, err := installWindowsLock(engine, ips, allowLAN)
	if err != nil {
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
	// The sweep inside installWindowsLock retired any tunnel permit too.
	ks.tunnelFilterId = 0
	ks.staleTunnelFilterIds = nil
	ks.forwardTunnelFilterId = 0
	ks.forwardLANFilterIds = nil
	// The forward blocks are in the persistent set just installed; the permits
	// that keep guests usable are best-effort on top of them.
	ks.forwardLock = true
	ks.swapForwardLANPermits(allowLAN)
	ks.installBootTimeLock()
	return nil
}

// installWindowsLock builds the whole lock in one transaction: stale permits
// out, the persistent set replaced by this build's, this arm's permits in.
func installWindowsLock(engine *wfpEngine, ips []string, allowLAN bool) (endpointIds, lanIds []uint64, err error) {
	if err := engine.beginTransaction(); err != nil {
		return nil, nil, err
	}
	fail := func(err error) ([]uint64, []uint64, error) {
		engine.abortTransaction()
		return nil, nil, err
	}

	if err := engine.addSublayer(); err != nil {
		return fail(err)
	}
	// A restarted daemon inherits the engine-keyed permits of the process
	// before it (old nodes, old tunnel LUID); retire them in this transaction.
	if _, err := engine.deleteFiltersInSublayer(pangeaVPNSublayerKey, isEphemeralFilter); err != nil {
		return fail(fmt.Errorf("sweep stale permits: %w", err))
	}
	// Delete-then-add under the same keys, so an older build's weights and
	// filter set are replaced without the lock ever being down.
	for _, key := range pangeaPersistentFilterKeys {
		if err := engine.deleteFilterByKey(key); err != nil {
			return fail(fmt.Errorf("retire persistent filter %v: %w", key, err))
		}
	}
	for _, step := range persistentLockFilters {
		if _, err := step.add(engine); err != nil {
			return fail(fmt.Errorf("%s: %w", step.name, err))
		}
	}

	endpointIds = make([]uint64, 0, len(ips))
	for _, ip := range ips {
		id, err := engine.addPermitEndpointIP(ip)
		if err != nil {
			return fail(fmt.Errorf("permit %s: %w", ip, err))
		}
		endpointIds = append(endpointIds, id)
	}

	// Unicast renewals to the server itself and the LAN ranges themselves,
	// only when the user opted into LAN access.
	lanIds = make([]uint64, 0, len(LANAllowPrefixes))
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			if cidr == "224.0.0.0/4" {
				continue // multicast — not a DHCP server/relay address
			}
			if _, err := engine.addPermitDHCP(cidr); err != nil {
				return fail(fmt.Errorf("permit DHCP renewal %s: %w", cidr, err))
			}
		}
		for _, cidr := range LANAllowPrefixes {
			id, err := engine.addPermitIPv4Subnet(cidr)
			if err != nil {
				return fail(fmt.Errorf("permit LAN %s: %w", cidr, err))
			}
			lanIds = append(lanIds, id)
		}
	}

	if err := engine.commitTransaction(); err != nil {
		engine.abortTransaction()
		return nil, nil, err
	}
	return endpointIds, lanIds, nil
}

// persistentLockFilters is the lock that outlives the process, in install
// order. Blocks first, then the permits that keep the host usable while locked.
var persistentLockFilters = []struct {
	name string
	add  func(*wfpEngine) (uint64, error)
}{
	{"block outbound v4", (*wfpEngine).addBlockAllOutbound},
	{"block inbound v4", (*wfpEngine).addBlockAllInbound},
	{"block outbound v6", (*wfpEngine).addBlockAllOutboundV6},
	{"block inbound v6", (*wfpEngine).addBlockAllInboundV6},
	{"block forward v4", (*wfpEngine).addBlockAllForwardV4},
	{"block forward v6", (*wfpEngine).addBlockAllForwardV6},
	{"block DNS udp", (*wfpEngine).addBlockDNSUDP},
	{"block DNS tcp", (*wfpEngine).addBlockDNSTCP},
	{"block DoT udp", (*wfpEngine).addBlockDoTUDP},
	{"block DoT tcp", (*wfpEngine).addBlockDoTTCP},
	{"permit loopback v4", (*wfpEngine).addPermitLoopback},
	{"permit loopback inbound v4", (*wfpEngine).addPermitLoopbackInboundV4},
	{"permit loopback subnet v4", (*wfpEngine).addPermitLoopbackSubnet},
	{"permit loopback subnet inbound v4", (*wfpEngine).addPermitLoopbackSubnetInboundV4},
	{"permit loopback v6", (*wfpEngine).addPermitLoopbackV6},
	{"permit loopback inbound v6", (*wfpEngine).addPermitLoopbackInboundV6},
	{"permit loopback subnet v6", func(e *wfpEngine) (uint64, error) {
		return e.addPermitLoopbackSubnetV6(cFWPM_LAYER_ALE_AUTH_CONNECT_V6, pangeaPermitLoopbackNetV6FilterKey, "PangeaVPN Allow Loopback Subnet IPv6")
	}},
	{"permit loopback subnet inbound v6", func(e *wfpEngine) (uint64, error) {
		return e.addPermitLoopbackSubnetV6(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6, pangeaPermitLoopbackNetInV6FilterKey, "PangeaVPN Allow Loopback Subnet Inbound IPv6")
	}},
	{"permit DHCP broadcast", (*wfpEngine).addPermitDHCPBroadcast},
	{"permit DHCP reply", (*wfpEngine).addPermitDHCPInbound},
}

// v4Permits drops addresses the IPv4-only permit builders cannot express. IPv6
// stays blocked outright, so a transport with a v6 endpoint falls back to v4.
func v4Permits(ips []string) []string {
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			out = append(out, ip)
			continue
		}
		KillSwitchWarn("kill switch: skipping non-IPv4 permit %s (IPv6 is blocked outright)", ip)
	}
	return out
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
	if ks.forwardTunnelFilterId != 0 {
		if err := ks.engine.deleteFilter(ks.forwardTunnelFilterId); err != nil {
			ks.staleTunnelFilterIds = append(ks.staleTunnelFilterIds, ks.forwardTunnelFilterId)
		}
		ks.forwardTunnelFilterId = 0
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
	ks.permitForwardingToTunnel(luid)

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
	pangeaBlockForwardV4FilterKey,
	pangeaBlockForwardV6FilterKey,
	pangeaBlockDNSUDPV4FilterKey,
	pangeaBlockDNSTCPV4FilterKey,
	pangeaBlockDoTUDPV4FilterKey,
	pangeaBlockDoTTCPV4FilterKey,
	pangeaPermitDHCPOutV4FilterKey,
	pangeaPermitDHCPInV4FilterKey,
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

	// Sweep the whole sublayer first. Endpoint, LAN and tunnel permits carry
	// engine-assigned keys, so a daemon that died without clearing left ones
	// only enumeration can still name — and the sublayer delete below fails
	// while any of them survive, which would strand the machine blocked.
	if _, err := engine.deleteFiltersInSublayer(pangeaVPNSublayerKey, anyFilter); err != nil {
		errs = append(errs, fmt.Sprintf("sublayer sweep: %v", err))
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
	for _, id := range append(ks.forwardLANFilterIds, ks.forwardTunnelFilterId) {
		if id == 0 {
			continue
		}
		if err := engine.deleteFilter(id); err != nil {
			errs = append(errs, fmt.Sprintf("forward permit %d: %v", id, err))
		}
	}
	for _, key := range pangeaPersistentFilterKeys {
		if err := engine.deleteFilterByKey(key); err != nil {
			errs = append(errs, fmt.Sprintf("filter %v: %v", key, err))
		}
		// Boot-time twins only ever act before BFE is up, so a leftover costs a
		// few seconds at the next boot; it must not strand a user's Disconnect.
		bootKey, _ := bootTimeVariant(key, 0)
		if err := engine.deleteFilterByKey(bootKey); err != nil {
			KillSwitchWarn("kill switch clear: boot-time filter %v: %v", bootKey, err)
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
	if ks.probe != nil {
		_ = ks.probe.close()
		ks.probe = nil
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
	ks.forwardTunnelFilterId = 0
	ks.forwardLANFilterIds = nil
	ks.forwardLock = false

	if err := removeKillSwitchState(); err != nil {
		return fmt.Errorf("kill switch clear: remove state: %w", err)
	}
	return nil
}

func (ks *windowsKillSwitch) Active() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.active {
		return true
	}
	return ks.persistentLockLive()
}

// persistentLockLive asks BFE whether the block is installed: a lock left by a
// previous process counts even though this one never armed it. Holds ks.mu.
func (ks *windowsKillSwitch) persistentLockLive() bool {
	if ks.probe == nil {
		engine, err := wfpOpen()
		if err != nil {
			return false
		}
		ks.probe = engine
	}
	live, err := ks.probe.filterExistsByKey(pangeaBlockAllOutboundV4FilterKey)
	if err != nil {
		_ = ks.probe.close()
		ks.probe = nil
		return false
	}
	return live
}

// permitForwardingToTunnel lets guest traffic the host forwards leave through
// the tunnel. On failure the forward blocks come down again (dropForwardLock).
func (ks *windowsKillSwitch) permitForwardingToTunnel(luid uint64) {
	if !ks.forwardLock {
		return
	}
	row, err := winipcfg.LUID(luid).Interface()
	if err != nil {
		ks.dropForwardLock(fmt.Errorf("interface index for LUID %d: %w", luid, err))
		return
	}
	id, err := ks.engine.addPermitForwardToInterface(row.InterfaceIndex)
	if err != nil {
		ks.dropForwardLock(fmt.Errorf("permit forwarding to tunnel: %w", err))
		return
	}
	ks.forwardTunnelFilterId = id
}

// swapForwardLANPermits mirrors the ALE LAN permits at the forward layer in a
// transaction of their own, so a failure there cannot take the main lock down.
func (ks *windowsKillSwitch) swapForwardLANPermits(allowLAN bool) {
	if !ks.forwardLock {
		return
	}
	if err := ks.engine.beginTransaction(); err != nil {
		ks.dropForwardLock(err)
		return
	}
	ids := make([]uint64, 0, len(LANAllowPrefixes))
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			id, err := ks.engine.addPermitForwardIPv4Subnet(cidr)
			if err != nil {
				ks.engine.abortTransaction()
				ks.dropForwardLock(fmt.Errorf("permit forwarding to LAN %s: %w", cidr, err))
				return
			}
			ids = append(ids, id)
		}
	}
	for _, id := range ks.forwardLANFilterIds {
		if err := ks.engine.deleteFilter(id); err != nil {
			KillSwitchWarn("kill switch could not retire forward LAN permit %d: %v", id, err)
		}
	}
	if err := ks.engine.commitTransaction(); err != nil {
		ks.engine.abortTransaction()
		ks.dropForwardLock(err)
		return
	}
	ks.forwardLANFilterIds = ids
}

// dropForwardLock returns the forward layers to unfiltered when a permit cannot
// be installed: a block with no way through would cut every guest off.
func (ks *windowsKillSwitch) dropForwardLock(cause error) {
	KillSwitchWarn("kill switch: forwarded-traffic lock disabled on this host (%v); WSL2/Hyper-V guests are not covered", cause)
	ks.forwardLock = false
	for _, key := range []windows.GUID{pangeaBlockForwardV4FilterKey, pangeaBlockForwardV6FilterKey} {
		if err := ks.engine.deleteFilterByKey(key); err != nil {
			KillSwitchWarn("kill switch: could not remove forward block %v: %v", key, err)
		}
	}
	for _, id := range append(ks.forwardLANFilterIds, ks.forwardTunnelFilterId) {
		if id != 0 {
			_ = ks.engine.deleteFilter(id)
		}
	}
	ks.forwardLANFilterIds = nil
	ks.forwardTunnelFilterId = 0
}

// installBootTimeLock adds boot-time twins of the persistent set so the lock
// also holds between stack start and BFE start. Best-effort, warned once.
func (ks *windowsKillSwitch) installBootTimeLock() {
	if ks.bootTimeFailed {
		return
	}
	if err := installBootTimeTwins(ks.engine, ks.forwardLock); err != nil {
		ks.bootTimeFailed = true
		KillSwitchWarn("kill switch: boot-time filters not installed (%v); the lock starts when BFE does", err)
	}
}

// installBootTimeTwins replaces the previous build's boot-time set by key in
// one transaction, the same way installWindowsLock treats the persistent set.
func installBootTimeTwins(engine *wfpEngine, includeForward bool) error {
	if err := engine.beginTransaction(); err != nil {
		return err
	}
	for _, key := range pangeaPersistentFilterKeys {
		bootKey, _ := bootTimeVariant(key, 0)
		if err := engine.deleteFilterByKey(bootKey); err != nil {
			engine.abortTransaction()
			return fmt.Errorf("retire boot-time filter %v: %w", bootKey, err)
		}
	}
	view := engine.bootTimeView()
	for _, step := range persistentLockFilters {
		if !includeForward && strings.HasPrefix(step.name, "block forward") {
			continue
		}
		if _, err := step.add(view); err != nil {
			engine.abortTransaction()
			return fmt.Errorf("%s (boot-time): %w", step.name, err)
		}
	}
	if err := engine.commitTransaction(); err != nil {
		engine.abortTransaction()
		return err
	}
	return nil
}

// DropTunnelPermit retires the tunnel permits while the lock stays armed, so a
// reassigned LUID cannot inherit them from a session that is over.
func (ks *windowsKillSwitch) DropTunnelPermit(_ context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active || ks.engine == nil {
		return nil
	}
	for _, id := range []uint64{ks.tunnelFilterId, ks.forwardTunnelFilterId} {
		if id == 0 {
			continue
		}
		if err := ks.engine.deleteFilter(id); err != nil {
			ks.staleTunnelFilterIds = append(ks.staleTunnelFilterIds, id)
		}
	}
	ks.tunnelFilterId, ks.forwardTunnelFilterId = 0, 0
	ks.retireStaleTunnelFilters()
	if len(ks.staleTunnelFilterIds) > 0 {
		return fmt.Errorf("kill switch drop tunnel: %d permits could not be retired", len(ks.staleTunnelFilterIds))
	}
	if _, err := updateTunnelInterfaceState(""); err != nil {
		KillSwitchWarn("kill switch drop tunnel: state reload failed, leaving persisted state untouched: %v", err)
	}
	return nil
}
