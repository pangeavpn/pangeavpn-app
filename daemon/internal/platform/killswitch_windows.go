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
}

func (ks *windowsKillSwitch) Enable(ctx context.Context, endpointHosts []string, allowLAN bool, locked bool) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ips, err := resolveEndpointHosts(ctx, endpointHosts)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}

	// Re-entry: stack new endpoint permits in one WFP transaction. Old permits
	// linger harmlessly until Disconnect closes the dynamic session.
	if ks.active && ks.engine != nil {
		prev, _ := loadKillSwitchState()
		if stringSlicesEqual(prev.EndpointIPs, ips) && prev.AllowLAN == allowLAN {
			return nil
		}

		if err := ks.engine.beginTransaction(); err != nil {
			return fmt.Errorf("kill switch re-enable: %w", err)
		}
		for _, ip := range ips {
			if _, err := ks.engine.addPermitEndpointIP(ip); err != nil {
				ks.engine.abortTransaction()
				return fmt.Errorf("kill switch re-enable: permit %s: %w", ip, err)
			}
		}
		// LAN permits can only be added on re-entry, not removed (no filter IDs tracked).
		if allowLAN && !prev.AllowLAN {
			for _, cidr := range LANAllowPrefixes {
				if _, err := ks.engine.addPermitIPv4Subnet(cidr); err != nil {
					ks.engine.abortTransaction()
					return fmt.Errorf("kill switch re-enable: permit LAN %s: %w", cidr, err)
				}
			}
		}
		if err := ks.engine.commitTransaction(); err != nil {
			return fmt.Errorf("kill switch re-enable: %w", err)
		}

		prev.EndpointIPs = mergeStringSets(prev.EndpointIPs, ips)
		prev.AllowLAN = allowLAN || prev.AllowLAN
		prev.Locked = prev.Locked || locked
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

	for _, ip := range ips {
		if _, err := engine.addPermitEndpointIP(ip); err != nil {
			engine.abortTransaction()
			engine.close()
			_ = removeKillSwitchState()
			return fmt.Errorf("kill switch enable: %w", err)
		}
	}

	// DHCP best-effort — ALE layer may not see broadcast DHCP traffic.
	_, _ = engine.addPermitDHCP()

	// Permit LAN ranges so captive portals / gateway probes / mDNS work on
	// restrictive WiFi. Only applied when the user opts in — fail-open would
	// defeat the kill switch.
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			if _, err := engine.addPermitIPv4Subnet(cidr); err != nil {
				engine.abortTransaction()
				engine.close()
				_ = removeKillSwitchState()
				return fmt.Errorf("kill switch enable: permit LAN %s: %w", cidr, err)
			}
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

	if err := engine.commitTransaction(); err != nil {
		engine.close()
		_ = removeKillSwitchState()
		return fmt.Errorf("kill switch enable: %w", err)
	}

	ks.engine = engine
	ks.active = true
	return nil
}

func (ks *windowsKillSwitch) Update(ctx context.Context, tunnelInterface string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active || ks.engine == nil {
		return fmt.Errorf("kill switch not active")
	}

	tunnelInterface = strings.TrimSpace(tunnelInterface)
	if tunnelInterface == "" {
		return fmt.Errorf("empty tunnel interface name")
	}

	// Resolve the tunnel adapter's LUID so we can permit traffic through it.
	iface, err := net.InterfaceByName(tunnelInterface)
	if err != nil {
		return fmt.Errorf("kill switch update: resolve interface %q: %w", tunnelInterface, err)
	}
	luid, err := winipcfg.LUIDFromIndex(uint32(iface.Index))
	if err != nil {
		return fmt.Errorf("kill switch update: LUID for %q: %w", tunnelInterface, err)
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
	filterId, err := ks.engine.addPermitTunnelInterface(uint64(luid))
	if err != nil {
		return fmt.Errorf("kill switch update: permit tunnel interface: %w", err)
	}
	ks.tunnelFilterId = filterId

	st, _ := loadKillSwitchState()
	st.TunnelInterface = tunnelInterface
	_ = saveKillSwitchState(st)

	return nil
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
	return nil
}

func (ks *windowsKillSwitch) Active() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.active
}
