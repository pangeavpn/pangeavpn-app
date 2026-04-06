//go:build windows

package platform

import (
	"runtime"
	"testing"
	"unsafe"
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
}
