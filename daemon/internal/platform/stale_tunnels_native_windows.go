//go:build windows

package platform

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"golang.zx2c4.com/wintun"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

var numberedAdapterPattern = regexp.MustCompile(`(?i)^(.+?)\s+\d+$`)

// deviceClassNetGUID is GUID_DEVCLASS_NET, used to enumerate network device
// nodes when removing a stale wintun adapter's PnP device node directly.
var deviceClassNetGUID = windows.GUID{
	Data1: 0x4d36e972,
	Data2: 0xe325,
	Data3: 0x11ce,
	Data4: [8]byte{0xbf, 0xc1, 0x08, 0x00, 0x2b, 0xe1, 0x03, 0x18},
}

// CleanupStaleTunnelArtifactsNative removes stale WireGuard/Wintun adapter
// artifacts using native Windows APIs (no PowerShell). It only acts on a
// tunnel target when Windows already shows a duplicate/numbered adapter for
// it, flushes and removes the adapters that are not currently active, and
// then removes the matching network profile registry entries.
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

	groups := make(map[string][]*winipcfg.MibIfRow2)
	for i := range ifaces {
		iface := &ifaces[i]
		desc := strings.ToLower(strings.TrimSpace(iface.Description()))
		if !strings.Contains(desc, "wintun") && !strings.Contains(desc, "wireguard") {
			continue
		}

		alias := strings.TrimSpace(iface.Alias())
		target, ok := matchTunnelTarget(alias, targetSet)
		if !ok {
			continue
		}

		groups[target] = append(groups[target], iface)
	}

	var actions []string
	var errs []error
	var staleCount int
	staleAliases := make(map[string]struct{})
	flushStart := time.Now()

	for target, group := range groups {
		aliases := make([]string, len(group))
		for i, iface := range group {
			aliases[i] = strings.ToLower(strings.TrimSpace(iface.Alias()))
		}
		if !hasDuplicateAdapter(target, aliases) {
			continue
		}

		for _, iface := range group {
			alias := strings.TrimSpace(iface.Alias())
			luid := uint64(iface.InterfaceLUID)

			if _, active := activeLUIDs[luid]; active {
				continue
			}
			if iface.OperStatus == winipcfg.IfOperStatusUp {
				continue
			}

			staleCount++
			flushActions, flushErrs := flushStaleLUID(winipcfg.LUID(luid), alias)
			actions = append(actions, flushActions...)
			errs = append(errs, flushErrs...)

			removeActions, removeErr := removeStaleAdapter(alias, iface.InterfaceGUID)
			actions = append(actions, removeActions...)
			if removeErr != nil {
				errs = append(errs, removeErr)
			} else {
				staleAliases[strings.ToLower(alias)] = struct{}{}
			}
		}
	}
	flushDuration := time.Since(flushStart)

	registryStart := time.Now()
	if len(staleAliases) > 0 {
		profileActions, profileErrs := cleanupNetworkProfiles(staleAliases)
		actions = append(actions, profileActions...)
		errs = append(errs, profileErrs...)
	}
	registryDuration := time.Since(registryStart)

	actions = append(actions, fmt.Sprintf(
		"timing: total=%dms enumerate=%dms flush=%dms(%d stale) registry=%dms",
		time.Since(totalStart).Milliseconds(),
		enumDuration.Milliseconds(),
		flushDuration.Milliseconds(),
		staleCount,
		registryDuration.Milliseconds(),
	))

	return actions, errors.Join(errs...)
}

// matchTunnelTarget reports whether alias matches one of the target tunnel
// names exactly, or is a numbered variant like "name 12", and returns the
// base target name it matched.
func matchTunnelTarget(alias string, targetSet map[string]struct{}) (string, bool) {
	normalized := strings.ToLower(strings.TrimSpace(alias))
	if _, ok := targetSet[normalized]; ok {
		return normalized, true
	}
	if m := numberedAdapterPattern.FindStringSubmatch(normalized); m != nil {
		if _, ok := targetSet[m[1]]; ok {
			return m[1], true
		}
	}
	return "", false
}

