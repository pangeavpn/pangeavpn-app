//go:build windows

package platform

import (
	"fmt"
	"net"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ---------------------------------------------------------------------------
// fwpuclnt.dll — Windows Filtering Platform user-mode API
// ---------------------------------------------------------------------------

var (
	modFwpuclnt = windows.NewLazySystemDLL("fwpuclnt.dll")

	procFwpmEngineOpen0          = modFwpuclnt.NewProc("FwpmEngineOpen0")
	procFwpmEngineClose0         = modFwpuclnt.NewProc("FwpmEngineClose0")
	procFwpmTransactionBegin0    = modFwpuclnt.NewProc("FwpmTransactionBegin0")
	procFwpmTransactionCommit0   = modFwpuclnt.NewProc("FwpmTransactionCommit0")
	procFwpmTransactionAbort0    = modFwpuclnt.NewProc("FwpmTransactionAbort0")
	procFwpmSubLayerAdd0         = modFwpuclnt.NewProc("FwpmSubLayerAdd0")
	procFwpmSubLayerDeleteByKey0 = modFwpuclnt.NewProc("FwpmSubLayerDeleteByKey0")
	procFwpmFilterAdd0              = modFwpuclnt.NewProc("FwpmFilterAdd0")
	procFwpmFilterDeleteById0       = modFwpuclnt.NewProc("FwpmFilterDeleteById0")
	procFwpmFilterCreateEnumHandle0 = modFwpuclnt.NewProc("FwpmFilterCreateEnumHandle0")
	procFwpmFilterEnum0             = modFwpuclnt.NewProc("FwpmFilterEnum0")
	procFwpmFilterDestroyEnumHandle0 = modFwpuclnt.NewProc("FwpmFilterDestroyEnumHandle0")
	procFwpmFreeMemory0             = modFwpuclnt.NewProc("FwpmFreeMemory0")
)

// ---------------------------------------------------------------------------
// WFP constants
// ---------------------------------------------------------------------------

const (
	fwpUint8      uint32 = 1
	fwpUint16     uint32 = 2
	fwpUint32     uint32 = 3
	fwpUint64     uint32 = 4
	fwpV4AddrMask uint32 = 0x100

	fwpMatchEqual       uint32 = 0
	fwpMatchFlagsAllSet uint32 = 6

	fwpActionBlock  uint32 = 0x00001001 // FWP_ACTION_BLOCK: 0x1 | FWP_ACTION_FLAG_TERMINATING
	fwpActionPermit uint32 = 0x00001002 // FWP_ACTION_PERMIT: 0x2 | FWP_ACTION_FLAG_TERMINATING

	fwpConditionFlagIsLoopback uint32 = 0x00000001

	fwpmSessionFlagDynamic uint32 = 0x00000001 // auto-cleanup on handle close / process exit

	fwpEAlreadyExists uint32 = 0x80320009 // FWP_E_ALREADY_EXISTS

	rpcCAuthnWinnt uint32 = 10
	ipprotoUDP     uint8  = 17
)

// Well-known WFP layer GUIDs.
var (
	fwpmLayerAleAuthConnectV4    = windows.GUID{Data1: 0xc38d57d1, Data2: 0x05a7, Data3: 0x4c33, Data4: [8]byte{0x90, 0x4f, 0x7f, 0xbc, 0xee, 0xe6, 0x0e, 0x82}}
	fwpmLayerAleAuthConnectV6    = windows.GUID{Data1: 0x4a72393b, Data2: 0x319f, Data3: 0x44bc, Data4: [8]byte{0x84, 0xc3, 0xba, 0x54, 0xdc, 0xb3, 0xb6, 0xb4}}
	fwpmLayerAleAuthRecvAcceptV4 = windows.GUID{Data1: 0xe1cd9fe7, Data2: 0xf4b5, Data3: 0x4273, Data4: [8]byte{0x96, 0xc0, 0x59, 0x2e, 0x48, 0x7b, 0x86, 0x50}}
	fwpmLayerAleAuthRecvAcceptV6 = windows.GUID{Data1: 0xa3b42c97, Data2: 0x9f04, Data3: 0x4672, Data4: [8]byte{0xb8, 0x7e, 0xce, 0xe9, 0xc4, 0x83, 0x25, 0x7f}}
)

// WFP condition field GUIDs.
var (
	fwpmConditionFlags           = windows.GUID{Data1: 0x632ce23b, Data2: 0x5167, Data3: 0x435c, Data4: [8]byte{0x86, 0xd7, 0xe9, 0x03, 0x68, 0x4a, 0xa8, 0x0c}}
	fwpmConditionIpRemoteAddress = windows.GUID{Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8, Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45}}
	fwpmConditionIpProtocol      = windows.GUID{Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a, Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa6}}
	fwpmConditionIpRemotePort    = windows.GUID{Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x440d, Data4: [8]byte{0xa1, 0xd4, 0x0f, 0x22, 0x44, 0xd3, 0xb2, 0xe2}}
	fwpmConditionIpLocalPort      = windows.GUID{Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f, Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf4, 0xfe, 0x04, 0x5f, 0x71}}
	fwpmConditionIpLocalInterface = windows.GUID{Data1: 0x4cd62a49, Data2: 0x59c3, Data3: 0x4969, Data4: [8]byte{0xb7, 0xf3, 0xbd, 0xa5, 0xd3, 0x28, 0x90, 0xa4}}
)

