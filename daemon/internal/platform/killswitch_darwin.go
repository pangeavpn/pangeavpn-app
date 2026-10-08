//go:build darwin

package platform

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	pfAnchorName      = "com.pangeavpn.killswitch"
	pfSplitAnchorPath = pfAnchorName + "/" + pfSplitAnchor
	pfAnchorFilePath  = "/etc/pf.anchors/" + pfAnchorName
	pfConfPath        = "/etc/pf.conf"
	pfAnchorLine      = `anchor "` + pfAnchorName + `"`
	pfLoadAnchorLine  = `load anchor "` + pfAnchorName + `" from "` + pfAnchorFilePath + `"`
	pfConfBackupFile  = "pf.conf.pangea-backup"
	pfTokenFile       = "killswitch-pf-token.txt"
)

var pfTokenPattern = regexp.MustCompile(`Token\s*:\s*(\d+)`)

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		return &darwinKillSwitch{}
	}
}

var _ SplitTunnelPermitter = (*darwinKillSwitch)(nil)

// Seams so lifecycle tests never reach pfctl or the directory service.
var (
	pfApply        = applyPFAnchor
	pfEnable       = enablePF
	pfIsEnabled    = pfEnabled
	pfVerifyLive   = verifyPFAnchorLive
	pfFlushStates  = flushPFStates
	pfKillStates   = killPFStates
	pfDisable      = disablePF
	pfRemoveAnchor = removePFAnchor
	pfFlushSplit   = func(ctx context.Context) error { return flushPFAnchor(ctx, pfSplitAnchorPath) }
	splitEgressGID = SplitEgressGroupID
)

// pfSplitError is a split-anchor failure behind a main lock already loaded and verified live.
type pfSplitError struct{ err error }

func (e *pfSplitError) Error() string { return e.err.Error() }
func (e *pfSplitError) Unwrap() error { return e.err }

type darwinKillSwitch struct {
	// opMu serialises Enable/Update/Clear; stateMu guards the flags so
	// Active() (called by every /status) never queues behind a slow op.
	opMu     sync.Mutex
	stateMu  sync.Mutex
	active   bool
	allowLAN bool

	// Under opMu, never persisted: split is what renders carry, pfSplit what the
	// live anchor holds, egressGID the group SetSplitEgress resolved.
	split     splitPermits
	pfSplit   splitPermits
	egressGID int

	// Cached pf probe, so a 1Hz status poll does not fork pfctl each time.
	liveAt time.Time
	live   bool
}

const (
	pfProbeTTL     = 1500 * time.Millisecond
	pfProbeTimeout = 3 * time.Second
	pfCleanupGrace = 5 * time.Second
)

func (ks *darwinKillSwitch) snapshotState() (active, allowLAN bool) {
	ks.stateMu.Lock()
	defer ks.stateMu.Unlock()
	return ks.active, ks.allowLAN
}

func (ks *darwinKillSwitch) setState(active, allowLAN bool) {
	ks.stateMu.Lock()
	defer ks.stateMu.Unlock()
	ks.active = active
	ks.allowLAN = allowLAN
}

