//go:build windows

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

type netRouteRecord struct {
	DestinationPrefix string
	InterfaceAlias    string
	InterfaceIndex    int
	NextHop           string
}

type defaultGatewayRecord struct {
	Gateway        string
	InterfaceAlias string
	InterfaceIndex int
	Metric         int
}

// RepairNetworkAfterTunnelDisconnect performs targeted cleanup for stale tunnel networking state.
func RepairNetworkAfterTunnelDisconnect(ctx context.Context, tunnelNames []string) ([]string, error) {
	actions := make([]string, 0, 8)

	flushOutput, flushErr := runHiddenCommand(ctx, "ipconfig", "/flushdns")
	if flushErr != nil {
		actions = append(actions, fmt.Sprintf("warning: dns flush failed: %v (%s)", flushErr, strings.TrimSpace(flushOutput)))
	} else {
		actions = append(actions, "flushed DNS cache")
	}

	destCacheOutput, destCacheErr := runHiddenCommand(ctx, "netsh", "interface", "ip", "delete", "destinationcache")
	if destCacheErr != nil {
		actions = append(actions, fmt.Sprintf("warning: destination cache cleanup failed: %v (%s)", destCacheErr, strings.TrimSpace(destCacheOutput)))
	} else {
		actions = append(actions, "cleared IP destination cache")
	}

	// The common disconnect needs only the cache hygiene above: gateway intact,
	// tunnel routes gone with the adapter. Skip the route surgery when so.
	if ok, _ := connectivityRestored(ctx, tunnelNames); ok {
		return actions, nil
	}

	removedRoutes, removeErr := removeLikelyTunnelDefaultRoutes(ctx, tunnelNames)
	if removeErr != nil {
		actions = append(actions, fmt.Sprintf("warning: stale route cleanup failed: %v", removeErr))
	} else if len(removedRoutes) > 0 {
		actions = append(actions, fmt.Sprintf("removed stale tunnel default routes: %s", strings.Join(removedRoutes, ", ")))
	}

	if stale, staleErr := hasStaleTunnelDefaultRoute(ctx, tunnelNames); staleErr != nil {
		actions = append(actions, fmt.Sprintf("warning: stale route verification failed: %v", staleErr))
	} else if stale {
		actions = append(actions, "warning: tunnel default route still present after cleanup")
	}

	if ok, _ := connectivityRestored(ctx, tunnelNames); ok {
		return actions, nil
	}

	renewedAdapters, renewAdaptersErr := renewLikelyPrimaryAdapters(ctx, tunnelNames)
	if renewAdaptersErr != nil {
		actions = append(actions, fmt.Sprintf("warning: adapter renew failed: %v", renewAdaptersErr))
	} else if len(renewedAdapters) > 0 {
		actions = append(actions, fmt.Sprintf("renewed active adapters after missing default route: %s", strings.Join(renewedAdapters, ", ")))
	}

	if ok, _ := connectivityRestored(ctx, tunnelNames); ok {
		actions = append(actions, "restored default route via targeted adapter renew")
		return actions, nil
	}

	renewOutput, renewErr := runHiddenCommand(ctx, "ipconfig", "/renew")
	if renewErr != nil {
		return actions, fmt.Errorf("default route missing after disconnect and network renew failed: %w (%s)", renewErr, strings.TrimSpace(renewOutput))
	}

	if ok, verifyErr := connectivityRestored(ctx, tunnelNames); !ok {
		if verifyErr != nil {
			return actions, fmt.Errorf("default route still missing after network renew: %w", verifyErr)
		}
		return actions, fmt.Errorf("stale tunnel default route still present after network renew")
	}

	actions = append(actions, "restored default route via network renew")
	return actions, nil
}

// connectivityRestored reports whether a non-tunnel IPv4 default route exists
// and no tunnel-owned default route (IPv4 or IPv6) is still present.
func connectivityRestored(ctx context.Context, tunnelNames []string) (bool, error) {
	if _, _, err := resolveDefaultGateway(ctx, tunnelNames); err != nil {
		return false, err
	}
	stale, err := hasStaleTunnelDefaultRoute(ctx, tunnelNames)
	if err != nil {
		return false, err
	}
	return !stale, nil
}