// PangeaVPN sublayer GUID — deterministic, unique to this application.
var pangeaVPNSublayerKey = windows.GUID{Data1: 0xa9d3e8f1, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f}}

// ---------------------------------------------------------------------------
// WFP struct definitions — must match C ABI on 64-bit Windows
// ---------------------------------------------------------------------------

type fwpmDisplayData0 struct {
	name        uintptr // *uint16
	description uintptr // *uint16
} // 16 bytes

type fwpByteBlob struct {
	size uint32
	_    uint32
	data uintptr
} // 16 bytes

type fwpValue0 struct {
	valueType uint32
	_         uint32
	value     uintptr // union: stores uint8/uint16/uint32 directly, or pointer for larger types
} // 16 bytes

type fwpmAction0 struct {
	actionType uint32
	filterType windows.GUID // union: filterType / calloutKey
} // 20 bytes

type fwpmSession0 struct {
	sessionKey           windows.GUID
	displayData          fwpmDisplayData0
	flags                uint32
	txnWaitTimeoutInMSec uint32
	processId            uint32
	_pad0                uint32
	sid                  uintptr
	username             uintptr
	kernelMode           int32
	_pad1                int32
} // 72 bytes

type fwpmSublayer0 struct {
	subLayerKey  windows.GUID
	displayData  fwpmDisplayData0
	flags        uint16
	_pad0        uint16
	_pad1        uint32
	providerKey  uintptr
	providerData fwpByteBlob
	weight       uint16
	_pad2        [6]byte
} // 72 bytes

type fwpmFilterCondition0 struct {
	fieldKey       windows.GUID
	matchType      uint32
	_pad           uint32
	conditionValue fwpValue0 // FWP_CONDITION_VALUE0 has same layout as FWP_VALUE0
} // 40 bytes

type fwpmFilter0 struct {
	filterKey           windows.GUID
	displayData         fwpmDisplayData0
	flags               uint32
	_pad0               uint32
	providerKey         uintptr
	providerData        fwpByteBlob
	layerKey            windows.GUID
	subLayerKey         windows.GUID
	weight              fwpValue0
	numFilterConditions uint32
	_pad1               uint32
	filterCondition     uintptr
	action              fwpmAction0
	_pad2               uint32
	rawContext          [16]byte // union: UINT64 rawContext / GUID providerContextKey
	reserved            uintptr
	filterId            uint64
	effectiveWeight     fwpValue0
} // 200 bytes

type fwpV4AddrAndMask struct {
	addr uint32 // IPv4 in host byte order: (a<<24)|(b<<16)|(c<<8)|d
	mask uint32
} // 8 bytes

