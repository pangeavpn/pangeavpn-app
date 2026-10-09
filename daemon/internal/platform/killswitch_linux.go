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

// Matches iptables/nft interface name constraints; rejected names are never
// interpolated into the nft script text.
var validTunnelInterfaceName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`).MatchString

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		return &linuxKillSwitch{}
	}
}

var _ SplitTunnelPermitter = (*linuxKillSwitch)(nil)

// Seams so lifecycle tests never run nft or iptables.
var (
	nftAvailable = hasNFT
	nftApply     = applyNFTRules
	nftRemove    = removeNFTRules
	iptApply     = applyIPTablesRules
	iptRemove    = removeIPTablesRules
)

type linuxKillSwitch struct {
	mu       sync.Mutex
	active   bool
	useNFT   bool // true = nftables, false = iptables
	allowLAN bool
	// split is what every render carries for split tunnelling; never persisted.
	split splitPermits

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
	var prev KillSwitchState
	if ks.active {
		prev, _ = loadKillSwitchState()
		tunnelInterface = prev.TunnelInterface
		if prev.Locked {
			// Never let a re-arm silently drop a previously recorded Lockdown.
			locked = true
		}
	}

	// Re-apply unconditionally rather than trusting in-memory state: an
	// external actor can remove the live rules without this process knowing.
	rules := ksRules{EndpointIPs: ips, Tunnel: tunnelInterface, AllowLAN: allowLAN, Split: ks.split}
	useNFT := false
	if nftAvailable(ctx) {
		if err := nftApply(ctx, rules); err == nil {
			useNFT = true
		} else if !ks.active {
			_ = nftRemove(ctx)
		}
	}
	if !useNFT {
		if err := iptApply(ctx, rules); err != nil {
			if !ks.active {
				_ = iptRemove(ctx)
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
	st := prev
	st.Active = true
	st.AllowLAN = allowLAN
	st.EndpointIPs = ips
	st.TunnelInterface = tunnelInterface
	st.Locked = locked
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

	if err := ks.render(ctx, ksRules{EndpointIPs: st.EndpointIPs, Tunnel: tunnelInterface, AllowLAN: ks.allowLAN, Split: ks.split}); err != nil {
		return fmt.Errorf("kill switch update %w", err)
	}

	// Persisted only after the rules land, so state never claims a permit
	// that a failed apply never installed.
	st.TunnelInterface = tunnelInterface
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch update: save state: %w", err)
	}
	return nil
}

// render re-applies the live backend's rules. ks.mu held.
func (ks *linuxKillSwitch) render(ctx context.Context, r ksRules) error {
	if ks.useNFT {
		if err := nftApply(ctx, r); err != nil {
			return fmt.Errorf("(nft): %w", err)
		}
		return nil
	}
	if err := iptApply(ctx, r); err != nil {
		return fmt.Errorf("(iptables): %w", err)
	}
	return nil
}

func (ks *linuxKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	// Dropped first: a re-arm after a failed Clear must not bring the permits back.
	ks.split = splitPermits{}

	// Tear down both backends unconditionally: which one is live cannot be
	// trusted after a restart (useNFT is in-memory only, never persisted).
	var errs []string
	if err := nftRemove(ctx); err != nil {
		errs = append(errs, fmt.Sprintf("remove nft rules: %v", err))
	}
	if err := iptRemove(ctx); err != nil {
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

// SetSplitEgress lets the daemon's marked bypass sockets out of the lock.
func (ks *linuxKillSwitch) SetSplitEgress(ctx context.Context, on bool) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	next := ks.split.clone()
	next.Egress = on
	return ks.setSplit(ctx, next)
}

// SetSplitCIDRs permits the excluded destination ranges, resolvers excepted.
func (ks *linuxKillSwitch) SetSplitCIDRs(ctx context.Context, cidrs []string) error {
	normalized, err := normalizeSplitCIDRs(cidrs)
	if err != nil {
		return fmt.Errorf("kill switch split permits: %w", err)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	next := ks.split.clone()
	next.CIDRs = normalized
	return ks.setSplit(ctx, next)
}

// setSplit re-renders an armed lock with next; an idle switch only records it for
// the next Enable. Always renders, so a retry repairs an earlier failed narrowing.
func (ks *linuxKillSwitch) setSplit(ctx context.Context, next splitPermits) error {
	if !ks.active {
		ks.split = next
		return nil
	}
	st, err := loadKillSwitchState()
	if err != nil {
		ks.split = narrowedSplit(ks.split, next)
		return fmt.Errorf("kill switch split permits: load state: %w", err)
	}
	if err := ks.render(ctx, ksRules{EndpointIPs: st.EndpointIPs, Tunnel: st.TunnelInterface, AllowLAN: ks.allowLAN, Split: next}); err != nil {
		ks.split = narrowedSplit(ks.split, next)
		return fmt.Errorf("kill switch split permits %w", err)
	}
	ks.split = next
	return nil
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
	chain, ok := liveIPTablesChain(ctx, "iptables", "OUTPUT", iptChainName, iptChainNameAlt)
	return ok && chain != ""
}

func hasNFT(ctx context.Context) bool {
	cmd := exec.CommandContext(ctx, "nft", "--version")
	return cmd.Run() == nil
}

func applyNFTRules(ctx context.Context, r ksRules) error {
	if r.Tunnel != "" && !validTunnelInterfaceName(r.Tunnel) {
		return fmt.Errorf("invalid tunnel interface name %q", r.Tunnel)
	}

	// nft -f runs the whole script as one kernel transaction, so there is
	// never a moment with no rules in place while the table is replaced.
	var b strings.Builder
	fmt.Fprintf(&b, "add table %s %s\n", nftFamily, nftTableName)
	fmt.Fprintf(&b, "delete table %s %s\n", nftFamily, nftTableName)
	b.WriteString(buildNFTRuleset(r))

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

// DropTunnelPermit removes the tunnel accepts while the lock stays, so an idle
// Lockdown lock names no interface at all.
func (ks *linuxKillSwitch) DropTunnelPermit(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if !ks.active {
		return nil
	}
	st, err := loadKillSwitchState()
	if err != nil {
		return fmt.Errorf("kill switch drop tunnel: load state: %w", err)
	}
	if st.TunnelInterface == "" {
		return nil
	}
	if err := ks.render(ctx, ksRules{EndpointIPs: st.EndpointIPs, AllowLAN: ks.allowLAN, Split: ks.split}); err != nil {
		return fmt.Errorf("kill switch drop tunnel %w", err)
	}
	st.TunnelInterface = ""
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch drop tunnel: save state: %w", err)
	}
	return nil
}
