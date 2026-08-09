//go:build linux

package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

const (
	nftTableName = "pangeavpn_killswitch"
	nftFamily    = "inet"
)

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		return &linuxKillSwitch{}
	}
}

type linuxKillSwitch struct {
	mu       sync.Mutex
	active   bool
	useNFT   bool // true = nftables, false = iptables
	allowLAN bool
}

func (ks *linuxKillSwitch) Enable(ctx context.Context, endpointHosts []string, allowLAN bool, locked bool) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ips, err := resolveEndpointHosts(ctx, endpointHosts)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}

	var tunnelInterface string
	if ks.active {
		prev, _ := loadKillSwitchState()
		if stringSlicesEqual(prev.EndpointIPs, ips) && prev.AllowLAN == allowLAN {
			// Rules match; still record a Lockdown re-arm.
			return persistLockedUpgrade(prev, locked)
		}
		tunnelInterface = prev.TunnelInterface
	}

	if hasNFT(ctx) {
		ks.useNFT = true
		if err := applyNFTRules(ctx, ips, tunnelInterface, allowLAN); err != nil {
			if !ks.active {
				_ = removeNFTRules(ctx)
			}
			return fmt.Errorf("kill switch enable (nft): %w", err)
		}
	} else {
		ks.useNFT = false
		if err := applyIPTablesRules(ctx, ips, tunnelInterface, allowLAN); err != nil {
			if !ks.active {
				_ = removeIPTablesRules(ctx)
			}
			return fmt.Errorf("kill switch enable (iptables): %w", err)
		}
	}

	// Rules are live from here. Marking active before the save means a save
	// failure still leaves them trackable by Clear.
	ks.active = true
	ks.allowLAN = allowLAN

	// Saved only after the rules land: saving first let a failed re-apply leave
	// phantom state that the next attempt's equality check would match.
	st := KillSwitchState{
		Active:          true,
		AllowLAN:        allowLAN,
		EndpointIPs:     ips,
		TunnelInterface: tunnelInterface,
		Locked:          locked,
	}
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch enable: save state: %w", err)
	}
	return nil
}

func (ks *linuxKillSwitch) Update(ctx context.Context, tunnelInterface string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active {
		return fmt.Errorf("kill switch not active")
	}

	tunnelInterface = strings.TrimSpace(tunnelInterface)
	if tunnelInterface == "" {
		return fmt.Errorf("empty tunnel interface name")
	}

	st, _ := loadKillSwitchState()
	st.TunnelInterface = tunnelInterface
	_ = saveKillSwitchState(st)

	if ks.useNFT {
		if err := applyNFTRules(ctx, st.EndpointIPs, tunnelInterface, ks.allowLAN); err != nil {
			return fmt.Errorf("kill switch update (nft): %w", err)
		}
	} else {
		if err := applyIPTablesRules(ctx, st.EndpointIPs, tunnelInterface, ks.allowLAN); err != nil {
			return fmt.Errorf("kill switch update (iptables): %w", err)
		}
	}

	return nil
}

func (ks *linuxKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var errs []string

	if ks.useNFT {
		if err := removeNFTRules(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("remove nft rules: %v", err))
		}
	} else {
		if err := removeIPTablesRules(ctx); err != nil {
			errs = append(errs, fmt.Sprintf("remove iptables rules: %v", err))
		}
	}

	_ = removeKillSwitchState()
	ks.active = false

	if len(errs) > 0 {
		return fmt.Errorf("kill switch clear incomplete: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (ks *linuxKillSwitch) Active() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.active
}

// ---------------------------------------------------------------------------
// nftables backend
// ---------------------------------------------------------------------------

func hasNFT(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "nft", "--version")
	return cmd.Run() == nil
}

// buildNFTRuleset generates a complete nftables ruleset for the kill switch.
func buildNFTRuleset(endpointIPs []string, tunnelInterface string, allowLAN bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "table %s %s {\n", nftFamily, nftTableName)
	fmt.Fprintf(&b, "  chain output {\n")
	fmt.Fprintf(&b, "    type filter hook output priority 0; policy drop;\n")
	fmt.Fprintf(&b, "\n")

	// Allow loopback.
	fmt.Fprintf(&b, "    oifname \"lo\" accept\n")

	// Allow DHCP.
	fmt.Fprintf(&b, "    udp sport 68 udp dport 67 accept\n")

	// Allow traffic to endpoint IPs.
	for _, ip := range endpointIPs {
		if strings.Contains(ip, ":") {
			continue
		}
		fmt.Fprintf(&b, "    ip daddr %s accept\n", ip)
	}

	// Allow LAN ranges so captive portals and gateway probes work on
	// restrictive WiFi. Only applied when the user opts in.
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			fmt.Fprintf(&b, "    ip daddr %s accept\n", cidr)
		}
	}

	// Allow IPv4 traffic on tunnel interface.
	if tunnelInterface != "" {
		fmt.Fprintf(&b, "    meta nfproto ipv4 oifname \"%s\" accept\n", tunnelInterface)
	}

	fmt.Fprintf(&b, "  }\n")
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

func applyNFTRules(ctx context.Context, endpointIPs []string, tunnelInterface string, allowLAN bool) error {
	// Replace rules atomically. nft -f processes the whole script as a single
	// kernel transaction: `add table` is a no-op if it exists, `delete table`
	// drops the old version, and the new `table {...}` block installs the
	// replacement. There is never a moment with no rules in place.
	var b strings.Builder
	fmt.Fprintf(&b, "add table %s %s\n", nftFamily, nftTableName)
	fmt.Fprintf(&b, "delete table %s %s\n", nftFamily, nftTableName)
	b.WriteString(buildNFTRuleset(endpointIPs, tunnelInterface, allowLAN))

	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(b.String())
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("apply nft rules: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func removeNFTRules(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "nft", "delete", "table", nftFamily, nftTableName)
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.ToLower(string(out))
		if strings.Contains(trimmed, "no such") || strings.Contains(trimmed, "does not exist") {
			return nil
		}
		return fmt.Errorf("delete nft table: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