// ---------------------------------------------------------------------------
// wfpEngine wraps a WFP engine handle
// ---------------------------------------------------------------------------

type wfpEngine struct {
	handle windows.Handle
}

func wfpAvailable() bool {
	return modFwpuclnt.Load() == nil && procFwpmEngineOpen0.Find() == nil
}

func wfpOpen() (*wfpEngine, error) {
	name, _ := windows.UTF16PtrFromString("PangeaVPN Kill Switch")

	session := fwpmSession0{
		flags: fwpmSessionFlagDynamic, // auto-cleanup on handle close / crash
	}
	session.displayData.name = uintptr(unsafe.Pointer(name))

	var handle windows.Handle
	r, _, _ := procFwpmEngineOpen0.Call(
		0,
		uintptr(rpcCAuthnWinnt),
		0,
		uintptr(unsafe.Pointer(&session)),
		uintptr(unsafe.Pointer(&handle)),
	)
	runtime.KeepAlive(name)
	runtime.KeepAlive(&session)
	if r != 0 {
		return nil, fmt.Errorf("FwpmEngineOpen0: %w", windows.Errno(r))
	}

	return &wfpEngine{handle: handle}, nil
}

func (e *wfpEngine) close() {
	if e.handle != 0 {
		procFwpmEngineClose0.Call(uintptr(e.handle))
		e.handle = 0
	}
}