// hasDuplicateAdapter reports whether the group for target actually shows a
// duplicate: more than one matching adapter, or a numbered alias like
// "name 12". A single, non-numbered adapter is the healthy case and is left
// alone.
func hasDuplicateAdapter(target string, aliases []string) bool {
	if len(aliases) > 1 {
		return true
	}
	for _, alias := range aliases {
		if m := numberedAdapterPattern.FindStringSubmatch(alias); m != nil && m[1] == target {
			return true
		}
	}
	return false
}

// flushStaleLUID removes routes, IP addresses, and DNS from a stale adapter,
// treating "already gone" as success and reporting real failures.
func flushStaleLUID(luid winipcfg.LUID, alias string) ([]string, []error) {
	var actions []string
	var errs []error
	v4 := winipcfg.AddressFamily(windows.AF_INET)
	v6 := winipcfg.AddressFamily(windows.AF_INET6)

	flush := func(label string, family winipcfg.AddressFamily, fn func(winipcfg.AddressFamily) error) {
		err := fn(family)
		switch {
		case err == nil:
			actions = append(actions, fmt.Sprintf("flushed %s on %s", label, alias))
		case errors.Is(err, windows.ERROR_NOT_FOUND):
			// already gone, nothing to report
		default:
			errs = append(errs, fmt.Errorf("flush %s on %s: %w", label, alias, err))
		}
	}

	flush("IPv4 routes", v4, luid.FlushRoutes)
	flush("IPv6 routes", v6, luid.FlushRoutes)
	flush("IPv4 addresses", v4, luid.FlushIPAddresses)
	flush("IPv6 addresses", v6, luid.FlushIPAddresses)
	flush("IPv4 DNS", v4, luid.FlushDNS)
	flush("IPv6 DNS", v6, luid.FlushDNS)

	return actions, errs
}

// removeStaleAdapter closes the wintun handle and removes the underlying PnP
// device node, then re-checks that the adapter is actually gone before
// reporting success.
func removeStaleAdapter(name string, guid windows.GUID) ([]string, error) {
	adapter, err := wintun.OpenAdapter(name)
	if err != nil {
		return nil, nil
	}

	if closeErr := adapter.Close(); closeErr != nil {
		if closeErr = adapter.Close(); closeErr != nil {
			return nil, fmt.Errorf("close wintun handle for %s: %w", name, closeErr)
		}
	}

	if err := removeWintunDeviceNode(guid); err != nil {
		return nil, fmt.Errorf("remove device node for %s: %w", name, err)
	}

	if again, err := wintun.OpenAdapter(name); err == nil {
		again.Close()
		return nil, fmt.Errorf("wintun adapter %s still present after removal", name)
	}

	return []string{fmt.Sprintf("removed wintun adapter %s", name)}, nil
}

