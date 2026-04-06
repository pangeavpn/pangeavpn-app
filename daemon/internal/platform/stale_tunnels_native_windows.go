//go:build windows

package platform

import (
	"fmt"
	"net/netip"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
	"golang.zx2c4.com/wintun"
)

var numberedAdapterPattern = regexp.MustCompile(`(?i)^(.+?)\s+\d+$`)

// CleanupStaleTunnelArtifactsNative removes stale WireGuard/Wintun adapter
// artifacts using native Windows APIs (no PowerShell). It flushes networking
// state from adapters that match the provided tunnel names but are not in the
// activeLUIDs set, then cleans up matching network profile registry entries.
func CleanupStaleTunnelArtifactsNative(tunnelNames []string, activeLUIDs map[uint64]struct{}) ([]string, error) {
	totalStart := time.Now()

	targets := normalizeTunnelNames(tunnelNames)
	if len(targets) == 0 {
		return nil, nil
	}

	targetSet := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		targetSet[t] = struct{}{}
	}

	enumStart := time.Now()
	ifaces, err := winipcfg.GetIfTable2Ex(winipcfg.MibIfEntryNormalWithoutStatistics)
	if err != nil {
		return nil, fmt.Errorf("enumerate interfaces: %w", err)
	}
	enumDuration := time.Since(enumStart)

	var actions []string
	var staleCount int
	flushStart := time.Now()

	for i := range ifaces {
		iface := &ifaces[i]
		desc := strings.ToLower(strings.TrimSpace(iface.Description()))
		if !strings.Contains(desc, "wintun") && !strings.Contains(desc, "wireguard") {
			continue
		}

		alias := strings.TrimSpace(iface.Alias())
		if !matchesTunnelTarget(alias, targetSet) {
			continue
		}

		luid := uint64(iface.InterfaceLUID)
		if _, active := activeLUIDs[luid]; active {
			continue
		}

		staleCount++
		actions = append(actions, flushStaleLUID(winipcfg.LUID(luid), alias)...)
		actions = append(actions, closeWintunAdapter(alias)...)
	}
	flushDuration := time.Since(flushStart)

	registryStart := time.Now()
	actions = append(actions, cleanupNetworkProfiles(targetSet)...)
	registryDuration := time.Since(registryStart)

	actions = append(actions, fmt.Sprintf(
		"timing: total=%dms enumerate=%dms flush=%dms(%d stale) registry=%dms",
		time.Since(totalStart).Milliseconds(),
		enumDuration.Milliseconds(),
		flushDuration.Milliseconds(),
		staleCount,
		registryDuration.Milliseconds(),
	))

	return actions, nil
}

// matchesTunnelTarget returns true if the adapter alias matches one of the
// target tunnel names exactly, or is a numbered variant like "name 12".
func matchesTunnelTarget(alias string, targetSet map[string]struct{}) bool {
	normalized := strings.ToLower(strings.TrimSpace(alias))
	if _, ok := targetSet[normalized]; ok {
		return true
	}
	if m := numberedAdapterPattern.FindStringSubmatch(normalized); m != nil {
		if _, ok := targetSet[m[1]]; ok {
			return true
		}
	}
	return false
}

// flushStaleLUID removes routes, IP addresses, and DNS from a stale adapter.
func flushStaleLUID(luid winipcfg.LUID, alias string) []string {
	var actions []string
	v4 := winipcfg.AddressFamily(windows.AF_INET)
	v6 := winipcfg.AddressFamily(windows.AF_INET6)

	if err := luid.FlushRoutes(v4); err != nil && err != windows.ERROR_NOT_FOUND {
		// best-effort
	} else {
		actions = append(actions, fmt.Sprintf("flushed IPv4 routes on %s", alias))
	}
	if err := luid.FlushRoutes(v6); err != nil && err != windows.ERROR_NOT_FOUND {
		// best-effort
	}

	if err := luid.FlushIPAddresses(v4); err != nil && err != windows.ERROR_NOT_FOUND {
		// best-effort
	} else {
		actions = append(actions, fmt.Sprintf("flushed IPv4 addresses on %s", alias))
	}
	if err := luid.FlushIPAddresses(v6); err != nil && err != windows.ERROR_NOT_FOUND {
		// best-effort
	}

	if err := luid.FlushDNS(v4); err != nil && err != windows.ERROR_NOT_FOUND {
		// best-effort
	}
	if err := luid.FlushDNS(v6); err != nil && err != windows.ERROR_NOT_FOUND {
		// best-effort
	}

	return actions
}