func (ks *darwinKillSwitch) Enable(ctx context.Context, endpointHosts []string, allowLAN bool, locked bool) error {
	ks.opMu.Lock()
	defer ks.opMu.Unlock()
	wasActive, _ := ks.snapshotState()

	ips, err := resolveEndpointHosts(ctx, endpointHosts)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}

	var tunnelInterface string
	var prev KillSwitchState
	if wasActive {
		prev, err = loadKillSwitchState()
		if err != nil {
			return fmt.Errorf("kill switch enable: load state: %w", err)
		}
		tunnelInterface = prev.TunnelInterface
		// Only skip re-arming if the live anchor still enforces the lock and
		// carries the split permits; an externally flushed anchor is re-applied.
		if stringSlicesEqual(prev.EndpointIPs, ips) && prev.AllowLAN == allowLAN && ks.pfSplit.equal(ks.split) {
			if pfVerifyLive(ctx) == nil {
				return persistLockedUpgrade(prev, locked)
			}
		}
	}

	// pf itself can be switched off under a live anchor (another tool's
	// pfctl -d); a re-arm takes a fresh reference whenever that happened.
	firstActivation := !wasActive || !pfIsEnabled(ctx)
	var token string
	if firstActivation {
		token, err = pfEnable(ctx)
		if err != nil {
			return fmt.Errorf("kill switch enable: %w", err)
		}
	}

	if err := ks.render(ctx, ksRules{EndpointIPs: ips, Tunnel: tunnelInterface, AllowLAN: allowLAN, Split: ks.split}); err != nil {
		var splitErr *pfSplitError
		if !errors.As(err, &splitErr) {
			// The caller's ctx is often the one that just died; cleanup gets its own.
			cleanupCtx, cancel := context.WithTimeout(context.Background(), pfCleanupGrace)
			defer cancel()
			if !wasActive {
				_ = pfDisable(cleanupCtx, token)
				_ = pfRemoveAnchor(cleanupCtx)
				ks.pfSplit = splitPermits{}
			} else if rbErr := ks.render(cleanupCtx, ksRules{EndpointIPs: prev.EndpointIPs, Tunnel: prev.TunnelInterface, AllowLAN: prev.AllowLAN, Split: ks.split}); rbErr != nil {
				KillSwitchWarn("kill switch enable: rollback to previous ruleset failed: %v", rbErr)
			}
			return fmt.Errorf("kill switch enable: %w", err)
		}
		// The block rule is live; tearing it down over the split permits would unlock the host.
		KillSwitchWarn("kill switch enable: armed without the split-tunnel permits: %v", err)
	}

	if firstActivation {
		if err := savePFToken(token); err != nil {
			cleanupCtx, cancel := context.WithTimeout(context.Background(), pfCleanupGrace)
			defer cancel()
			_ = pfDisable(cleanupCtx, token)
			if !wasActive {
				_ = pfRemoveAnchor(cleanupCtx)
				ks.pfSplit = splitPermits{}
			}
			return fmt.Errorf("kill switch enable: save pf token: %w", err)
		}
		pfFlushStates(ctx)
	}

	ks.setState(true, allowLAN)

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

func (ks *darwinKillSwitch) Update(ctx context.Context, tunnel TunnelRef) error {
	ks.opMu.Lock()
	defer ks.opMu.Unlock()

	active, allowLAN := ks.snapshotState()
	if !active {
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

	if err := ks.render(ctx, ksRules{EndpointIPs: st.EndpointIPs, Tunnel: tunnelInterface, AllowLAN: allowLAN, Split: ks.split}); err != nil {
		var splitErr *pfSplitError
		if !errors.As(err, &splitErr) {
			return fmt.Errorf("kill switch update: %w", err)
		}
		KillSwitchWarn("kill switch update: lock updated without the split-tunnel permits: %v", err)
	}

	st.TunnelInterface = tunnelInterface
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch update: save state: %w", err)
	}
	return nil
}

func (ks *darwinKillSwitch) Clear(ctx context.Context) error {
	ks.opMu.Lock()
	defer ks.opMu.Unlock()
	// Dropped first: a re-arm after a failed Clear must not bring the permits back.
	ks.split, ks.egressGID = splitPermits{}, 0

	if err := pfRemoveAnchor(ctx); err != nil {
		return fmt.Errorf("kill switch clear: remove anchor: %w", err)
	}
	ks.pfSplit = splitPermits{}

	if token, err := loadPFToken(); err != nil {
		return fmt.Errorf("kill switch clear: load pf token: %w", err)
	} else if token != "" {
		if err := pfDisable(ctx, token); err != nil {
			return fmt.Errorf("kill switch clear: disable pf: %w", err)
		}
	}
	_ = removePFToken()

	if err := removeKillSwitchState(); err != nil {
		return fmt.Errorf("kill switch clear: remove state: %w", err)
	}
	ks.setState(false, false)
	return nil
}

// SetSplitEgress lets sockets of the split-egress group out of the lock. It fails
// when the group is missing or shared, since pf could then match other sockets.
func (ks *darwinKillSwitch) SetSplitEgress(ctx context.Context, on bool) error {
	ks.opMu.Lock()
	defer ks.opMu.Unlock()
	next := ks.split.clone()
	next.Egress = on
	if on {
		gid, err := splitEgressGID()
		if err != nil {
			err = fmt.Errorf("kill switch split egress: %w", err)
			if ks.split.Egress {
				next.Egress = false
				err = errors.Join(err, ks.setSplit(ctx, next))
			}
			return err
		}
		ks.egressGID = gid
	}
	return ks.setSplit(ctx, next)
}