func renewLikelyPrimaryAdapters(ctx context.Context, tunnelNames []string) ([]string, error) {
	script := buildAdapterRenewScript(tunnelNames)
	output, err := runHiddenCommand(ctx, "powershell.exe", psArgs(script)...)
	if err != nil {
		return nil, fmt.Errorf("powershell adapter renew command failed: %w (%s)", err, strings.TrimSpace(output))
	}

	trimmed := strings.TrimSpace(output)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil, nil
	}

	renewed, parseErr := parseJSONStrings(trimmed)
	if parseErr != nil {
		return nil, fmt.Errorf("parse adapter renew output failed: %w (%s)", parseErr, trimmed)
	}
	return renewed, nil
}

func parseJSONStrings(raw string) ([]string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil, nil
	}

	var list []string
	if err := json.Unmarshal([]byte(trimmed), &list); err == nil {
		return normalizeNonEmptyStrings(list), nil
	}

	var single string
	if err := json.Unmarshal([]byte(trimmed), &single); err == nil {
		single = strings.TrimSpace(single)
		if single == "" {
			return nil, nil
		}
		return []string{single}, nil
	}

	return nil, fmt.Errorf("unsupported json value type")
}

func normalizeNonEmptyStrings(values []string) []string {
	unique := map[string]struct{}{}
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		unique[trimmed] = struct{}{}
	}
	if len(unique) == 0 {
		return nil
	}

	out := make([]string, 0, len(unique))
	for value := range unique {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func removeLikelyTunnelDefaultRoutes(ctx context.Context, tunnelNames []string) ([]string, error) {
	if len(normalizeTunnelNames(tunnelNames)) == 0 {
		return nil, nil
	}

	script := buildTunnelDefaultRouteCleanupScript(tunnelNames)
	output, err := runHiddenCommand(ctx, "powershell.exe", psArgs(script)...)
	if err != nil {
		return nil, fmt.Errorf("powershell cleanup command failed: %w (%s)", err, strings.TrimSpace(output))
	}

	trimmed := strings.TrimSpace(output)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return nil, nil
	}

	var routes []netRouteRecord
	if unmarshalErr := json.Unmarshal([]byte(trimmed), &routes); unmarshalErr != nil {
		return nil, fmt.Errorf("parse cleanup output failed: %w (%s)", unmarshalErr, trimmed)
	}
	if len(routes) == 0 {
		return nil, nil
	}

	removed := make([]string, 0, len(routes))
	for _, route := range routes {
		alias := strings.TrimSpace(route.InterfaceAlias)
		if alias == "" {
			alias = "unknown"
		}
		nextHop := strings.TrimSpace(route.NextHop)
		if nextHop == "" {
			nextHop = "n/a"
		}
		removed = append(removed, fmt.Sprintf("%s#%d:%s", alias, route.InterfaceIndex, nextHop))
	}
	return removed, nil
}

// targetsAssignment renders the tunnel-name exclusion set shared by every
// generated PowerShell script; names are compared with -eq, never wildcards.
func targetsAssignment(tunnelNames []string) string {
	targets := normalizeTunnelNames(tunnelNames)
	quoted := make([]string, 0, len(targets))
	for _, name := range targets {
		quoted = append(quoted, fmt.Sprintf("'%s'", psSingleQuote(name)))
	}
	if len(quoted) == 0 {
		return "$targets=@()"
	}
	return "$targets=@(" + strings.Join(quoted, ", ") + ")"
}

