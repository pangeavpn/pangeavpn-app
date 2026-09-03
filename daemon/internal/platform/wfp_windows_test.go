//go:build windows

package platform

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestWFPStructSizes(t *testing.T) {
	ptrSize := unsafe.Sizeof(uintptr(0))
	t.Logf("GOARCH=%s, pointer size=%d bytes", runtime.GOARCH, ptrSize)

	if ptrSize != 8 {
		t.Fatalf("WFP structs are designed for 64-bit; pointer size is %d", ptrSize)
	}

	tests := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"fwpmDisplayData0", unsafe.Sizeof(fwpmDisplayData0{}), 16},
		{"fwpByteBlob", unsafe.Sizeof(fwpByteBlob{}), 16},
		{"fwpValue0", unsafe.Sizeof(fwpValue0{}), 16},
		{"fwpmAction0", unsafe.Sizeof(fwpmAction0{}), 20},
		{"fwpmSession0", unsafe.Sizeof(fwpmSession0{}), 72},
		{"fwpmSublayer0", unsafe.Sizeof(fwpmSublayer0{}), 72},
		{"fwpmFilterCondition0", unsafe.Sizeof(fwpmFilterCondition0{}), 40},
		{"fwpmFilter0", unsafe.Sizeof(fwpmFilter0{}), 200},
		{"fwpV4AddrAndMask", unsafe.Sizeof(fwpV4AddrAndMask{}), 8},
		{"fwpV6AddrAndMask", unsafe.Sizeof(fwpV6AddrAndMask{}), 17},
	}

	for _, tc := range tests {
		if tc.got != tc.want {
			t.Errorf("%s: size = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestWFPFilterFieldOffsets(t *testing.T) {
	var f fwpmFilter0
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"filterKey", unsafe.Offsetof(f.filterKey), 0},
		{"displayData", unsafe.Offsetof(f.displayData), 16},
		{"flags", unsafe.Offsetof(f.flags), 32},
		{"providerKey", unsafe.Offsetof(f.providerKey), 40},
		{"providerData", unsafe.Offsetof(f.providerData), 48},
		{"layerKey", unsafe.Offsetof(f.layerKey), 64},
		{"subLayerKey", unsafe.Offsetof(f.subLayerKey), 80},
		{"weight", unsafe.Offsetof(f.weight), 96},
		{"numFilterConditions", unsafe.Offsetof(f.numFilterConditions), 112},
		{"filterCondition", unsafe.Offsetof(f.filterCondition), 120},
		{"action", unsafe.Offsetof(f.action), 128},
		{"rawContext", unsafe.Offsetof(f.rawContext), 152},
		{"reserved", unsafe.Offsetof(f.reserved), 168},
		{"filterId", unsafe.Offsetof(f.filterId), 176},
		{"effectiveWeight", unsafe.Offsetof(f.effectiveWeight), 184},
	}

	for _, tc := range offsets {
		if tc.got != tc.want {
			t.Errorf("fwpmFilter0.%s: offset = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestWFPAddrMaskFieldOffsets(t *testing.T) {
	var v4 fwpV4AddrAndMask
	var v6 fwpV6AddrAndMask
	offsets := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"fwpV4AddrAndMask.addr", unsafe.Offsetof(v4.addr), 0},
		{"fwpV4AddrAndMask.mask", unsafe.Offsetof(v4.mask), 4},
		{"fwpV6AddrAndMask.addr", unsafe.Offsetof(v6.addr), 0},
		{"fwpV6AddrAndMask.prefixLength", unsafe.Offsetof(v6.prefixLength), 16},
	}

	for _, tc := range offsets {
		if tc.got != tc.want {
			t.Errorf("%s: offset = %d, want %d", tc.name, tc.got, tc.want)
		}
	}
}

func TestWFPConstants(t *testing.T) {
	// FWP_DATA_TYPE
	if fwpUint8 != 1 {
		t.Errorf("fwpUint8 = %d, want 1", fwpUint8)
	}
	if fwpUint16 != 2 {
		t.Errorf("fwpUint16 = %d, want 2", fwpUint16)
	}
	if fwpUint32 != 3 {
		t.Errorf("fwpUint32 = %d, want 3", fwpUint32)
	}
	if fwpV4AddrMask != 0x100 {
		t.Errorf("fwpV4AddrMask = 0x%x, want 0x100", fwpV4AddrMask)
	}
	if fwpV6AddrMask != 0x101 {
		t.Errorf("fwpV6AddrMask = 0x%x, want 0x101", fwpV6AddrMask)
	}

	// FWP_MATCH_TYPE
	if fwpMatchEqual != 0 {
		t.Errorf("fwpMatchEqual = %d, want 0", fwpMatchEqual)
	}
	if fwpMatchFlagsAllSet != 6 {
		t.Errorf("fwpMatchFlagsAllSet = %d, want 6", fwpMatchFlagsAllSet)
	}

	// FWP_ACTION_TYPE (must include FWP_ACTION_FLAG_TERMINATING = 0x1000)
	if fwpActionBlock != 0x00001001 {
		t.Errorf("fwpActionBlock = 0x%x, want 0x00001001", fwpActionBlock)
	}
	if fwpActionPermit != 0x00001002 {
		t.Errorf("fwpActionPermit = 0x%x, want 0x00001002", fwpActionPermit)
	}

	// Misc
	if fwpConditionFlagIsLoopback != 1 {
		t.Errorf("fwpConditionFlagIsLoopback = %d, want 1", fwpConditionFlagIsLoopback)
	}
	if rpcCAuthnWinnt != 10 {
		t.Errorf("rpcCAuthnWinnt = %d, want 10", rpcCAuthnWinnt)
	}
	if ipprotoUDP != 17 {
		t.Errorf("ipprotoUDP = %d, want 17", ipprotoUDP)
	}

	// A wrong value here is rejected at runtime with FWP_E_INVALID_FLAGS.
	if fwpmFilterFlagPersistent != 0x00000001 {
		t.Errorf("fwpmFilterFlagPersistent = 0x%x, want 0x00000001", fwpmFilterFlagPersistent)
	}
	if fwpmSublayerFlagPersistent != 0x0001 {
		t.Errorf("fwpmSublayerFlagPersistent = 0x%x, want 0x0001", fwpmSublayerFlagPersistent)
	}
}

func TestDHCPReplyConditionsMatchClientPortsOnly(t *testing.T) {
	conditions := dhcpReplyConditions()

	want := map[windows.GUID]uintptr{
		fwpmConditionIpProtocol:   uintptr(ipprotoUDP),
		fwpmConditionIpRemotePort: 67,
		fwpmConditionIpLocalPort:  68,
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

func TestWFPConditionGUIDsMatchFwpmu(t *testing.T) {
	// FWPM_CONDITION_* keys as published in fwpmu.h; a wrong key fails every
	// FwpmFilterAdd0 that uses it with FWP_E_CONDITION_NOT_FOUND.
	want := map[string]struct {
		got windows.GUID
		key string
	}{
		"IP_PROTOCOL":        {fwpmConditionIpProtocol, "{3971EF2B-623E-4F9A-8CB1-6E79B806B9A7}"},
		"IP_LOCAL_PORT":      {fwpmConditionIpLocalPort, "{0C1BA1AF-5765-453F-AF22-A8F791AC775B}"},
		"IP_REMOTE_PORT":     {fwpmConditionIpRemotePort, "{C35A604D-D22B-4E1A-91B4-68F674EE674B}"},
		"IP_REMOTE_ADDRESS":  {fwpmConditionIpRemoteAddress, "{B235AE9A-1D64-49B8-A44C-5FF3D9095045}"},
		"IP_LOCAL_INTERFACE": {fwpmConditionIpLocalInterface, "{4CD62A49-59C3-4969-B7F3-BDA5D32890A4}"},
		"FLAGS":              {fwpmConditionFlags, "{632CE23B-5167-435C-86D7-E903684AA80C}"},
	}
	for name, tc := range want {
		canonical, err := windows.GUIDFromString(tc.key)
		if err != nil {
			t.Fatalf("%s: bad reference GUID %s: %v", name, tc.key, err)
		}
		if tc.got != canonical {
			t.Errorf("FWPM_CONDITION_%s = %s, want %s", name, tc.got.String(), canonical.String())
		}
	}
}