func (e *wfpEngine) beginTransaction() error {
	r, _, _ := procFwpmTransactionBegin0.Call(uintptr(e.handle), 0)
	if r != 0 {
		return fmt.Errorf("FwpmTransactionBegin0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) commitTransaction() error {
	r, _, _ := procFwpmTransactionCommit0.Call(uintptr(e.handle))
	if r != 0 {
		return fmt.Errorf("FwpmTransactionCommit0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) abortTransaction() {
	procFwpmTransactionAbort0.Call(uintptr(e.handle))
}

func (e *wfpEngine) addSublayer() error {
	name, _ := windows.UTF16PtrFromString("PangeaVPN Kill Switch")
	desc, _ := windows.UTF16PtrFromString("Blocks non-VPN traffic")

	sublayer := fwpmSublayer0{
		subLayerKey: pangeaVPNSublayerKey,
		displayData: fwpmDisplayData0{
			name:        uintptr(unsafe.Pointer(name)),
			description: uintptr(unsafe.Pointer(desc)),
		},
		weight: 0xFFFF, // highest priority sublayer
	}

	r, _, _ := procFwpmSubLayerAdd0.Call(
		uintptr(e.handle),
		uintptr(unsafe.Pointer(&sublayer)),
		0,
	)
	runtime.KeepAlive(name)
	runtime.KeepAlive(desc)
	runtime.KeepAlive(&sublayer)
	if r != 0 {
		if uint32(r) == fwpEAlreadyExists {
			return nil
		}
		return fmt.Errorf("FwpmSubLayerAdd0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) deleteSublayer() error {
	key := pangeaVPNSublayerKey
	r, _, _ := procFwpmSubLayerDeleteByKey0.Call(
		uintptr(e.handle),
		uintptr(unsafe.Pointer(&key)),
	)
	if r != 0 {
		if windows.Errno(r) == windows.ERROR_NOT_FOUND {
			return nil
		}
		return fmt.Errorf("FwpmSubLayerDeleteByKey0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) addFilter(layer windows.GUID, filterName string, weight uint8, action uint32, conditions []fwpmFilterCondition0) (uint64, error) {
	namePtr, _ := windows.UTF16PtrFromString(filterName)

	filter := fwpmFilter0{
		displayData: fwpmDisplayData0{
			name: uintptr(unsafe.Pointer(namePtr)),
		},
		layerKey:    layer,
		subLayerKey: pangeaVPNSublayerKey,
		weight: fwpValue0{
			valueType: fwpUint8,
			value:     uintptr(weight),
		},
		action: fwpmAction0{
			actionType: action,
		},
		numFilterConditions: uint32(len(conditions)),
	}

	if len(conditions) > 0 {
		filter.filterCondition = uintptr(unsafe.Pointer(&conditions[0]))
	}

	var filterId uint64
	r, _, _ := procFwpmFilterAdd0.Call(
		uintptr(e.handle),
		uintptr(unsafe.Pointer(&filter)),
		0,
		uintptr(unsafe.Pointer(&filterId)),
	)
	runtime.KeepAlive(namePtr)
	runtime.KeepAlive(&filter)
	runtime.KeepAlive(conditions)
	if r != 0 {
		return 0, fmt.Errorf("FwpmFilterAdd0 (%s): %w", filterName, windows.Errno(r))
	}
	return filterId, nil
}

func (e *wfpEngine) deleteFilter(filterId uint64) error {
	r, _, _ := procFwpmFilterDeleteById0.Call(
		uintptr(e.handle),
		uintptr(filterId),
	)
	if r != 0 {
		return fmt.Errorf("FwpmFilterDeleteById0: %w", windows.Errno(r))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Kill switch filter builders
// ---------------------------------------------------------------------------

func (e *wfpEngine) addBlockAllOutbound() (uint64, error) {
	return e.addFilter(fwpmLayerAleAuthConnectV4, "PangeaVPN Block All Outbound", 1, fwpActionBlock, nil)
}

func (e *wfpEngine) addPermitLoopback() (uint64, error) {
	conditions := []fwpmFilterCondition0{
		{
			fieldKey:  fwpmConditionFlags,
			matchType: fwpMatchFlagsAllSet,
			conditionValue: fwpValue0{
				valueType: fwpUint32,
				value:     uintptr(fwpConditionFlagIsLoopback),
			},
		},
	}
	return e.addFilter(fwpmLayerAleAuthConnectV4, "PangeaVPN Allow Loopback", 10, fwpActionPermit, conditions)
}

func (e *wfpEngine) addPermitEndpointIP(ipStr string) (uint64, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return 0, fmt.Errorf("invalid IPv4 address: %s", ipStr)
	}

	addrMask := fwpV4AddrAndMask{
		addr: uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3]),
		mask: 0xFFFFFFFF,
	}

	conditions := []fwpmFilterCondition0{
		{
			fieldKey:  fwpmConditionIpRemoteAddress,
			matchType: fwpMatchEqual,
			conditionValue: fwpValue0{
				valueType: fwpV4AddrMask,
				value:     uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	id, err := e.addFilter(fwpmLayerAleAuthConnectV4, "PangeaVPN Allow Endpoint "+ipStr, 10, fwpActionPermit, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

// addPermitIPv4Subnet permits outbound traffic to the given IPv4 CIDR.
// Used for the "Allow LAN" option — permits RFC1918, link-local, and
// multicast ranges so captive portals and gateway probes work on
// restrictive WiFi.
func (e *wfpEngine) addPermitIPv4Subnet(cidr string) (uint64, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return 0, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}
	ip := network.IP.To4()
	if ip == nil {
		return 0, fmt.Errorf("CIDR %s is not IPv4", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return 0, fmt.Errorf("CIDR %s has non-IPv4 mask", cidr)
	}
	var maskUint uint32
	if ones == 0 {
		maskUint = 0
	} else {
		maskUint = uint32(0xFFFFFFFF) << uint32(32-ones)
	}

	addrMask := fwpV4AddrAndMask{
		addr: uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3]),
		mask: maskUint,
	}

	conditions := []fwpmFilterCondition0{
		{
			fieldKey:  fwpmConditionIpRemoteAddress,
			matchType: fwpMatchEqual,
			conditionValue: fwpValue0{
				valueType: fwpV4AddrMask,
				value:     uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	id, err := e.addFilter(fwpmLayerAleAuthConnectV4, "PangeaVPN Allow LAN "+cidr, 10, fwpActionPermit, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

func (e *wfpEngine) addPermitDHCP() (uint64, error) {
	conditions := []fwpmFilterCondition0{
		{
			fieldKey:  fwpmConditionIpProtocol,
			matchType: fwpMatchEqual,
			conditionValue: fwpValue0{
				valueType: fwpUint8,
				value:     uintptr(ipprotoUDP),
			},
		},
		{
			fieldKey:  fwpmConditionIpRemotePort,
			matchType: fwpMatchEqual,
			conditionValue: fwpValue0{
				valueType: fwpUint16,
				value:     uintptr(67),
			},
		},
		{
			fieldKey:  fwpmConditionIpLocalPort,
			matchType: fwpMatchEqual,
			conditionValue: fwpValue0{
				valueType: fwpUint16,
				value:     uintptr(68),
			},
		},
	}
	return e.addFilter(fwpmLayerAleAuthConnectV4, "PangeaVPN Allow DHCP", 10, fwpActionPermit, conditions)
}

func (e *wfpEngine) addPermitTunnelInterface(luid uint64) (uint64, error) {
	conditions := []fwpmFilterCondition0{
		{
			fieldKey:  fwpmConditionIpLocalInterface,
			matchType: fwpMatchEqual,
			conditionValue: fwpValue0{
				valueType: fwpUint64,
				value:     uintptr(unsafe.Pointer(&luid)),
			},
		},
	}
	id, err := e.addFilter(fwpmLayerAleAuthConnectV4, "PangeaVPN Allow Tunnel Interface", 10, fwpActionPermit, conditions)
	runtime.KeepAlive(&luid)
	return id, err
}

func (e *wfpEngine) addBlockAllOutboundV6() (uint64, error) {
	return e.addFilter(fwpmLayerAleAuthConnectV6, "PangeaVPN Block All Outbound IPv6", 1, fwpActionBlock, nil)
}

func (e *wfpEngine) addBlockAllInboundV6() (uint64, error) {
	return e.addFilter(fwpmLayerAleAuthRecvAcceptV6, "PangeaVPN Block All Inbound IPv6", 1, fwpActionBlock, nil)
}

func (e *wfpEngine) addPermitLoopbackV6() (uint64, error) {
	conditions := []fwpmFilterCondition0{
		{
			fieldKey:  fwpmConditionFlags,
			matchType: fwpMatchFlagsAllSet,
			conditionValue: fwpValue0{
				valueType: fwpUint32,
				value:     uintptr(fwpConditionFlagIsLoopback),
			},
		},
	}
	return e.addFilter(fwpmLayerAleAuthConnectV6, "PangeaVPN Allow Loopback IPv6", 10, fwpActionPermit, conditions)
}

// deleteAllSublayerFilters enumerates all WFP filters and deletes those
// belonging to our sublayer. Used to clean up persistent filters left by
// a previous version that didn't use dynamic sessions.
func (e *wfpEngine) deleteAllSublayerFilters() {
	var enumHandle windows.Handle
	r, _, _ := procFwpmFilterCreateEnumHandle0.Call(
		uintptr(e.handle),
		0, // NULL template = all filters
		uintptr(unsafe.Pointer(&enumHandle)),
	)
	if r != 0 {
		return
	}
	defer procFwpmFilterDestroyEnumHandle0.Call(uintptr(e.handle), uintptr(enumHandle))

	for {
		var entries uintptr
		var numEntries uint32
		r, _, _ := procFwpmFilterEnum0.Call(
			uintptr(e.handle),
			uintptr(enumHandle),
			100,
			uintptr(unsafe.Pointer(&entries)),
			uintptr(unsafe.Pointer(&numEntries)),
		)
		if r != 0 || numEntries == 0 {
			break
		}

		ptrs := unsafe.Slice((*uintptr)(unsafe.Pointer(entries)), numEntries)
		for _, ptr := range ptrs {
			filter := (*fwpmFilter0)(unsafe.Pointer(ptr))
			if filter.subLayerKey == pangeaVPNSublayerKey {
				_ = e.deleteFilter(filter.filterId)
			}
		}

		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
	}
}