// removeWintunDeviceNode deletes the PnP device node matching guid.
// WintunCloseAdapter only deletes device nodes it created in this process,
// so a leftover adapter needs SetupAPI removal to actually disappear.
func removeWintunDeviceNode(guid windows.GUID) error {
	devInfoSet, err := windows.SetupDiGetClassDevsEx(&deviceClassNetGUID, "", 0, windows.DIGCF_PRESENT, windows.DevInfo(0), "")
	if err != nil {
		return fmt.Errorf("get class devs: %w", err)
	}
	defer devInfoSet.Close()

	target := guid.String()
	for i := 0; ; i++ {
		devInfoData, err := devInfoSet.EnumDeviceInfo(i)
		if err != nil {
			return windows.ERROR_NOT_FOUND
		}

		key, err := devInfoSet.OpenDevRegKey(devInfoData, windows.DICS_FLAG_GLOBAL, 0, windows.DIREG_DRV, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		instanceID, _, valErr := registry.Key(key).GetStringValue("NetCfgInstanceId")
		registry.Key(key).Close()
		if valErr != nil || !strings.EqualFold(instanceID, target) {
			continue
		}

		return devInfoSet.CallClassInstaller(windows.DIF_REMOVE, devInfoData)
	}
}

const (
	networkProfilesPath     = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkList\Profiles`
	managedSignaturesPath   = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkList\Signatures\Managed`
	unmanagedSignaturesPath = `SOFTWARE\Microsoft\Windows NT\CurrentVersion\NetworkList\Signatures\Unmanaged`
)

// cleanupNetworkProfiles removes Windows network profile and signature
// registry entries whose ProfileName exactly matches one of the adapter
// aliases we just confirmed stale, never a still-active adapter's name.
func cleanupNetworkProfiles(staleAliases map[string]struct{}) ([]string, []error) {
	var actions []string
	var errs []error
	var removedGUIDs []string

	profilesKey, err := registry.OpenKey(registry.LOCAL_MACHINE, networkProfilesPath, registry.READ)
	if err != nil {
		if !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, fmt.Errorf("open network profiles key: %w", err))
		}
		return actions, errs
	}

	subkeys, err := profilesKey.ReadSubKeyNames(-1)
	profilesKey.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("read network profiles subkeys: %w", err))
		return actions, errs
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

		if _, ok := staleAliases[strings.ToLower(strings.TrimSpace(profileName))]; !ok {
			continue
		}

		if err := deleteRegistryKeyRecursive(registry.LOCAL_MACHINE, subKeyPath); err != nil {
			errs = append(errs, fmt.Errorf("remove network profile %s: %w", profileName, err))
			continue
		}
		actions = append(actions, fmt.Sprintf("removed network profile %s", profileName))
		removedGUIDs = append(removedGUIDs, strings.Trim(strings.ToLower(guid), "{}"))
	}

	if len(removedGUIDs) > 0 {
		for _, sigRoot := range []string{managedSignaturesPath, unmanagedSignaturesPath} {
			sigActions, sigErrs := cleanupSignatures(sigRoot, removedGUIDs)
			actions = append(actions, sigActions...)
			errs = append(errs, sigErrs...)
		}
	}

	return actions, errs
}

// cleanupSignatures removes network signature entries whose ProfileGuid
// matches one of the removed profile GUIDs.
func cleanupSignatures(sigRoot string, removedGUIDs []string) ([]string, []error) {
	var actions []string
	var errs []error

	sigKey, err := registry.OpenKey(registry.LOCAL_MACHINE, sigRoot, registry.READ)
	if err != nil {
		if !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, fmt.Errorf("open signatures key %s: %w", sigRoot, err))
		}
		return actions, errs
	}

	subkeys, err := sigKey.ReadSubKeyNames(-1)
	sigKey.Close()
	if err != nil {
		errs = append(errs, fmt.Errorf("read signatures subkeys %s: %w", sigRoot, err))
		return actions, errs
	}

	removedSet := make(map[string]struct{}, len(removedGUIDs))
	for _, g := range removedGUIDs {
		removedSet[g] = struct{}{}
	}

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

		if err := deleteRegistryKeyRecursive(registry.LOCAL_MACHINE, subKeyPath); err != nil {
			errs = append(errs, fmt.Errorf("remove network signature %s: %w", name, err))
			continue
		}
		actions = append(actions, fmt.Sprintf("removed network signature %s", name))
	}

	return actions, errs
}

// deleteRegistryKeyRecursive deletes path and all of its subkeys, since
// RegDeleteKey fails with ERROR_ACCESS_DENIED on a key that still has
// children.
func deleteRegistryKeyRecursive(baseKey registry.Key, path string) error {
	key, err := registry.OpenKey(baseKey, path, registry.ENUMERATE_SUB_KEYS|registry.READ)
	if err == nil {
		subkeys, subErr := key.ReadSubKeyNames(-1)
		key.Close()
		if subErr != nil {
			return fmt.Errorf("read subkeys of %s: %w", path, subErr)
		}
		for _, sub := range subkeys {
			if err := deleteRegistryKeyRecursive(baseKey, path+`\`+sub); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("open %s: %w", path, err)
	}

	if err := registry.DeleteKey(baseKey, path); err != nil && !errors.Is(err, registry.ErrNotExist) {
		return err
	}
	return nil
}
