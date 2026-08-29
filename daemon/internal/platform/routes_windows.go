//go:build windows

package platform

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

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

// removeLikelyTunnelDefaultRoutes deletes every /0 route owned by one of the
// named tunnel interfaces, via GetIpForwardTable2/DeleteIpForwardEntry2 rather
// than Get-NetRoute/Remove-NetRoute — no PowerShell process on the disconnect
// repair path.
func removeLikelyTunnelDefaultRoutes(_ context.Context, tunnelNames []string) ([]string, error) {
	set := tunnelNameSet(tunnelNames)
	if len(set) == 0 {
		return nil, nil
	}
	var removed []string
	var failures []string
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		rows, err := defaultRouteRows(family)
		if err != nil {
			return removed, fmt.Errorf("read routing table: %w", err)
		}
		for i := range rows {
			row := &rows[i]
			alias := routeInterfaceAlias(row)
			if alias == "" || !set[alias] {
				continue
			}
			if err := row.Delete(); err != nil && !errors.Is(err, windows.ERROR_NOT_FOUND) {
				failures = append(failures, fmt.Sprintf("%s: %v", alias, err))
				continue
			}
			nextHop := "n/a"
			if nh := row.NextHop.Addr(); nh.IsValid() {
				nextHop = nh.String()
			}
			removed = append(removed, fmt.Sprintf("%s#%d:%s", alias, row.InterfaceIndex, nextHop))
		}
	}
	if len(failures) > 0 {
		return removed, fmt.Errorf("remove tunnel default routes: %s", strings.Join(failures, "; "))
	}
	return removed, nil
}

// tunnelNameSet lowercases and de-dupes the tunnel names into a lookup set.
func tunnelNameSet(tunnelNames []string) map[string]bool {
	names := normalizeTunnelNames(tunnelNames)
	set := make(map[string]bool, len(names))
	for _, name := range names {
		set[name] = true
	}
	return set
}

// defaultRouteRows returns every default (/0) route for the family.
func defaultRouteRows(family winipcfg.AddressFamily) ([]winipcfg.MibIPforwardRow2, error) {
	table, err := winipcfg.GetIPForwardTable2(family)
	if err != nil {
		return nil, err
	}
	out := make([]winipcfg.MibIPforwardRow2, 0, 4)
	for i := range table {
		prefix := table[i].DestinationPrefix.Prefix()
		if prefix.IsValid() && prefix.Bits() == 0 {
			out = append(out, table[i])
		}
	}
	return out, nil
}

// routeInterfaceAlias is the lowercased interface alias owning a route, "" if
// it can't be resolved.
func routeInterfaceAlias(row *winipcfg.MibIPforwardRow2) string {
	iface, err := row.InterfaceLUID.Interface()
	if err != nil {
		return ""
	}
	return strings.ToLower(iface.Alias())
}

// defaultRouteInfo is the slice of a default route the gateway election needs,
// decoupled from winipcfg so the selection logic is testable.
type defaultRouteInfo struct {
	aliasLower string
	nextHop    netip.Addr
	metric     uint64
}

// selectDefaultGateway picks the lowest-metric default route whose interface is
// not a tunnel and whose next hop is a real gateway.
func selectDefaultGateway(rows []defaultRouteInfo, tunnelSet map[string]bool) (gateway string, metric int, ok bool) {
	best := -1
	for i := range rows {
		r := rows[i]
		if r.aliasLower == "" || tunnelSet[r.aliasLower] {
			continue
		}
		if !r.nextHop.IsValid() || r.nextHop.IsUnspecified() || r.nextHop.IsLoopback() || r.nextHop.IsMulticast() {
			continue
		}
		if best == -1 || r.metric < rows[best].metric {
			best = i
		}
	}
	if best == -1 {
		return "", 0, false
	}
	return rows[best].nextHop.String(), int(rows[best].metric), true
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

// resolveDefaultGateway returns the active IPv4 default gateway, excluding any
// route owned by a tunnel interface. Native GetIpForwardTable2 read — no
// PowerShell.
func resolveDefaultGateway(_ context.Context, tunnelNames []string) (gateway string, metric string, err error) {
	set := tunnelNameSet(tunnelNames)
	rows, err := defaultRouteRows(windows.AF_INET)
	if err != nil {
		return "", "", fmt.Errorf("read ipv4 routing table: %w", err)
	}
	infos := make([]defaultRouteInfo, 0, len(rows))
	for i := range rows {
		row := &rows[i]
		m := uint64(row.Metric)
		if ipif, e := row.InterfaceLUID.IPInterface(windows.AF_INET); e == nil {
			m += uint64(ipif.Metric)
		}
		infos = append(infos, defaultRouteInfo{
			aliasLower: routeInterfaceAlias(row),
			nextHop:    row.NextHop.Addr(),
			metric:     m,
		})
	}
	gw, met, ok := selectDefaultGateway(infos, set)
	if !ok {
		return "", "", fmt.Errorf("no usable ipv4 default gateway found")
	}
	return gw, strconv.Itoa(met), nil
}

// hasStaleTunnelDefaultRoute reports whether a tunnel-owned default route
// (IPv4 or IPv6) is still present. Native read — no PowerShell.
func hasStaleTunnelDefaultRoute(_ context.Context, tunnelNames []string) (bool, error) {
	set := tunnelNameSet(tunnelNames)
	if len(set) == 0 {
		return false, nil
	}
	for _, family := range []winipcfg.AddressFamily{windows.AF_INET, windows.AF_INET6} {
		rows, err := defaultRouteRows(family)
		if err != nil {
			return false, fmt.Errorf("read routing table: %w", err)
		}
		for i := range rows {
			alias := routeInterfaceAlias(&rows[i])
			if alias != "" && set[alias] {
				return true, nil
			}
		}
	}
	return false, nil
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
