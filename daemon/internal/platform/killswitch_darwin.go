//go:build darwin

package platform

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync"
)

const (
	pfAnchorName = "com.pangeavpn.killswitch"
	pfAnchorPath = "com.pangeavpn.killswitch"
)

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
	if ks.active {
		prev, _ := loadKillSwitchState()
		if stringSlicesEqual(prev.EndpointIPs, ips) && prev.AllowLAN == allowLAN {
			// Rules match; still record a Lockdown re-arm.
			return persistLockedUpgrade(prev, locked)
		}
		tunnelInterface = prev.TunnelInterface
	}

	if err := applyPFAnchor(ctx, ips, tunnelInterface, allowLAN); err != nil {
		if !ks.active {
			_ = removePFAnchor(ctx)
		}
		return fmt.Errorf("kill switch enable: %w", err)
	}

	// Rules are live from here. Marking active before the save means a save
	// failure still leaves the anchor trackable by Clear.
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

	st, _ := loadKillSwitchState()
	st.TunnelInterface = tunnelInterface
	_ = saveKillSwitchState(st)

	if err := applyPFAnchor(ctx, st.EndpointIPs, tunnelInterface, ks.allowLAN); err != nil {
		return fmt.Errorf("kill switch update: %w", err)
	}

	return nil
}

func (ks *darwinKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var errs []string

	if err := removePFAnchor(ctx); err != nil {
		errs = append(errs, fmt.Sprintf("remove anchor: %v", err))
	}

	_ = removeKillSwitchState()
	ks.active = false

	if len(errs) > 0 {
		return fmt.Errorf("kill switch clear incomplete: %s", strings.Join(errs, "; "))
	}
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
func buildPFRules(endpointIPs []string, tunnelInterface string, allowLAN bool) string {
	var rules []string

	// Allow all loopback traffic.
	rules = append(rules, "pass out quick on lo0 all")

	// Allow traffic to VPN transport endpoint IPs.
	for _, ip := range endpointIPs {
		if strings.Contains(ip, ":") {
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

	return strings.Join(rules, "\n") + "\n"
}

// applyPFAnchor loads the kill-switch rules into a PF anchor.
func applyPFAnchor(ctx context.Context, endpointIPs []string, tunnelInterface string, allowLAN bool) error {
	rules := buildPFRules(endpointIPs, tunnelInterface, allowLAN)

	// Load rules into the anchor via stdin.
	cmd := exec.CommandContext(ctx, "pfctl", "-a", pfAnchorPath, "-f", "-")
	cmd.Stdin = strings.NewReader(rules)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("load pf anchor: %w (%s)", err, strings.TrimSpace(string(out)))
	}

	// Ensure PF is enabled.
	enableCmd := exec.CommandContext(ctx, "pfctl", "-e")
	enableOut, enableErr := enableCmd.CombinedOutput()
	if enableErr != nil {
		// pfctl -e returns error if already enabled; check output.
		trimmed := strings.ToLower(string(enableOut))
		if !strings.Contains(trimmed, "already enabled") && !strings.Contains(trimmed, "pf enabled") {
			return fmt.Errorf("enable pf: %w (%s)", enableErr, strings.TrimSpace(string(enableOut)))
		}
	}

	return nil
}

// removePFAnchor flushes and removes the kill-switch anchor rules.
func removePFAnchor(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "pfctl", "-a", pfAnchorPath, "-F", "all")
	out, err := cmd.CombinedOutput()
	if err != nil {
		trimmed := strings.ToLower(string(out))
		if strings.Contains(trimmed, "no such") {
			return nil
		}
		return fmt.Errorf("flush pf anchor: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}
