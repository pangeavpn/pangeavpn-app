//go:build windows

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var defaultRouteLine = regexp.MustCompile(`(?m)^\s*0\.0\.0\.0\s+0\.0\.0\.0\s+(\d+\.\d+\.\d+\.\d+)\s+(\d+\.\d+\.\d+\.\d+)\s+(\d+)\s*$`)

type netRouteRecord struct {
	DestinationPrefix string
	InterfaceAlias    string
	InterfaceIndex    int
	NextHop           string
}

// RepairNetworkAfterTunnelDisconnect performs targeted cleanup for stale tunnel networking state.
func RepairNetworkAfterTunnelDisconnect(ctx context.Context, tunnelNames []string) ([]string, error) {
	actions := make([]string, 0, 7)

	removedRoutes, removeErr := removeLikelyTunnelDefaultRoutes(ctx, tunnelNames)
	if removeErr != nil {
		actions = append(actions, fmt.Sprintf("warning: stale route cleanup failed: %v", removeErr))
	} else if len(removedRoutes) > 0 {
		actions = append(actions, fmt.Sprintf("removed stale tunnel default routes: %s", strings.Join(removedRoutes, ", ")))
	}

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

	if _, _, routeErr := resolveDefaultGateway(ctx); routeErr == nil {
		return actions, nil
	}

	renewedAdapters, renewAdaptersErr := renewLikelyPrimaryAdapters(ctx, tunnelNames)
	if renewAdaptersErr != nil {
		actions = append(actions, fmt.Sprintf("warning: adapter renew failed: %v", renewAdaptersErr))
	} else if len(renewedAdapters) > 0 {
		actions = append(actions, fmt.Sprintf("renewed active adapters after missing default route: %s", strings.Join(renewedAdapters, ", ")))
	}

	if _, _, verifyAfterAdapterRenewErr := resolveDefaultGateway(ctx); verifyAfterAdapterRenewErr == nil {
		actions = append(actions, "restored default route via targeted adapter renew")
		return actions, nil
	}

	renewOutput, renewErr := runHiddenCommand(ctx, "ipconfig", "/renew")
	if renewErr != nil {
		return actions, fmt.Errorf("default route missing after disconnect and network renew failed: %w (%s)", renewErr, strings.TrimSpace(renewOutput))
	}

	if _, _, verifyErr := resolveDefaultGateway(ctx); verifyErr != nil {
		return actions, fmt.Errorf("default route still missing after network renew: %w", verifyErr)
	}

	actions = append(actions, "restored default route via network renew")
	return actions, nil
}

func renewLikelyPrimaryAdapters(ctx context.Context, tunnelNames []string) ([]string, error) {
	script := buildAdapterRenewScript(tunnelNames)
	output, err := runHiddenCommand(
		ctx,
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		script,
	)
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
	output, err := runHiddenCommand(
		ctx,
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-ExecutionPolicy",
		"Bypass",
		"-Command",
		script,
	)
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

func buildTunnelDefaultRouteCleanupScript(tunnelNames []string) string {
	targets := normalizeTunnelNames(tunnelNames)
	quotedTargets := make([]string, 0, len(targets))
	for _, name := range targets {
		quotedTargets = append(quotedTargets, fmt.Sprintf("'%s'", psSingleQuote(name)))
	}

	targetArray := "@()"
	if len(quotedTargets) > 0 {
		targetArray = "@(" + strings.Join(quotedTargets, ", ") + ")"
	}

	parts := []string{
		"$ErrorActionPreference='SilentlyContinue'",
		"$targets=" + targetArray,
		"$routes=Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction SilentlyContinue | Where-Object {",
		"  $alias=[string]$_.InterfaceAlias",
		"  if ([string]::IsNullOrWhiteSpace($alias)) { return $false }",
		"  $aliasLower=$alias.ToLowerInvariant()",
		"  foreach ($target in $targets) { if ($aliasLower -eq $target -or $aliasLower -like ('*' + $target + '*')) { return $true } }",
		"  return $false",
		"}",
		"$removed=@()",
		"foreach ($route in $routes) {",
		"  Remove-NetRoute -AddressFamily IPv4 -DestinationPrefix $route.DestinationPrefix -InterfaceIndex $route.InterfaceIndex -NextHop $route.NextHop -Confirm:$false -ErrorAction SilentlyContinue | Out-Null",
		"  $removed += [pscustomobject]@{DestinationPrefix=[string]$route.DestinationPrefix;InterfaceAlias=[string]$route.InterfaceAlias;InterfaceIndex=[int]$route.InterfaceIndex;NextHop=[string]$route.NextHop}",
		"}",
		"$removed | ConvertTo-Json -Compress",
	}

	return strings.Join(parts, "; ")
}

func buildAdapterRenewScript(tunnelNames []string) string {
	targets := normalizeTunnelNames(tunnelNames)
	quotedTargets := make([]string, 0, len(targets))
	for _, name := range targets {
		quotedTargets = append(quotedTargets, fmt.Sprintf("'%s'", psSingleQuote(name)))
	}

	targetArray := "@()"
	if len(quotedTargets) > 0 {
		targetArray = "@(" + strings.Join(quotedTargets, ", ") + ")"
	}

	parts := []string{
		"$ErrorActionPreference='SilentlyContinue'",
		"$targets=" + targetArray,
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
		"  foreach ($target in $targets) { if ($aliasLower -eq $target -or $aliasLower -like ('*' + $target + '*')) { $skip=$true; break } }",
		"  if ($skip) { continue }",
		"  ipconfig /renew \"$alias\" | Out-Null",
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

func resolveDefaultGateway(ctx context.Context) (gateway string, metric string, err error) {
	output, cmdErr := runHiddenCommand(ctx, "route", "print", "-4")
	if cmdErr != nil {
		return "", "", fmt.Errorf("query default gateway failed: %w (%s)", cmdErr, strings.TrimSpace(output))
	}

	matches := defaultRouteLine.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return "", "", fmt.Errorf("no ipv4 default route found")
	}

	bestMetric := int(^uint(0) >> 1)
	bestGateway := ""
	for _, match := range matches {
		if len(match) < 4 {
			continue
		}

		candidateGateway := match[1]
		candidateIface := match[2]
		candidateMetric := match[3]

		if candidateGateway == "0.0.0.0" || strings.HasPrefix(candidateIface, "127.") {
			continue
		}

		metricValue, convErr := strconv.Atoi(candidateMetric)
		if convErr != nil {
			continue
		}
		if metricValue < bestMetric {
			bestMetric = metricValue
			bestGateway = candidateGateway
		}
	}

	if bestGateway == "" {
		return "", "", fmt.Errorf("no usable ipv4 default gateway found")
	}

	return bestGateway, strconv.Itoa(bestMetric), nil
}

func runHiddenCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	ConfigureBackgroundProcess(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	return combined, err
}