func buildTunnelDefaultRouteCleanupScript(tunnelNames []string) string {
	parts := []string{
		"$ErrorActionPreference='SilentlyContinue'",
		targetsAssignment(tunnelNames),
		"$families=@(@{Family='IPv4';Prefix='0.0.0.0/0'},@{Family='IPv6';Prefix='::/0'})",
		"$removed=@()",
		"foreach ($fam in $families) {",
		"  $routes=Get-NetRoute -AddressFamily $fam.Family -DestinationPrefix $fam.Prefix -ErrorAction SilentlyContinue | Where-Object {",
		"    $alias=[string]$_.InterfaceAlias",
		"    if ([string]::IsNullOrWhiteSpace($alias)) { return $false }",
		"    $aliasLower=$alias.ToLowerInvariant()",
		"    foreach ($target in $targets) { if ($aliasLower -eq $target) { return $true } }",
		"    return $false",
		"  }",
		"  foreach ($route in $routes) {",
		"    Remove-NetRoute -AddressFamily $fam.Family -DestinationPrefix $route.DestinationPrefix -InterfaceIndex $route.InterfaceIndex -NextHop $route.NextHop -Confirm:$false -ErrorAction SilentlyContinue | Out-Null",
		"    $stillPresent=Get-NetRoute -AddressFamily $fam.Family -DestinationPrefix $route.DestinationPrefix -InterfaceIndex $route.InterfaceIndex -NextHop $route.NextHop -ErrorAction SilentlyContinue",
		"    if (-not $stillPresent) {",
		"      $removed += [pscustomobject]@{DestinationPrefix=[string]$route.DestinationPrefix;InterfaceAlias=[string]$route.InterfaceAlias;InterfaceIndex=[int]$route.InterfaceIndex;NextHop=[string]$route.NextHop}",
		"    }",
		"  }",
		"}",
		"@($removed) | ConvertTo-Json -Compress",
	}

	return strings.Join(parts, "; ")
}

// buildTunnelDefaultRoutePresenceScript checks whether any tunnel-owned
// default route (IPv4 or IPv6) is still present after cleanup.
func buildTunnelDefaultRoutePresenceScript(tunnelNames []string) string {
	parts := []string{
		"$ErrorActionPreference='SilentlyContinue'",
		targetsAssignment(tunnelNames),
		"$families=@(@{Family='IPv4';Prefix='0.0.0.0/0'},@{Family='IPv6';Prefix='::/0'})",
		"$found=$false",
		"foreach ($fam in $families) {",
		"  $routes=Get-NetRoute -AddressFamily $fam.Family -DestinationPrefix $fam.Prefix -ErrorAction SilentlyContinue",
		"  foreach ($route in $routes) {",
		"    $alias=[string]$route.InterfaceAlias",
		"    if ([string]::IsNullOrWhiteSpace($alias)) { continue }",
		"    $aliasLower=$alias.ToLowerInvariant()",
		"    foreach ($target in $targets) { if ($aliasLower -eq $target) { $found=$true } }",
		"  }",
		"}",
		"if ($found) { 'true' } else { 'false' }",
	}

	return strings.Join(parts, "; ")
}

// buildResolveDefaultGatewayScript finds the lowest-metric IPv4 default
// route whose interface is not one of the excluded tunnel aliases.
func buildResolveDefaultGatewayScript(tunnelNames []string) string {
	parts := []string{
		"$ErrorActionPreference='SilentlyContinue'",
		targetsAssignment(tunnelNames),
		"$candidates=Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object {",
		"  $alias=[string]$_.InterfaceAlias",
		"  $nextHop=[string]$_.NextHop",
		"  if ([string]::IsNullOrWhiteSpace($alias) -or [string]::IsNullOrWhiteSpace($nextHop) -or $nextHop -eq '0.0.0.0') { return $false }",
		"  $aliasLower=$alias.ToLowerInvariant()",
		"  foreach ($target in $targets) { if ($aliasLower -eq $target) { return $false } }",
		"  return $true",
		"}",
		"$best=$candidates | Sort-Object { [int]$_.RouteMetric + [int]$_.InterfaceMetric } | Select-Object -First 1",
		"if ($null -eq $best) { '' } else { [pscustomobject]@{Gateway=[string]$best.NextHop;InterfaceAlias=[string]$best.InterfaceAlias;InterfaceIndex=[int]$best.InterfaceIndex;Metric=([int]$best.RouteMetric + [int]$best.InterfaceMetric)} | ConvertTo-Json -Compress }",
	}

	return strings.Join(parts, "; ")
}

