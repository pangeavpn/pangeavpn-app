//go:build windows

package platform

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

const (
	ksRulePrefix           = "PangeaVPN-KS-"
	ksRuleLoopback         = ksRulePrefix + "Loopback"
	ksRuleLegacyLoopbackV6 = ksRulePrefix + "Loopback-v6"
	ksRuleEndpoint         = ksRulePrefix + "Endpoint"
	ksRuleLegacyTunnel     = ksRulePrefix + "Tunnel"
	ksRuleTunnelOutbound   = ksRulePrefix + "Tunnel-Out"
	ksRuleTunnelInbound    = ksRulePrefix + "Tunnel-In"
	ksRuleDHCP             = ksRulePrefix + "DHCP"
)

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		// Clean up any persistent sublayer left by a previous version.
		cleanupPersistentSublayer()
		return &windowsKillSwitch{}
	}
}

func cleanupPersistentSublayer() {
	engine, err := wfpOpen()
	if err != nil {
		return
	}
	defer engine.close()

	// Try direct delete first (works if no filters reference it).
	if engine.deleteSublayer() == nil {
		return
	}

	// Sublayer has persistent filters — enumerate and delete them first.
	engine.deleteAllSublayerFilters()
	_ = engine.deleteSublayer()
}

type windowsKillSwitch struct {
	mu     sync.Mutex
	active bool
	engine *wfpEngine // dynamic session — closing handle removes all filters
}

func (ks *windowsKillSwitch) Enable(ctx context.Context, endpointHost string) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	if ks.active {
		return nil
	}

	ips, err := resolveEndpointIPs(ctx, endpointHost)
	if err != nil {
		return fmt.Errorf("kill switch enable: %w", err)
	}

	// Persist state for crash recovery. No PreviousPolicy — WFP doesn't
	// modify the Windows Firewall outbound policy.
	st := KillSwitchState{
		Active:      true,
		EndpointIPs: ips,
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

	// WireGuard traffic flows: app → TUN adapter → wireguard-go → Cloak
	// (127.0.0.1) → Cloak endpoint IP. The loopback and endpoint IP permits
	// already cover this path. No blanket permit-all is needed — that would
	// defeat the kill switch by allowing traffic out non-tunnel interfaces.

	st, _ := loadKillSwitchState()
	st.TunnelInterface = tunnelInterface
	_ = saveKillSwitchState(st)

	return nil
}

func (ks *windowsKillSwitch) Clear(ctx context.Context) error {
	ks.mu.Lock()
	defer ks.mu.Unlock()

	var errs []string

	// Dynamic session: closing the engine handle removes all filters + sublayer.
	if ks.engine != nil {
		ks.engine.close()
		ks.engine = nil
	}

	// Handle legacy netsh rules from a previous version of PangeaVPN.
	st, stateErr := loadKillSwitchState()
	if stateErr == nil && len(st.PreviousPolicy) > 0 {
		if err := legacyNetshCleanup(ctx, st.PreviousPolicy); err != nil {
			errs = append(errs, fmt.Sprintf("legacy cleanup: %v", err))
		}
	}

	_ = removeKillSwitchState()
	ks.active = false

	if len(errs) > 0 {
		return fmt.Errorf("kill switch clear incomplete: %s", strings.Join(errs, "; "))
	}
	return nil
}

func (ks *windowsKillSwitch) Active() bool {
	ks.mu.Lock()
	defer ks.mu.Unlock()
	return ks.active
}

// ---------------------------------------------------------------------------
// Legacy netsh cleanup (for upgrades from PowerShell-based kill switch)
// ---------------------------------------------------------------------------

func legacyNetshCleanup(ctx context.Context, previousPolicy map[string]string) error {
	script := buildLegacyCleanupScript(previousPolicy)
	out, err := runHiddenCommand(ctx,
		"powershell.exe", "-NoProfile", "-NonInteractive",
		"-ExecutionPolicy", "Bypass", "-Command", script,
	)
	if err != nil {
		return fmt.Errorf("%w (%s)", err, strings.TrimSpace(out))
	}
	return nil
}