// removeStaleTunnelDefaultRoutes removes 0.0.0.0/0 routes from stale tunnel
// adapters using native APIs.
func removeStaleTunnelDefaultRoutes(tunnelNames []string, activeLUIDs map[uint64]struct{}) []string {
	targets := normalizeTunnelNames(tunnelNames)
	if len(targets) == 0 {
		return nil
	}

	targetSet := make(map[string]struct{}, len(targets))
	for _, t := range targets {
		targetSet[t] = struct{}{}
	}

	defaultDst := netip.MustParsePrefix("0.0.0.0/0")
	v4 := winipcfg.AddressFamily(windows.AF_INET)

	routes, err := winipcfg.GetIPForwardTable2(v4)
	if err != nil {
		return nil
	}

	// Build LUID→alias map for stale tunnel adapters.
	staleLUIDs := make(map[winipcfg.LUID]string)
	ifaces, err := winipcfg.GetIfTable2Ex(winipcfg.MibIfEntryNormalWithoutStatistics)
	if err != nil {
		return nil
	}
	for i := range ifaces {
		iface := &ifaces[i]
		desc := strings.ToLower(strings.TrimSpace(iface.Description()))
		if !strings.Contains(desc, "wintun") && !strings.Contains(desc, "wireguard") {
			continue
		}
		alias := strings.TrimSpace(iface.Alias())
		if !matchesTunnelTarget(alias, targetSet) {
			continue
		}
		luid := winipcfg.LUID(iface.InterfaceLUID)
		if _, active := activeLUIDs[uint64(luid)]; active {
			continue
		}
		staleLUIDs[luid] = alias
	}

	var actions []string
	for _, route := range routes {
		dstPrefix := route.DestinationPrefix.Prefix()
		if dstPrefix != defaultDst {
			continue
		}
		alias, isStale := staleLUIDs[route.InterfaceLUID]
		if !isStale {
			continue
		}
		nextHop := route.NextHop.Addr()
		if err := route.InterfaceLUID.DeleteRoute(defaultDst, nextHop); err == nil {
			actions = append(actions, fmt.Sprintf("removed default route on %s", alias))
		}
	}

	return actions
}

// closeWintunAdapter tries to open and close a wintun adapter by name,
// releasing the handle so Windows can clean up the adapter.
func closeWintunAdapter(name string) []string {
	adapter, err := wintun.OpenAdapter(name)
	if err != nil {
		return nil
	}
	if err := adapter.Close(); err != nil {
		return nil
	}
	return []string{fmt.Sprintf("closed wintun adapter %s", name)}
}

const (
	networkProfilesPath   = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkList\Profiles`
	managedSignaturesPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkList\Signatures\Managed`
	unmanagedSignaturesPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkList\Signatures\Unmanaged`
)

// cleanupNetworkProfiles removes Windows network profile and signature
// registry entries that match the tunnel names.
func cleanupNetworkProfiles(targetSet map[string]struct{}) []string {
	var actions []string
	var removedGUIDs []string

	profilesKey, err := registry.OpenKey(registry.LOCAL_MACHINE, networkProfilesPath, registry.READ)
	if err != nil {
		return nil
	}
	defer profilesKey.Close()

	subkeys, err := profilesKey.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	for _, guid := range subkeys {
		subKeyPath := networkProfilesPath + `\` + guid
		subKey, err := registry.OpenKey(registry.LOCAL_MACHINE, subKeyPath, registry.READ)
		if err != nil {
			continue
		}

		profileName, _, err := subKey.GetStringValue("ProfileName")
		subKey.Close()
		if err != nil {
			continue
		}

		if !matchesTunnelTarget(profileName, targetSet) {
			continue
		}

		if err := registry.DeleteKey(registry.LOCAL_MACHINE, subKeyPath); err == nil {
			actions = append(actions, fmt.Sprintf("removed network profile %s", profileName))
			removedGUIDs = append(removedGUIDs, strings.Trim(strings.ToLower(guid), "{}"))
		}
	}

	if len(removedGUIDs) > 0 {
		for _, sigRoot := range []string{managedSignaturesPath, unmanagedSignaturesPath} {
			actions = append(actions, cleanupSignatures(sigRoot, removedGUIDs)...)
		}
	}

	return actions
}

// cleanupSignatures removes network signature entries whose ProfileGuid
// matches one of the removed profile GUIDs.
func cleanupSignatures(sigRoot string, removedGUIDs []string) []string {
	sigKey, err := registry.OpenKey(registry.LOCAL_MACHINE, sigRoot, registry.READ)
	if err != nil {
		return nil
	}
	defer sigKey.Close()

	subkeys, err := sigKey.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	removedSet := make(map[string]struct{}, len(removedGUIDs))
	for _, g := range removedGUIDs {
		removedSet[g] = struct{}{}
	}

	var actions []string
	for _, name := range subkeys {
		subKeyPath := sigRoot + `\` + name
		subKey, err := registry.OpenKey(registry.LOCAL_MACHINE, subKeyPath, registry.READ)
		if err != nil {
			continue
		}

		profileGuid, _, err := subKey.GetStringValue("ProfileGuid")
		subKey.Close()
		if err != nil {
			continue
		}

		normalized := strings.Trim(strings.ToLower(strings.TrimSpace(profileGuid)), "{}")
		if _, match := removedSet[normalized]; !match {
			continue
		}

		if err := registry.DeleteKey(registry.LOCAL_MACHINE, subKeyPath); err == nil {
			actions = append(actions, fmt.Sprintf("removed network signature %s", name))
		}
	}

	return actions
}