// SetSplitCIDRs permits the excluded destination ranges, resolvers excepted.
func (ks *darwinKillSwitch) SetSplitCIDRs(ctx context.Context, cidrs []string) error {
	normalized, err := normalizeSplitCIDRs(cidrs)
	if err != nil {
		return fmt.Errorf("kill switch split permits: %w", err)
	}
	ks.opMu.Lock()
	defer ks.opMu.Unlock()
	next := ks.split.clone()
	next.CIDRs = normalized
	return ks.setSplit(ctx, next)
}

// setSplit re-renders an armed lock with next; an idle switch only records it for
// the next Enable. Always renders, so a retry repairs an earlier failed narrowing.
func (ks *darwinKillSwitch) setSplit(ctx context.Context, next splitPermits) error {
	active, allowLAN := ks.snapshotState()
	if !active {
		ks.split = next
		return nil
	}
	st, err := loadKillSwitchState()
	if err != nil {
		ks.split = narrowedSplit(ks.pfSplit, next)
		return fmt.Errorf("kill switch split permits: load state: %w", err)
	}
	if err := ks.render(ctx, ksRules{EndpointIPs: st.EndpointIPs, Tunnel: st.TunnelInterface, AllowLAN: allowLAN, Split: next}); err != nil {
		ks.split = narrowedSplit(ks.pfSplit, next)
		return fmt.Errorf("kill switch split permits: %w", err)
	}
	ks.split = next
	return nil
}

// render loads r into the anchor, then ends the states of ranges it stopped
// permitting: pf keeps established flows across a reload. opMu held.
func (ks *darwinKillSwitch) render(ctx context.Context, r ksRules) error {
	gid := 0
	if r.Split.Egress {
		gid = ks.egressGID
	}
	if err := pfApply(ctx, r, gid); err != nil {
		var splitErr *pfSplitError
		if !errors.As(err, &splitErr) {
			return err
		}
		// An empty split anchor fails the permits closed; the stale set it held could be wider.
		if ferr := pfFlushSplit(ctx); ferr != nil {
			return &pfSplitError{err: fmt.Errorf("%w; emptying the split anchor also failed: %v", splitErr.err, ferr)}
		}
		r.Split = splitPermits{}
		ks.killDroppedRanges(ctx, r)
		return err
	}
	ks.killDroppedRanges(ctx, r)
	return nil
}

// killDroppedRanges ends the states of ranges r no longer permits. Egress-group states need
// no kill: those sockets are the split engine's own, closed before the permit is withdrawn.
func (ks *darwinKillSwitch) killDroppedRanges(ctx context.Context, r ksRules) {
	if gone := splitRangesToKill(ks.pfSplit.CIDRs, r); len(gone) > 0 {
		pfKillStates(ctx, gone, r.Tunnel)
	}
	ks.pfSplit = r.Split.clone()
}

func (ks *darwinKillSwitch) Active() bool {
	if active, _ := ks.snapshotState(); active {
		return true
	}
	return ks.lockLive()
}

// lockLive asks pf whether a lock is enforcing right now: one left by a
// previous process counts even though this one never armed it.
func (ks *darwinKillSwitch) lockLive() bool {
	ks.stateMu.Lock()
	if time.Since(ks.liveAt) < pfProbeTTL {
		live := ks.live
		ks.stateMu.Unlock()
		return live
	}
	ks.stateMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), pfProbeTimeout)
	defer cancel()
	live := pfIsEnabled(ctx) && pfVerifyLive(ctx) == nil

	ks.stateMu.Lock()
	ks.live, ks.liveAt = live, time.Now()
	ks.stateMu.Unlock()
	return live
}

// pfEnabled reads pf's own status: loaded rules enforce nothing while pf is off.
func pfEnabled(ctx context.Context) bool {
	out, err := exec.CommandContext(ctx, "pfctl", "-s", "info").CombinedOutput()
	return err == nil && strings.Contains(string(out), "Status: Enabled")
}

// applyPFAnchor writes the lock to disk, wires it into /etc/pf.conf, reloads and
// verifies it, then loads the split rules into the in-memory child anchor.
func applyPFAnchor(ctx context.Context, r ksRules, egressGID int) error {
	rules, err := buildPFRules(r)
	if err != nil {
		return err
	}

	if err := os.WriteFile(pfAnchorFilePath, []byte(rules), 0o644); err != nil {
		return fmt.Errorf("write pf anchor file: %w", err)
	}

	if err := ensurePFConf(); err != nil {
		return err
	}

	reloadCmd := exec.CommandContext(ctx, "pfctl", "-f", pfConfPath)
	if out, err := reloadCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reload pf.conf: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	if err := verifyPFAnchorLive(ctx); err != nil {
		return fmt.Errorf("verify pf anchor: %w", err)
	}
	if err := loadPFSplitAnchor(ctx, pfSplitRules(r.Split, egressGID)); err != nil {
		return &pfSplitError{err: err}
	}
	return nil
}

