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

	iptChainName  = "PANGEAVPN_KS"
	ipt6ChainName = "PANGEAVPN_KS6"
)

var runIPTablesCommand = func(ctx context.Context, binary string, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (%s)", binary, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		return &linuxKillSwitch{}
	}
}

type linuxKillSwitch struct {
	mu     sync.Mutex
	active bool
	useNFT bool // true = nftables, false = iptables
}

func (ks *linuxKillSwitch) Enable(ctx context.Context, endpointHost string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.active {
		return nil
	}

	ips, err := resolveEndpointIPs(ctx, endpointHost)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}

	st := KillSwitchState{
		Active:      true,
		EndpointIPs: ips,
	}
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch enable: save state: %w", err)
	}

	// Try nftables first, fall back to iptables.
	if hasNFT(ctx) {
		ks.useNFT = true
		if err := applyNFTRules(ctx, ips, ""); err != nil {
			_ = removeNFTRules(ctx)
			_ = removeKillSwitchState()
			return fmt.Errorf("kill switch enable (nft): %w", err)
		}
	} else {
		ks.useNFT = false
		if err := applyIPTablesRules(ctx, ips, ""); err != nil {
			_ = removeIPTablesRules(ctx)
			_ = removeKillSwitchState()
			return fmt.Errorf("kill switch enable (iptables): %w", err)
		}
	}

	ks.active = true
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
		if err := applyNFTRules(ctx, st.EndpointIPs, tunnelInterface); err != nil {
			return fmt.Errorf("kill switch update (nft): %w", err)
		}
	} else {
		if err := applyIPTablesRules(ctx, st.EndpointIPs, tunnelInterface); err != nil {
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
func buildNFTRuleset(endpointIPs []string, tunnelInterface string) string {
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

	// Allow IPv4 traffic on tunnel interface.
	if tunnelInterface != "" {
		fmt.Fprintf(&b, "    meta nfproto ipv4 oifname \"%s\" accept\n", tunnelInterface)
	}

	fmt.Fprintf(&b, "  }\n")
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

func applyNFTRules(ctx context.Context, endpointIPs []string, tunnelInterface string) error {
	// Delete existing table first (ignore error if it doesn't exist).
	_ = removeNFTRules(ctx)

	ruleset := buildNFTRuleset(endpointIPs, tunnelInterface)

	cmd := exec.CommandContext(ctx, "nft", "-f", "-")
	cmd.Stdin = strings.NewReader(ruleset)
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

// ---------------------------------------------------------------------------
// iptables fallback backend
// ---------------------------------------------------------------------------

func applyIPTablesRules(ctx context.Context, endpointIPs []string, tunnelInterface string) error {
	// Clean up any previous rules.
	_ = removeIPTablesRules(ctx)

	// Create chain.
	if err := iptCmd(ctx, "-N", iptChainName); err != nil {
		return fmt.Errorf("create chain: %w", err)
	}

	// Allow loopback.
	if err := iptCmd(ctx, "-A", iptChainName, "-o", "lo", "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("allow loopback: %w", err)
	}

	// Allow DHCP.
	if err := iptCmd(ctx, "-A", iptChainName, "-p", "udp", "--sport", "68", "--dport", "67", "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("allow DHCP: %w", err)
	}

	// Allow endpoint IPs.
	for _, ip := range endpointIPs {
		if strings.Contains(ip, ":") {
			continue // Skip IPv6 in iptables (would need ip6tables).
		}
		if err := iptCmd(ctx, "-A", iptChainName, "-d", ip, "-j", "ACCEPT"); err != nil {
			return fmt.Errorf("allow endpoint %s: %w", ip, err)
		}
	}

	// Allow tunnel interface.
	if tunnelInterface != "" {
		if err := iptCmd(ctx, "-A", iptChainName, "-o", tunnelInterface, "-j", "ACCEPT"); err != nil {
			return fmt.Errorf("allow tunnel interface: %w", err)
		}
	}

	// Drop everything else.
	if err := iptCmd(ctx, "-A", iptChainName, "-j", "DROP"); err != nil {
		return fmt.Errorf("add drop rule: %w", err)
	}

	// Insert jump to our chain at the top of OUTPUT.
	if err := iptCmd(ctx, "-I", "OUTPUT", "1", "-j", iptChainName); err != nil {
		return fmt.Errorf("insert jump: %w", err)
	}

	// Enforce outbound IPv6 block when nftables is unavailable.
	if err := ip6tCmd(ctx, "-N", ipt6ChainName); err != nil {
		return fmt.Errorf("create IPv6 chain: %w", err)
	}
	if err := ip6tCmd(ctx, "-A", ipt6ChainName, "-o", "lo", "-j", "ACCEPT"); err != nil {
		return fmt.Errorf("allow IPv6 loopback: %w", err)
	}
	if err := ip6tCmd(ctx, "-A", ipt6ChainName, "-j", "DROP"); err != nil {
		return fmt.Errorf("add IPv6 drop rule: %w", err)
	}
	if err := ip6tCmd(ctx, "-I", "OUTPUT", "1", "-j", ipt6ChainName); err != nil {
		return fmt.Errorf("insert IPv6 jump: %w", err)
	}

	return nil
}

func removeIPTablesRules(ctx context.Context) error {
	// Remove jump from OUTPUT (ignore errors if chain doesn't exist).
	_ = iptCmd(ctx, "-D", "OUTPUT", "-j", iptChainName)
	_ = ip6tCmd(ctx, "-D", "OUTPUT", "-j", ipt6ChainName)

	// Flush and delete our chain.
	_ = iptCmd(ctx, "-F", iptChainName)
	_ = iptCmd(ctx, "-X", iptChainName)
	_ = ip6tCmd(ctx, "-F", ipt6ChainName)
	_ = ip6tCmd(ctx, "-X", ipt6ChainName)

	return nil
}

func iptCmd(ctx context.Context, args ...string) error {
	return runIPTablesCommand(ctx, "iptables", args...)
}

func ip6tCmd(ctx context.Context, args ...string) error {
	return runIPTablesCommand(ctx, "ip6tables", args...)
}
