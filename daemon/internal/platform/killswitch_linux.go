//go:build linux

package platform

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	nftTableName = "pangeavpn_killswitch"
	nftFamily    = "inet"
)

// Matches iptables/nft interface name constraints; rejected names are never
// interpolated into the nft script text.
var validTunnelInterfaceName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`).MatchString

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

	// Cached kernel probe under its own lock, so a 1Hz status poll neither
	// forks nft each time nor holds up Enable/Clear behind a slow probe.
	probeMu sync.Mutex
	liveAt  time.Time
	live    bool
}

const (
	liveProbeTTL     = 1500 * time.Millisecond
	liveProbeTimeout = 3 * time.Second
)

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
		tunnelInterface = prev.TunnelInterface
		if prev.Locked {
			// Never let a re-arm silently drop a previously recorded Lockdown.
			locked = true
		}
	}

	// Re-apply unconditionally rather than trusting in-memory state: an
	// external actor (firewalld reload, manual flush) can remove the live
	// rules without this process knowing. Both apply paths are idempotent.
	useNFT := false
	if hasNFT(ctx) {
		if err := applyNFTRules(ctx, ips, tunnelInterface, allowLAN); err == nil {
			useNFT = true
		} else if !ks.active {
			_ = removeNFTRules(ctx)
		}
	}
	if !useNFT {
		if err := applyIPTablesRules(ctx, ips, tunnelInterface, allowLAN); err != nil {
			if !ks.active {
				_ = removeIPTablesRules(ctx)
			}
			return fmt.Errorf("kill switch enable (iptables): %w", err)
		}
	}
	ks.useNFT = useNFT

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

func (ks *linuxKillSwitch) Update(ctx context.Context, tunnel TunnelRef) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active {
		return fmt.Errorf("kill switch not active")
	}

	tunnelInterface := strings.TrimSpace(tunnel.Name)
	if tunnelInterface == "" {
		return fmt.Errorf("empty tunnel interface name")
	}

	st, err := loadKillSwitchState()
	if err != nil {
		return fmt.Errorf("kill switch update: load state: %w", err)
	}

	if ks.useNFT {
		if err := applyNFTRules(ctx, st.EndpointIPs, tunnelInterface, ks.allowLAN); err != nil {
			return fmt.Errorf("kill switch update (nft): %w", err)
		}
	} else {
		if err := applyIPTablesRules(ctx, st.EndpointIPs, tunnelInterface, ks.allowLAN); err != nil {
			return fmt.Errorf("kill switch update (iptables): %w", err)
		}
	}

	// Persisted only after the rules land, so state never claims a permit
	// that a failed apply never installed.
	st.TunnelInterface = tunnelInterface
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch update: save state: %w", err)
	}
	return nil
}

func (ks *linuxKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	// Tear down both backends unconditionally: which one is actually live
	// cannot be trusted after a crash/restart (useNFT is in-memory only,
	// never persisted), and each removal is a no-op when its backend is absent.
	var errs []string
	if err := removeNFTRules(ctx); err != nil {
		errs = append(errs, fmt.Sprintf("remove nft rules: %v", err))
	}
	if err := removeIPTablesRules(ctx); err != nil {
		errs = append(errs, fmt.Sprintf("remove iptables rules: %v", err))
	}
	if len(errs) > 0 {
		// Leave active + persisted state untouched so a half-removed chain
		// keeps Active() true and gets retried instead of stranding silently.
		return fmt.Errorf("kill switch clear incomplete: %s", strings.Join(errs, "; "))
	}

	ks.active = false
	if err := removeKillSwitchState(); err != nil {
		return fmt.Errorf("kill switch clear: remove state: %w", err)
	}
	return nil
}

func (ks *linuxKillSwitch) Active() bool {
	ks.mu.Lock()
	active := ks.active
	ks.mu.Unlock()
	if active {
		return true
	}
	return ks.lockLive()
}

// lockLive asks the kernel whether either backend still holds the lock: one
// left by a previous process counts even though this one never armed it.
func (ks *linuxKillSwitch) lockLive() bool {
	ks.probeMu.Lock()
	defer ks.probeMu.Unlock()
	if time.Since(ks.liveAt) < liveProbeTTL {
		return ks.live
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveProbeTimeout)
	defer cancel()
	ks.live = nftTableLive(ctx) || iptablesLockLive(ctx)
	ks.liveAt = time.Now()
	return ks.live
}

func nftTableLive(ctx context.Context) bool {
	return exec.CommandContext(ctx, "nft", "list", "table", nftFamily, nftTableName).Run() == nil
}

func iptablesLockLive(ctx context.Context) bool {
	chain, ok := liveIPTablesChain(ctx, "iptables", iptChainName, iptChainNameAlt)
	return ok && chain != ""
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

	// Allow DHCP, scoped to broadcast so it can't be used to reach an
	// arbitrary remote host on udp/67, and to IPv4 only.
	fmt.Fprintf(&b, "    meta nfproto ipv4 udp sport 68 udp dport 67 ip daddr 255.255.255.255 accept\n")

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
	if tunnelInterface != "" && !validTunnelInterfaceName(tunnelInterface) {
		return fmt.Errorf("invalid tunnel interface name %q", tunnelInterface)
	}

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