// loadPFSplitAnchor swaps the split rules into the child anchor the file hooks,
// straight from memory; an empty set flushes it.
func loadPFSplitAnchor(ctx context.Context, rules []string) error {
	if len(rules) == 0 {
		return flushPFAnchor(ctx, pfSplitAnchorPath)
	}
	cmd := exec.CommandContext(ctx, "pfctl", "-a", pfSplitAnchorPath, "-f", "-")
	cmd.Stdin = strings.NewReader(strings.Join(rules, "\n") + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("load pf split anchor: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func flushPFAnchor(ctx context.Context, anchor string) error {
	out, err := exec.CommandContext(ctx, "pfctl", "-a", anchor, "-F", "all").CombinedOutput()
	if err != nil && !strings.Contains(strings.ToLower(string(out)), "no such") {
		return fmt.Errorf("flush pf anchor %s: %w (%s)", anchor, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// verifyPFAnchorLive confirms the block rule is actually loaded and
// evaluated in the kernel, not just written to the anchor file on disk.
func verifyPFAnchorLive(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "pfctl", "-a", pfAnchorName, "-s", "rules")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("read pf anchor rules: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if strings.Contains(line, "block") && strings.Contains(line, "out all") {
			return nil
		}
	}
	return fmt.Errorf("block rule not found in live pf anchor %s", pfAnchorName)
}

// ensurePFConf idempotently wires our anchor into /etc/pf.conf: pf only
// evaluates anchors the main ruleset references.
func ensurePFConf() error {
	data, err := os.ReadFile(pfConfPath)
	if err != nil {
		return fmt.Errorf("read pf.conf: %w", err)
	}
	if strings.Contains(string(data), pfLoadAnchorLine) {
		return nil
	}

	if err := backupPFConfOnce(data); err != nil {
		return err
	}

	updated := string(data)
	if !strings.HasSuffix(updated, "\n") {
		updated += "\n"
	}
	// Appended last: pf is last-match-wins for non-quick rules, so our
	// anchor must be evaluated after everything already in pf.conf.
	updated += pfAnchorLine + "\n" + pfLoadAnchorLine + "\n"

	if err := os.WriteFile(pfConfPath, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write pf.conf: %w", err)
	}
	return nil
}

// backupPFConfOnce preserves the original pf.conf before our first edit.
func backupPFConfOnce(original []byte) error {
	path, err := pfConfBackupPath()
	if err != nil {
		return err
	}
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, original, 0o644); err != nil {
		return fmt.Errorf("backup pf.conf: %w", err)
	}
	return nil
}

func pfConfBackupPath() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pfConfBackupFile), nil
}

// removePFAnchor flushes the live anchor rules and empties the on-disk anchor
// file so a later reload does not resurrect a stale block-all.
func removePFAnchor(ctx context.Context) error {
	// The split anchor too: the next arm's hook would otherwise pick its rules up again.
	if err := errors.Join(flushPFAnchor(ctx, pfSplitAnchorPath), flushPFAnchor(ctx, pfAnchorName)); err != nil {
		return err
	}

	if err := os.WriteFile(pfAnchorFilePath, nil, 0o644); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear pf anchor file: %w", err)
	}
	return nil
}

// enablePF reference-counts pf on via -E and returns the token needed to
// release our reference later without disabling pf for other users.
func enablePF(ctx context.Context) (string, error) {
	cmd := exec.CommandContext(ctx, "pfctl", "-E")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("enable pf: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	match := pfTokenPattern.FindStringSubmatch(string(out))
	if match == nil {
		return "", fmt.Errorf("enable pf: no reference token in output (%s)", strings.TrimSpace(string(out)))
	}
	return match[1], nil
}

// disablePF releases our pf reference token, returning pf to whatever
// enabled/disabled state existed before we engaged the kill switch.
func disablePF(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "pfctl", "-X", token)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("disable pf: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// pfTokenPath and friends track the darwin-only pf reference token in its
// own file, alongside the cross-platform KillSwitchState in killswitch.go.
func pfTokenPath() (string, error) {
	dir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, pfTokenFile), nil
}

func savePFToken(token string) error {
	path, err := pfTokenPath()
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return fmt.Errorf("write pf token: %w", err)
	}
	return nil
}

