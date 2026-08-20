//go:build darwin

package platform

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

const (
	pfAnchorName     = "com.pangeavpn.killswitch"
	pfAnchorFilePath = "/etc/pf.anchors/" + pfAnchorName
	pfConfPath       = "/etc/pf.conf"
	pfAnchorLine     = `anchor "` + pfAnchorName + `"`
	pfLoadAnchorLine = `load anchor "` + pfAnchorName + `" from "` + pfAnchorFilePath + `"`
	pfConfBackupFile = "pf.conf.pangea-backup"
	pfTokenFile      = "killswitch-pf-token.txt"
)

var tunnelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)
var pfTokenPattern = regexp.MustCompile(`Token\s*:\s*(\d+)`)

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		return &darwinKillSwitch{}
	}
}

type darwinKillSwitch struct {
	mu       sync.Mutex
	active   bool
	allowLAN bool
}

func (ks *darwinKillSwitch) Enable(ctx context.Context, endpointHosts []string, allowLAN bool, locked bool) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	ips, err := resolveEndpointHosts(ctx, endpointHosts)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}

	var tunnelInterface string
	var prev KillSwitchState
	if ks.active {
		prev, err = loadKillSwitchState()
		if err != nil {
			return fmt.Errorf("kill switch enable: load state: %w", err)
		}
		tunnelInterface = prev.TunnelInterface
		// Only skip re-arming if the live anchor still enforces the lock;
		// an externally flushed anchor must always be re-applied.
		if stringSlicesEqual(prev.EndpointIPs, ips) && prev.AllowLAN == allowLAN {
			if verifyPFAnchorLive(ctx) == nil {
				return persistLockedUpgrade(prev, locked)
			}
		}
	}

	firstActivation := !ks.active
	var token string
	if firstActivation {
		token, err = enablePF(ctx)
		if err != nil {
			return fmt.Errorf("kill switch enable: %w", err)
		}
	}

	if err := applyPFAnchor(ctx, ips, tunnelInterface, allowLAN); err != nil {
		if firstActivation {
			_ = disablePF(ctx, token)
			_ = removePFAnchor(ctx)
		} else if rbErr := applyPFAnchor(ctx, prev.EndpointIPs, prev.TunnelInterface, prev.AllowLAN); rbErr != nil {
			KillSwitchWarn("kill switch enable: rollback to previous ruleset failed: %v", rbErr)
		}
		return fmt.Errorf("kill switch enable: %w", err)
	}

	if firstActivation {
		if err := savePFToken(token); err != nil {
			_ = disablePF(ctx, token)
			_ = removePFAnchor(ctx)
			return fmt.Errorf("kill switch enable: save pf token: %w", err)
		}
	}

	ks.active = true
	ks.allowLAN = allowLAN

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

func (ks *darwinKillSwitch) Update(ctx context.Context, tunnel TunnelRef) error {
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

	if err := applyPFAnchor(ctx, st.EndpointIPs, tunnelInterface, ks.allowLAN); err != nil {
		return fmt.Errorf("kill switch update: %w", err)
	}

	st.TunnelInterface = tunnelInterface
	if err := saveKillSwitchState(st); err != nil {
		return fmt.Errorf("kill switch update: save state: %w", err)
	}
	return nil
}

func (ks *darwinKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if err := removePFAnchor(ctx); err != nil {
		return fmt.Errorf("kill switch clear: remove anchor: %w", err)
	}

	if token, err := loadPFToken(); err != nil {
		return fmt.Errorf("kill switch clear: load pf token: %w", err)
	} else if token != "" {
		if err := disablePF(ctx, token); err != nil {
			return fmt.Errorf("kill switch clear: disable pf: %w", err)
		}
	}
	_ = removePFToken()

	if err := removeKillSwitchState(); err != nil {
		return fmt.Errorf("kill switch clear: remove state: %w", err)
	}
	ks.active = false
	return nil
}

func (ks *darwinKillSwitch) Active() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.active
}

// ---------------------------------------------------------------------------
// PF anchor management
// ---------------------------------------------------------------------------

// buildPFRules generates a PF ruleset for the kill-switch anchor.
func buildPFRules(endpointIPs []string, tunnelInterface string, allowLAN bool) (string, error) {
	if tunnelInterface != "" && !tunnelNamePattern.MatchString(tunnelInterface) {
		return "", fmt.Errorf("invalid tunnel interface name %q", tunnelInterface)
	}

	var rules []string

	// Allow all loopback traffic.
	rules = append(rules, "pass out quick on lo0 all")

	// Allow traffic to VPN transport endpoint IPs, v4 and v6 alike.
	for _, ip := range endpointIPs {
		if strings.Contains(ip, ":") {
			rules = append(rules, fmt.Sprintf("pass out quick inet6 proto { tcp udp } to %s", ip))
			continue
		}
		rules = append(rules, fmt.Sprintf("pass out quick inet proto { tcp udp } to %s", ip))
	}

	// Allow DHCP.
	rules = append(rules, "pass out quick inet proto udp from any port 68 to any port 67")

	// Allow LAN ranges so captive portals and gateway probes work on
	// restrictive WiFi. Only applied when the user opts in.
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			rules = append(rules, fmt.Sprintf("pass out quick inet to %s", cidr))
		}
	}

	// Allow traffic on the tunnel interface if set.
	if tunnelInterface != "" {
		rules = append(rules, fmt.Sprintf("pass out quick on %s inet all", tunnelInterface))
	}

	// Block everything else outbound.
	rules = append(rules, "block out all")

	return strings.Join(rules, "\n") + "\n", nil
}

// applyPFAnchor writes the kill-switch ruleset to disk, wires it into
// /etc/pf.conf, reloads pf, and verifies the block rule is actually live.
func applyPFAnchor(ctx context.Context, endpointIPs []string, tunnelInterface string, allowLAN bool) error {
	rules, err := buildPFRules(endpointIPs, tunnelInterface, allowLAN)
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
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "block") && strings.Contains(line, "out all") {
			return nil
		}
	}
	return fmt.Errorf("block rule not found in live pf anchor %s", pfAnchorName)
}

// ensurePFConf idempotently wires our anchor into /etc/pf.conf. pf only
// evaluates anchors the main ruleset references, so without this the
// killswitch anchor is loaded but never consulted.
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

// removePFAnchor flushes the live anchor rules and empties the on-disk
// anchor file so a reload (including one before the daemon next starts)
// does not resurrect a stale block-all.
func removePFAnchor(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "pfctl", "-a", pfAnchorName, "-F", "all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.ToLower(string(out))
		if !strings.Contains(trimmed, "no such") {
			return fmt.Errorf("flush pf anchor: %w (%s)", err, strings.TrimSpace(string(out)))
		}
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

// ---------------------------------------------------------------------------
// PF reference-token persistence
//
// KillSwitchState (killswitch.go) is shared across platforms; the pf token
// is darwin-only, so it is tracked in its own small file alongside it.
// ---------------------------------------------------------------------------

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