func buildLegacyCleanupScript(previousPolicy map[string]string) string {
	ruleNames := allKillSwitchRuleNames()
	quotedRules := make([]string, len(ruleNames))
	for i, name := range ruleNames {
		quotedRules[i] = fmt.Sprintf("'%s'", psSingleQuote(name))
	}
	ruleArray := "@(" + strings.Join(quotedRules, ",") + ")"

	policyEntries := make([]string, 0, 3)
	for _, profile := range []string{"domainprofile", "privateprofile", "publicprofile"} {
		outbound := "AllowOutbound"
		if previousPolicy != nil {
			if saved, ok := previousPolicy[profile]; ok && saved != "" {
				outbound = saved
			}
		}
		policyEntries = append(policyEntries, fmt.Sprintf("'%s'='%s'", psSingleQuote(profile), psSingleQuote(outbound)))
	}
	policyHashtable := "@{" + strings.Join(policyEntries, ";") + "}"

	parts := []string{
		fmt.Sprintf(
			"foreach ($r in %s) { netsh advfirewall firewall delete rule name=$r 2>&1 | Out-Null }",
			ruleArray,
		),
		fmt.Sprintf("$pol=%s", policyHashtable),
		"foreach ($p in $pol.Keys) {" +
			" $ob=$pol[$p];" +
			" $o=(netsh advfirewall show $p firewallpolicy 2>&1) -join \"`n\";" +
			" $ib='BlockInbound';" +
			" foreach ($ln in ($o -split \"`n\")) {" +
			" $lt=$ln.Trim().ToLower();" +
			" if ($lt -match 'firewall' -and $lt -match 'polic') {" +
			" $fld=($ln.Trim() -split '\\s+');" +
			" if ($fld.Count -gt 0) { $pr=$fld[-1] -split ','; if ($pr.Count -ge 1) { $ib=$pr[0].Trim() } }" +
			" } };" +
			" $sa=\"$ib,$ob\";" +
			" netsh advfirewall set $p firewallpolicy $sa 2>&1 | Out-Null" +
			" }",
	}

	return strings.Join(parts, "; ")
}

func allKillSwitchRuleNames() []string {
	return []string{
		ksRuleLoopback, ksRuleLegacyLoopbackV6, ksRuleEndpoint,
		ksRuleLegacyTunnel, ksRuleTunnelOutbound, ksRuleTunnelInbound, ksRuleDHCP,
	}
}

// ---------------------------------------------------------------------------
// Policy parsing helpers (kept for tests and legacy compatibility)
// ---------------------------------------------------------------------------

func parseOutboundPolicy(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		lowerLine := strings.ToLower(line)
		if !strings.Contains(lowerLine, "firewall") || !strings.Contains(lowerLine, "polic") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		policyPair := parts[len(parts)-1]
		commaParts := strings.SplitN(policyPair, ",", 2)
		if len(commaParts) == 2 {
			return strings.TrimSpace(commaParts[1])
		}
		return strings.TrimSpace(policyPair)
	}
	return ""
}

func parseInboundPolicy(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		lowerLine := strings.ToLower(line)
		if !strings.Contains(lowerLine, "firewall") || !strings.Contains(lowerLine, "polic") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}

		policyPair := parts[len(parts)-1]
		commaParts := strings.SplitN(policyPair, ",", 2)
		if len(commaParts) >= 1 {
			return strings.TrimSpace(commaParts[0])
		}
	}
	return "BlockInbound"
}

func buildTunnelRuleScript(tunnelInterface string) string {
	_ = tunnelInterface
	return fmt.Sprintf(
		"New-NetFirewallRule -Name '%s' -DisplayName '%s' -Direction Outbound -Action Allow -Profile Any -Enabled True -RemoteAddress '0.0.0.0-255.255.255.255' -ErrorAction Stop | Out-Null; "+
			"New-NetFirewallRule -Name '%s' -DisplayName '%s' -Direction Inbound -Action Allow -Profile Any -Enabled True -RemoteAddress '0.0.0.0-255.255.255.255' -ErrorAction Stop | Out-Null",
		psSingleQuote(ksRuleTunnelOutbound),
		psSingleQuote(ksRuleTunnelOutbound),
		psSingleQuote(ksRuleTunnelInbound),
		psSingleQuote(ksRuleTunnelInbound),
	)
}
