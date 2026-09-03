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