func loadPFToken() (string, error) {
	path, err := pfTokenPath()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("read pf token: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

func removePFToken() error {
	path, err := pfTokenPath()
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pf token: %w", err)
	}
	return nil
}

// DropTunnelPermit removes the utun pass rule while the lock stays: macOS gives
// the same utunN to whatever creates a tunnel next.
func (ks *darwinKillSwitch) DropTunnelPermit(ctx context.Context) error {
	ks.opMu.Lock()
	defer ks.opMu.Unlock()

	active, allowLAN := ks.snapshotState()
	if !active {
		return nil
	}
	st, err := loadKillSwitchState()
	if err != nil {
		return fmt.Errorf("kill switch drop tunnel: load state: %w", err)
	}
	if st.TunnelInterface == "" {
		return nil
	}
	if err := ks.render(ctx, ksRules{EndpointIPs: st.EndpointIPs, AllowLAN: allowLAN, Split: ks.split}); err != nil {
		return fmt.Errorf("kill switch drop tunnel: %w", err)
	}
	st.TunnelInterface = ""
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch drop tunnel: save state: %w", err)
	}
	return nil
}

// flushPFStates ends the flows pf tracked before the lock landed: with pf already
// on for another tool, an established connection would otherwise outlive block out all.
func flushPFStates(ctx context.Context) {
	if out, err := exec.CommandContext(ctx, "pfctl", "-F", "states").CombinedOutput(); err != nil {
		KillSwitchWarn("kill switch enable: could not flush pf states: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

// killPFStates ends the host's off-tunnel flows to and from ranges the anchor no
// longer permits.
func killPFStates(ctx context.Context, cidrs []string, tunnel string) {
	locals, err := pfKillLocalAddrs(tunnel)
	if err != nil {
		KillSwitchWarn("kill switch: could not list local addresses to end pf states: %v", err)
		return
	}
	for _, args := range pfKillArgs(locals, cidrs) {
		if out, err := exec.CommandContext(ctx, "pfctl", args...).CombinedOutput(); err != nil {
			KillSwitchWarn("kill switch: could not end pf states %v: %v (%s)", args, err, strings.TrimSpace(string(out)))
		}
	}
}

const dsclTimeout = 5 * time.Second

// SplitEgressGroupID resolves the gid pf matches for split-tunnel egress sockets.
// It refuses a group anyone else could carry: members, or a user's primary gid.
func SplitEgressGroupID() (int, error) {
	grp, err := user.LookupGroup(SplitEgressGroupName)
	if err != nil {
		return 0, fmt.Errorf("look up group %s: %w", SplitEgressGroupName, err)
	}
	gid, err := splitEgressGIDFromString(grp.Gid)
	if err != nil {
		return 0, err
	}
	if back, err := user.LookupGroupId(grp.Gid); err != nil || back.Name != SplitEgressGroupName {
		return 0, fmt.Errorf("gid %d does not resolve back to %s; another group shares it", gid, SplitEgressGroupName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), dsclTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "/usr/bin/dscl", ".", "-read", "/Groups/"+SplitEgressGroupName).Output()
	if err != nil {
		return 0, fmt.Errorf("read group %s: %w", SplitEgressGroupName, err)
	}
	if dsclGroupHasMembers(string(out)) {
		return 0, fmt.Errorf("group %s has members, who could send through the egress pass", SplitEgressGroupName)
	}
	out, err = exec.CommandContext(ctx, "/usr/bin/dscl", ".", "-list", "/Users", "PrimaryGroupID").Output()
	if err != nil {
		return 0, fmt.Errorf("list user primary groups: %w", err)
	}
	if users := dsclPrimaryGIDUsers(string(out), gid); len(users) > 0 {
		return 0, fmt.Errorf("gid %d of group %s is the primary group of %d local users", gid, SplitEgressGroupName, len(users))
	}
	// LDAP/AD records too. Unreachable means no one can log in from it, so only warn.
	search := func(path string) string {
		out, err := exec.CommandContext(ctx, "/usr/bin/dscl", "/Search", "-search", path, "PrimaryGroupID", strconv.Itoa(gid)).Output()
		if err != nil {
			KillSwitchWarn("kill switch: directory search of %s for gid %d failed: %v", path, gid, err)
		}
		return string(out)
	}
	if err := splitEgressDirectoryCheck(gid, search("/Users"), search("/Groups")); err != nil {
		return 0, err
	}
	return gid, nil
}
