//go:build windows && (amd64 || arm64)

package platform

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// The ABI facts here come from fwpmtypes.h/fwptypes.h via the vendored
// wireguard-windows definitions; these tests pin our build to them.
func TestWFPStructLayoutsMatchSDK(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Fatalf("WFP layouts are the 64-bit ones; pointer size is %d", unsafe.Sizeof(uintptr(0)))
	}
	sizes := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"wtFwpmDisplayData0", unsafe.Sizeof(wtFwpmDisplayData0{}), wtFwpmDisplayData0_Size},
		{"wtFwpByteBlob", unsafe.Sizeof(wtFwpByteBlob{}), wtFwpByteBlob_Size},
		{"wtFwpValue0", unsafe.Sizeof(wtFwpValue0{}), wtFwpValue0_Size},
		{"wtFwpConditionValue0", unsafe.Sizeof(wtFwpConditionValue0{}), wtFwpConditionValue0_Size},
		{"wtFwpmAction0", unsafe.Sizeof(wtFwpmAction0{}), wtFwpmAction0_Size},
		{"wtFwpmSession0", unsafe.Sizeof(wtFwpmSession0{}), wtFwpmSession0_Size},
		{"wtFwpmSublayer0", unsafe.Sizeof(wtFwpmSublayer0{}), wtFwpmSublayer0_Size},
		{"wtFwpmFilterCondition0", unsafe.Sizeof(wtFwpmFilterCondition0{}), wtFwpmFilterCondition0_Size},
		{"wtFwpmFilter0", unsafe.Sizeof(wtFwpmFilter0{}), wtFwpmFilter0_Size},
		{"wtFwpV4AddrAndMask", unsafe.Sizeof(wtFwpV4AddrAndMask{}), wtFwpV4AddrAndMask_Size},
		{"wtFwpV6AddrAndMask", unsafe.Sizeof(wtFwpV6AddrAndMask{}), wtFwpV6AddrAndMask_Size},
	}
	for _, tc := range sizes {
		if tc.got != tc.want {
			t.Errorf("%s: size = %d, want %d", tc.name, tc.got, tc.want)
		}
	}

	var f wtFwpmFilter0
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"displayData", unsafe.Offsetof(f.displayData), wtFwpmFilter0_displayData_Offset},
		{"flags", unsafe.Offsetof(f.flags), wtFwpmFilter0_flags_Offset},
		{"providerKey", unsafe.Offsetof(f.providerKey), wtFwpmFilter0_providerKey_Offset},
		{"providerData", unsafe.Offsetof(f.providerData), wtFwpmFilter0_providerData_Offset},
		{"layerKey", unsafe.Offsetof(f.layerKey), wtFwpmFilter0_layerKey_Offset},
		{"subLayerKey", unsafe.Offsetof(f.subLayerKey), wtFwpmFilter0_subLayerKey_Offset},
		{"weight", unsafe.Offsetof(f.weight), wtFwpmFilter0_weight_Offset},
		{"numFilterConditions", unsafe.Offsetof(f.numFilterConditions), wtFwpmFilter0_numFilterConditions_Offset},
		{"filterCondition", unsafe.Offsetof(f.filterCondition), wtFwpmFilter0_filterCondition_Offset},
		{"action", unsafe.Offsetof(f.action), wtFwpmFilter0_action_Offset},
		{"providerContextKey", unsafe.Offsetof(f.providerContextKey), wtFwpmFilter0_providerContextKey_Offset},
		{"reserved", unsafe.Offsetof(f.reserved), wtFwpmFilter0_reserved_Offset},
		{"filterID", unsafe.Offsetof(f.filterID), wtFwpmFilter0_filterID_Offset},
		{"effectiveWeight", unsafe.Offsetof(f.effectiveWeight), wtFwpmFilter0_effectiveWeight_Offset},
	}
	for _, tc := range offsets {
		if tc.got != tc.want {
			t.Errorf("wtFwpmFilter0.%s: offset = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
	var c wtFwpmFilterCondition0
	if got := unsafe.Offsetof(c.matchType); got != wtFwpmFilterCondition0_matchType_Offset {
		t.Errorf("wtFwpmFilterCondition0.matchType: offset = %d, want %d", got, wtFwpmFilterCondition0_matchType_Offset)
	}
	if got := unsafe.Offsetof(c.conditionValue); got != wtFwpmFilterCondition0_conditionValue_Offset {
		t.Errorf("wtFwpmFilterCondition0.conditionValue: offset = %d, want %d", got, wtFwpmFilterCondition0_conditionValue_Offset)
	}
	var s wtFwpmSublayer0
	if got := unsafe.Offsetof(s.weight); got != wtFwpmSublayer0_weight_Offset {
		t.Errorf("wtFwpmSublayer0.weight: offset = %d, want %d", got, wtFwpmSublayer0_weight_Offset)
	}
	var v6 wtFwpV6AddrAndMask
	if got := unsafe.Offsetof(v6.prefixLength); got != wtFwpV6AddrAndMask_prefixLength_Offset {
		t.Errorf("wtFwpV6AddrAndMask.prefixLength: offset = %d, want %d", got, wtFwpV6AddrAndMask_prefixLength_Offset)
	}
}

func TestWFPConstantsMatchSDK(t *testing.T) {
	checks := []struct {
		name string
		got  uint64
		want uint64
	}{
		{"FWP_UINT8", uint64(cFWP_UINT8), 1},
		{"FWP_UINT16", uint64(cFWP_UINT16), 2},
		{"FWP_UINT32", uint64(cFWP_UINT32), 3},
		{"FWP_UINT64", uint64(cFWP_UINT64), 4},
		{"FWP_V4_ADDR_MASK", uint64(cFWP_V4_ADDR_MASK), 0x100},
		{"FWP_V6_ADDR_MASK", uint64(cFWP_V6_ADDR_MASK), 0x101},
		{"FWP_MATCH_EQUAL", uint64(cFWP_MATCH_EQUAL), 0},
		{"FWP_MATCH_FLAGS_ALL_SET", uint64(cFWP_MATCH_FLAGS_ALL_SET), 6},
		{"FWP_ACTION_BLOCK", uint64(cFWP_ACTION_BLOCK), 0x00001001},
		{"FWP_ACTION_PERMIT", uint64(cFWP_ACTION_PERMIT), 0x00001002},
		{"FWP_CONDITION_FLAG_IS_LOOPBACK", uint64(cFWP_CONDITION_FLAG_IS_LOOPBACK), 1},
		{"FWPM_FILTER_FLAG_PERSISTENT", uint64(cFWPM_FILTER_FLAG_PERSISTENT), 1},
		{"FWPM_SUBLAYER_FLAG_PERSISTENT", uint64(cFWPM_SUBLAYER_FLAG_PERSISTENT), 1},
		{"RPC_C_AUTHN_WINNT", uint64(cRPC_C_AUTHN_WINNT), 10},
		{"IPPROTO_UDP", uint64(cIPPROTO_UDP), 17},
		{"IPPROTO_TCP", uint64(cIPPROTO_TCP), 6},
		{"FWP_E_ALREADY_EXISTS", uint64(fwpEAlreadyExists), 0x80320009},
		{"FWP_E_FILTER_NOT_FOUND", uint64(fwpEFilterNotFound), 0x80320003},
		{"FWP_E_SUBLAYER_NOT_FOUND", uint64(fwpESublayerNotFound), 0x80320007},
	}
	for _, tc := range checks {
		if tc.got != tc.want {
			t.Errorf("%s = 0x%x, want 0x%x", tc.name, tc.got, tc.want)
		}
	}
}

// Every layer and condition key the kill switch uses, as printed in fwpmu.h.
// A wrong key fails FwpmFilterAdd0 with FWP_E_CONDITION_NOT_FOUND at runtime.
func TestWFPGUIDsMatchFwpmu(t *testing.T) {
	want := map[string]struct {
		got windows.GUID
		key string
	}{
		"LAYER_ALE_AUTH_CONNECT_V4":     {cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "{C38D57D1-05A7-4C33-904F-7FBCEEE60E82}"},
		"LAYER_ALE_AUTH_CONNECT_V6":     {cFWPM_LAYER_ALE_AUTH_CONNECT_V6, "{4A72393B-319F-44BC-84C3-BA54DCB3B6B4}"},
		"LAYER_ALE_AUTH_RECV_ACCEPT_V4": {cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, "{E1CD9FE7-F4B5-4273-96C0-592E487B8650}"},
		"LAYER_ALE_AUTH_RECV_ACCEPT_V6": {cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6, "{A3B42C97-9F04-4672-B87E-CEE9C483257F}"},
		"CONDITION_IP_PROTOCOL":         {cFWPM_CONDITION_IP_PROTOCOL, "{3971EF2B-623E-4F9A-8CB1-6E79B806B9A7}"},
		"CONDITION_IP_LOCAL_PORT":       {cFWPM_CONDITION_IP_LOCAL_PORT, "{0C1BA1AF-5765-453F-AF22-A8F791AC775B}"},
		"CONDITION_IP_REMOTE_PORT":      {cFWPM_CONDITION_IP_REMOTE_PORT, "{C35A604D-D22B-4E1A-91B4-68F674EE674B}"},
		"CONDITION_IP_REMOTE_ADDRESS":   {cFWPM_CONDITION_IP_REMOTE_ADDRESS, "{B235AE9A-1D64-49B8-A44C-5FF3D9095045}"},
		"CONDITION_IP_LOCAL_INTERFACE":  {cFWPM_CONDITION_IP_LOCAL_INTERFACE, "{4CD62A49-59C3-4969-B7F3-BDA5D32890A4}"},
		"CONDITION_FLAGS":               {cFWPM_CONDITION_FLAGS, "{632CE23B-5167-435C-86D7-E903684AA80C}"},
		// The forward layers see traffic the host routes for others (WSL2,
		// Hyper-V NAT, ICS), which never reaches the ALE layers above.
		"LAYER_IPFORWARD_V4":                    {cFWPM_LAYER_IPFORWARD_V4, "{A82ACC24-4EE1-4EE1-B465-FD1D25CB10A4}"},
		"LAYER_IPFORWARD_V6":                    {cFWPM_LAYER_IPFORWARD_V6, "{7B964818-19C7-493A-B71F-832C3684D28C}"},
		"CONDITION_IP_DESTINATION_ADDRESS":      {cFWPM_CONDITION_IP_DESTINATION_ADDRESS, "{2D79133B-B390-45C6-8699-ACACEAAFED33}"},
		"CONDITION_DESTINATION_INTERFACE_INDEX": {cFWPM_CONDITION_DESTINATION_INTERFACE_INDEX, "{35CF6522-4139-45EE-A0D5-67B80949D879}"},
	}
	for name, tc := range want {
		canonical, err := windows.GUIDFromString(tc.key)
		if err != nil {
			t.Fatalf("%s: bad reference GUID %s: %v", name, tc.key, err)
		}
		if tc.got != canonical {
			t.Errorf("FWPM_%s = %s, want %s", name, tc.got.String(), canonical.String())
		}
	}
}

func TestDHCPReplyConditionsMatchClientPortsOnly(t *testing.T) {
	conditions := dhcpReplyConditions()

	want := map[windows.GUID]uintptr{
		cFWPM_CONDITION_IP_PROTOCOL:    uintptr(cIPPROTO_UDP),
		cFWPM_CONDITION_IP_REMOTE_PORT: 67,
		cFWPM_CONDITION_IP_LOCAL_PORT:  68,
	}
	if len(conditions) != len(want) {
		t.Fatalf("got %d conditions, want %d (protocol + both ports, no address)", len(conditions), len(want))
	}
	for _, c := range conditions {
		value, ok := want[c.fieldKey]
		if !ok {
			t.Fatalf("unexpected condition on %v: the reply comes from the server or relay, not the broadcast address", c.fieldKey)
		}
		if c.conditionValue.value != value {
			t.Fatalf("condition %v = %d, want %d", c.fieldKey, c.conditionValue.value, value)
		}
	}
}

func TestSweepKeepsPersistentLockFilters(t *testing.T) {
	persistent := wtFwpmFilter0{flags: cFWPM_FILTER_FLAG_PERSISTENT}
	ephemeral := wtFwpmFilter0{}
	if isEphemeralFilter(&persistent) {
		t.Fatal("a persistent block or loopback permit must survive the re-arm sweep")
	}
	if !isEphemeralFilter(&ephemeral) {
		t.Fatal("an engine-keyed permit left by a dead process must be swept")
	}
}

func TestPersistentLockKeysAreDistinct(t *testing.T) {
	seen := make(map[windows.GUID]bool, len(pangeaPersistentFilterKeys))
	for _, key := range pangeaPersistentFilterKeys {
		if seen[key] {
			t.Fatalf("filter key %s appears twice; a shared key makes one filter silently replace another", key)
		}
		seen[key] = true
	}
	if seen[pangeaVPNSublayerKey] {
		t.Fatal("the sublayer key doubles as a filter key")
	}
	if len(pangeaPersistentFilterKeys) != len(persistentLockFilters) {
		t.Fatalf("%d persistent keys but %d persistent filters: Clear would leave one behind", len(pangeaPersistentFilterKeys), len(persistentLockFilters))
	}
}

// The ladder is the whole DNS story: LAN permits sit under the DNS block, the
// permits the lock trusts sit over it.
func TestFilterWeightLadder(t *testing.T) {
	if !(weightBlockAll < weightLANPermit && weightLANPermit < weightDNSBlock && weightDNSBlock < weightTrustedPermit) {
		t.Fatalf("weights block=%d lan=%d dns=%d trusted=%d are not strictly ascending", weightBlockAll, weightLANPermit, weightDNSBlock, weightTrustedPermit)
	}
}

func TestDNSBlockMatchesPortAndProtocolOnly(t *testing.T) {
	conditions := dnsBlockConditions(cIPPROTO_UDP, dnsPort)
	want := map[windows.GUID]uintptr{
		cFWPM_CONDITION_IP_PROTOCOL:    uintptr(cIPPROTO_UDP),
		cFWPM_CONDITION_IP_REMOTE_PORT: dnsPort,
	}
	if len(conditions) != len(want) {
		t.Fatalf("got %d conditions, want %d: the block must cover every remote address", len(conditions), len(want))
	}
	for _, c := range conditions {
		value, ok := want[c.fieldKey]
		if !ok || c.conditionValue.value != value {
			t.Fatalf("condition %v = %d, want %d", c.fieldKey, c.conditionValue.value, value)
		}
	}
}

func TestV4PermitsSkipsIPv6(t *testing.T) {
	got := v4Permits([]string{"203.0.113.7", "2001:db8::1", "not-an-ip"})
	if len(got) != 1 || got[0] != "203.0.113.7" {
		t.Fatalf("v4Permits = %v, want only the IPv4 literal", got)
	}
}

// Host-originated traffic is authorised at the ALE layers; traffic the host
// forwards for a VM or WSL2 is not, so the lock needs its own forward blocks.
func TestPersistentLockBlocksForwardedTraffic(t *testing.T) {
	keys := make(map[windows.GUID]bool, len(pangeaPersistentFilterKeys))
	for _, key := range pangeaPersistentFilterKeys {
		keys[key] = true
	}
	if !keys[pangeaBlockForwardV4FilterKey] || !keys[pangeaBlockForwardV6FilterKey] {
		t.Fatal("the persistent lock has no IPFORWARD block; NAT'd guest traffic bypasses it")
	}
	names := make(map[string]bool, len(persistentLockFilters))
	for _, step := range persistentLockFilters {
		names[step.name] = true
	}
	if !names["block forward v4"] || !names["block forward v6"] {
		t.Fatalf("persistent filter steps %v lack the forward blocks", names)
	}
}

// A forwarded packet is allowed only when it is about to leave via the tunnel.
func TestForwardTunnelPermitMatchesDestinationInterfaceIndex(t *testing.T) {
	conditions := forwardToInterfaceConditions(42)
	if len(conditions) != 1 {
		t.Fatalf("got %d conditions, want exactly the destination interface", len(conditions))
	}
	c := conditions[0]
	if c.fieldKey != cFWPM_CONDITION_DESTINATION_INTERFACE_INDEX || c.matchType != cFWP_MATCH_EQUAL {
		t.Fatalf("condition on %v/%v, want DESTINATION_INTERFACE_INDEX equal", c.fieldKey, c.matchType)
	}
	if c.conditionValue._type != cFWP_UINT32 || c.conditionValue.value != 42 {
		t.Fatalf("condition value type=%v value=%d, want UINT32 42", c.conditionValue._type, c.conditionValue.value)
	}
}

// Boot-time twins run before BFE loads the persistent set, so each needs its
// own key and must swap PERSISTENT for BOOTTIME.
func TestBootTimeVariantIsDistinctAndFlaggedBootTime(t *testing.T) {
	persistent := make(map[windows.GUID]bool, len(pangeaPersistentFilterKeys))
	for _, key := range pangeaPersistentFilterKeys {
		persistent[key] = true
	}
	seen := make(map[windows.GUID]bool, len(pangeaPersistentFilterKeys))
	for _, key := range pangeaPersistentFilterKeys {
		bootKey, flags := bootTimeVariant(key, cFWPM_FILTER_FLAG_PERSISTENT)
		if persistent[bootKey] || seen[bootKey] || bootKey == pangeaVPNSublayerKey {
			t.Fatalf("boot-time key %s collides with another filter key", bootKey)
		}
		seen[bootKey] = true
		if flags&cFWPM_FILTER_FLAG_PERSISTENT != 0 || flags&cFWPM_FILTER_FLAG_BOOTTIME == 0 {
			t.Fatalf("boot-time flags = %#x, want BOOTTIME without PERSISTENT", flags)
		}
	}
}

// The re-arm sweep retires engine-keyed permits only; the boot-time lock is as
// much part of the lock as the persistent one.
func TestSweepKeepsBootTimeFilters(t *testing.T) {
	bootTime := wtFwpmFilter0{flags: cFWPM_FILTER_FLAG_BOOTTIME}
	if isEphemeralFilter(&bootTime) {
		t.Fatal("a boot-time block was classed as a stale permit and would be swept on every arm")
	}
}

// A router that speaks DNS over TLS is the same Allow-LAN hole as plain DNS.
func TestPersistentLockBlocksDNSOverTLS(t *testing.T) {
	keys := make(map[windows.GUID]bool, len(pangeaPersistentFilterKeys))
	for _, key := range pangeaPersistentFilterKeys {
		keys[key] = true
	}
	if !keys[pangeaBlockDoTUDPV4FilterKey] || !keys[pangeaBlockDoTTCPV4FilterKey] {
		t.Fatal("the persistent lock has no port-853 block; Allow LAN lets DoT/DoQ reach a LAN resolver")
	}
	names := make(map[string]bool, len(persistentLockFilters))
	for _, step := range persistentLockFilters {
		names[step.name] = true
	}
	if !names["block DoT udp"] || !names["block DoT tcp"] {
		t.Fatalf("persistent filter steps %v lack the DoT blocks", names)
	}
	for _, c := range dnsBlockConditions(cIPPROTO_TCP, dotPort) {
		if c.fieldKey == cFWPM_CONDITION_IP_REMOTE_PORT && c.conditionValue.value != 853 {
			t.Fatalf("DoT block matches port %d, want 853", c.conditionValue.value)
		}
	}
}