func buildAdapterRenewScript(tunnelNames []string) string {
	parts := []string{
		"$ErrorActionPreference='SilentlyContinue'",
		targetsAssignment(tunnelNames),
		"$ipconfigExe=Join-Path $env:SystemRoot 'System32\\ipconfig.exe'",
		"$adapters=Get-NetAdapter -Physical -ErrorAction SilentlyContinue | Where-Object {",
		"  $_.Status -eq 'Up' -and $_.HardwareInterface -eq $true",
		"}",
		"$renewed=@()",
		"foreach ($adapter in $adapters) {",
		"  $alias=[string]$adapter.Name",
		"  if ([string]::IsNullOrWhiteSpace($alias)) { continue }",
		"  $aliasLower=$alias.ToLowerInvariant()",
		"  if ($aliasLower -like 'wireguard*' -or $aliasLower -like 'wintun*' -or $aliasLower -like '*loopback*') { continue }",
		"  $skip=$false",
		"  foreach ($target in $targets) { if ($aliasLower -eq $target) { $skip=$true; break } }",
		"  if ($skip) { continue }",
		"  & $ipconfigExe /renew \"$alias\" | Out-Null",
		"  $renewed += [string]$alias",
		"}",
		"@($renewed | Select-Object -Unique) | ConvertTo-Json -Compress",
	}

	return strings.Join(parts, "; ")
}

func normalizeTunnelNames(names []string) []string {
	unique := map[string]struct{}{}
	for _, name := range names {
		trimmed := strings.TrimSpace(strings.ToLower(name))
		if trimmed == "" {
			continue
		}
		unique[trimmed] = struct{}{}
	}

	out := make([]string, 0, len(unique))
	for name := range unique {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func psSingleQuote(value string) string {
	return strings.ReplaceAll(value, "'", "''")
}

// psArgs returns the standard non-interactive powershell.exe invocation args for script.
func psArgs(script string) []string {
	return []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", script}
}

// resolveDefaultGateway returns the active IPv4 default gateway, excluding
// any route owned by a tunnel interface.
func resolveDefaultGateway(ctx context.Context, tunnelNames []string) (gateway string, metric string, err error) {
	script := buildResolveDefaultGatewayScript(tunnelNames)
	output, cmdErr := runHiddenCommand(ctx, "powershell.exe", psArgs(script)...)
	if cmdErr != nil {
		return "", "", fmt.Errorf("query default gateway failed: %w (%s)", cmdErr, strings.TrimSpace(output))
	}

	trimmed := strings.TrimSpace(output)
	if trimmed == "" || strings.EqualFold(trimmed, "null") {
		return "", "", fmt.Errorf("no usable ipv4 default gateway found")
	}

	var best defaultGatewayRecord
	if unmarshalErr := json.Unmarshal([]byte(trimmed), &best); unmarshalErr != nil {
		return "", "", fmt.Errorf("parse default gateway output failed: %w (%s)", unmarshalErr, trimmed)
	}
	if best.Gateway == "" {
		return "", "", fmt.Errorf("no usable ipv4 default gateway found")
	}

	return best.Gateway, strconv.Itoa(best.Metric), nil
}

// hasStaleTunnelDefaultRoute reports whether a tunnel-owned default route
// (IPv4 or IPv6) is still present on the system.
func hasStaleTunnelDefaultRoute(ctx context.Context, tunnelNames []string) (bool, error) {
	if len(normalizeTunnelNames(tunnelNames)) == 0 {
		return false, nil
	}

	script := buildTunnelDefaultRoutePresenceScript(tunnelNames)
	output, err := runHiddenCommand(ctx, "powershell.exe", psArgs(script)...)
	if err != nil {
		return false, fmt.Errorf("powershell route presence check failed: %w (%s)", err, strings.TrimSpace(output))
	}

	return strings.EqualFold(strings.TrimSpace(output), "true"), nil
}

func runHiddenCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, resolveSystemCommand(command), args...)
	ConfigureBackgroundProcess(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	return combined, err
}
