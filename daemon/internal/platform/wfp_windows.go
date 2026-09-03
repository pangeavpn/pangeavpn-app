//go:build windows

package platform

import (
	"fmt"
	"net"
	"runtime"
	"strings"
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
	procFwpmFilterAdd0           = modFwpuclnt.NewProc("FwpmFilterAdd0")
	procFwpmFilterDeleteById0    = modFwpuclnt.NewProc("FwpmFilterDeleteById0")
	procFwpmFilterDeleteByKey0   = modFwpuclnt.NewProc("FwpmFilterDeleteByKey0")

	procFwpmFilterCreateEnumHandle0  = modFwpuclnt.NewProc("FwpmFilterCreateEnumHandle0")
	procFwpmFilterEnum0              = modFwpuclnt.NewProc("FwpmFilterEnum0")
	procFwpmFilterDestroyEnumHandle0 = modFwpuclnt.NewProc("FwpmFilterDestroyEnumHandle0")
	procFwpmFreeMemory0              = modFwpuclnt.NewProc("FwpmFreeMemory0")
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
	fwpV6AddrMask uint32 = 0x101

	fwpMatchEqual       uint32 = 0
	fwpMatchFlagsAllSet uint32 = 6

	fwpActionBlock  uint32 = 0x00001001 // FWP_ACTION_BLOCK: 0x1 | FWP_ACTION_FLAG_TERMINATING
	fwpActionPermit uint32 = 0x00001002 // FWP_ACTION_PERMIT: 0x2 | FWP_ACTION_FLAG_TERMINATING

	fwpConditionFlagIsLoopback uint32 = 0x00000001

	// Static-session objects outlive this process; PERSISTENT also survives a
	// reboot, so the lock stays engaged until explicitly cleared.
	fwpmSublayerFlagPersistent uint16 = 0x0001
	fwpmFilterFlagPersistent   uint32 = 0x00000001

	fwpEAlreadyExists    uint32 = 0x80320009 // FWP_E_ALREADY_EXISTS
	fwpEFilterNotFound   uint32 = 0x80320003 // FWP_E_FILTER_NOT_FOUND
	fwpESublayerNotFound uint32 = 0x80320007 // FWP_E_SUBLAYER_NOT_FOUND

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
	fwpmConditionFlags            = windows.GUID{Data1: 0x632ce23b, Data2: 0x5167, Data3: 0x435c, Data4: [8]byte{0x86, 0xd7, 0xe9, 0x03, 0x68, 0x4a, 0xa8, 0x0c}}
	fwpmConditionIpRemoteAddress  = windows.GUID{Data1: 0xb235ae9a, Data2: 0x1d64, Data3: 0x49b8, Data4: [8]byte{0xa4, 0x4c, 0x5f, 0xf3, 0xd9, 0x09, 0x50, 0x45}}
	fwpmConditionIpProtocol       = windows.GUID{Data1: 0x3971ef2b, Data2: 0x623e, Data3: 0x4f9a, Data4: [8]byte{0x8c, 0xb1, 0x6e, 0x79, 0xb8, 0x06, 0xb9, 0xa7}}
	fwpmConditionIpRemotePort     = windows.GUID{Data1: 0xc35a604d, Data2: 0xd22b, Data3: 0x4e1a, Data4: [8]byte{0x91, 0xb4, 0x68, 0xf6, 0x74, 0xee, 0x67, 0x4b}}
	fwpmConditionIpLocalPort      = windows.GUID{Data1: 0x0c1ba1af, Data2: 0x5765, Data3: 0x453f, Data4: [8]byte{0xaf, 0x22, 0xa8, 0xf7, 0x91, 0xac, 0x77, 0x5b}}
	fwpmConditionIpLocalInterface = windows.GUID{Data1: 0x4cd62a49, Data2: 0x59c3, Data3: 0x4969, Data4: [8]byte{0xb7, 0xf3, 0xbd, 0xa5, 0xd3, 0x28, 0x90, 0xa4}}
)

// PangeaVPN sublayer GUID — deterministic, unique to this application.
var pangeaVPNSublayerKey = windows.GUID{Data1: 0xa9d3e8f1, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x6f}}

// Deterministic filter keys so a later process can find and delete these
// filters without a filter ID from the dead process that added them.
var (
	pangeaBlockAllOutboundV4FilterKey = windows.GUID{Data1: 0xa9d3e8f2, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x70}}
	pangeaBlockAllInboundV4FilterKey  = windows.GUID{Data1: 0xa9d3e8f3, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x71}}
	pangeaBlockAllOutboundV6FilterKey = windows.GUID{Data1: 0xa9d3e8f4, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x72}}
	pangeaBlockAllInboundV6FilterKey  = windows.GUID{Data1: 0xa9d3e8f5, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x73}}
)

// Loopback permits are persistent for the same reason the blocks are: a lock
// that outlives a reboot must not take the local daemon API down with it.
var (
	pangeaPermitLoopbackV4FilterKey        = windows.GUID{Data1: 0xa9d3e8f6, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x74}}
	pangeaPermitLoopbackInboundV4FilterKey = windows.GUID{Data1: 0xa9d3e8f7, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x75}}
	pangeaPermitLoopbackNetV4FilterKey     = windows.GUID{Data1: 0xa9d3e8f8, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x76}}
	pangeaPermitLoopbackNetInV4FilterKey   = windows.GUID{Data1: 0xa9d3e8f9, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x77}}
	pangeaPermitLoopbackV6FilterKey        = windows.GUID{Data1: 0xa9d3e8fa, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x78}}
	pangeaPermitLoopbackInboundV6FilterKey = windows.GUID{Data1: 0xa9d3e8fb, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x79}}
	pangeaPermitLoopbackNetV6FilterKey     = windows.GUID{Data1: 0xa9d3e8fc, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x7a}}
	pangeaPermitLoopbackNetInV6FilterKey   = windows.GUID{Data1: 0xa9d3e8fd, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x7b}}
)

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

type fwpV6AddrAndMask struct {
	addr         [16]byte // IPv6 in network byte order
	prefixLength uint8
} // 17 bytes — all UINT8 fields, no padding

// ---------------------------------------------------------------------------
// wfpEngine wraps a WFP engine handle
// ---------------------------------------------------------------------------

type wfpEngine struct {
	handle windows.Handle
}

func wfpOpen() (*wfpEngine, error) {
	name, err := windows.UTF16PtrFromString("PangeaVPN Kill Switch")
	if err != nil {
		return nil, fmt.Errorf("session display name: %w", err)
	}

	// Static (non-dynamic) session: filters survive this process dying, so a
	// crash/kill/OOM doesn't silently open the firewall.
	session := fwpmSession0{}
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

func (e *wfpEngine) close() error {
	if e.handle == 0 {
		return nil
	}
	r, _, _ := procFwpmEngineClose0.Call(uintptr(e.handle))
	e.handle = 0
	if r != 0 {
		return fmt.Errorf("FwpmEngineClose0: %w", windows.Errno(r))
	}
	return nil
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
	name, err := windows.UTF16PtrFromString("PangeaVPN Kill Switch")
	if err != nil {
		return fmt.Errorf("sublayer display name: %w", err)
	}
	desc, err := windows.UTF16PtrFromString("Blocks non-VPN traffic")
	if err != nil {
		return fmt.Errorf("sublayer description: %w", err)
	}

	sublayer := fwpmSublayer0{
		subLayerKey: pangeaVPNSublayerKey,
		displayData: fwpmDisplayData0{
			name:        uintptr(unsafe.Pointer(name)),
			description: uintptr(unsafe.Pointer(desc)),
		},
		flags:  fwpmSublayerFlagPersistent,
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

func (e *wfpEngine) deleteSublayerByKey(key windows.GUID) error {
	r, _, _ := procFwpmSubLayerDeleteByKey0.Call(
		uintptr(e.handle),
		uintptr(unsafe.Pointer(&key)),
	)
	if r != 0 {
		if uint32(r) == fwpESublayerNotFound {
			return nil
		}
		return fmt.Errorf("FwpmSubLayerDeleteByKey0: %w", windows.Errno(r))
	}
	return nil
}

// addFilter adds an ephemeral, engine-assigned-key filter. See addFilterKeyed
// for filters that must be idempotent and outlive this process.
func (e *wfpEngine) addFilter(layer windows.GUID, filterName string, weight uint8, action uint32, conditions []fwpmFilterCondition0) (uint64, error) {
	return e.addFilterKeyed(layer, windows.GUID{}, filterName, weight, action, 0, conditions)
}

// addFilterKeyed adds a filter under a caller-chosen key so a later process
// can find/delete it by that key. Re-adding an existing key returns (0, nil).
func (e *wfpEngine) addFilterKeyed(layer, filterKey windows.GUID, filterName string, weight uint8, action, flags uint32, conditions []fwpmFilterCondition0) (uint64, error) {
	namePtr, err := windows.UTF16PtrFromString(filterName)
	if err != nil {
		return 0, fmt.Errorf("filter name %q: %w", filterName, err)
	}

	filter := fwpmFilter0{
		filterKey: filterKey,
		displayData: fwpmDisplayData0{
			name: uintptr(unsafe.Pointer(namePtr)),
		},
		flags:       flags,
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
		if uint32(r) == fwpEAlreadyExists {
			return 0, nil
		}
		return 0, fmt.Errorf("FwpmFilterAdd0 (%s): %w", filterName, windows.Errno(r))
	}
	return filterId, nil
}

func (e *wfpEngine) deleteFilterByKey(key windows.GUID) error {
	r, _, _ := procFwpmFilterDeleteByKey0.Call(
		uintptr(e.handle),
		uintptr(unsafe.Pointer(&key)),
	)
	if r != 0 {
		if uint32(r) == fwpEFilterNotFound {
			return nil
		}
		return fmt.Errorf("FwpmFilterDeleteByKey0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) deleteFilter(filterId uint64) error {
	r, _, _ := procFwpmFilterDeleteById0.Call(
		uintptr(e.handle),
		uintptr(filterId),
	)
	if r != 0 {
		// Already gone is the outcome the caller wanted. Without this a
		// sublayer sweep would make every later delete look like a failure.
		if uint32(r) == fwpEFilterNotFound {
			return nil
		}
		return fmt.Errorf("FwpmFilterDeleteById0: %w", windows.Errno(r))
	}
	return nil
}

// sublayerFilterIds lists every filter currently in one of our sublayers,
// including ones this process never added.
//
// Filters added with an engine-assigned key are only ever reachable by the id
// the adding process held in memory, so a daemon that died without a Clear
// leaves permits nothing can name. Enumeration is how a fresh process finds
// them again.
func (e *wfpEngine) sublayerFilterIds(subLayer windows.GUID) ([]uint64, error) {
	var enumHandle windows.Handle
	r, _, _ := procFwpmFilterCreateEnumHandle0.Call(
		uintptr(e.handle),
		0, // no template: every layer, filtered by sublayer below
		uintptr(unsafe.Pointer(&enumHandle)),
	)
	if r != 0 {
		return nil, fmt.Errorf("FwpmFilterCreateEnumHandle0: %w", windows.Errno(r))
	}
	defer procFwpmFilterDestroyEnumHandle0.Call(uintptr(e.handle), uintptr(enumHandle))

	const batch = 256
	var ids []uint64
	for {
		var entries **fwpmFilter0
		var returned uint32
		r, _, _ = procFwpmFilterEnum0.Call(
			uintptr(e.handle),
			uintptr(enumHandle),
			batch,
			uintptr(unsafe.Pointer(&entries)),
			uintptr(unsafe.Pointer(&returned)),
		)
		if r != 0 {
			return nil, fmt.Errorf("FwpmFilterEnum0: %w", windows.Errno(r))
		}
		if returned == 0 {
			return ids, nil
		}
		for _, filter := range unsafe.Slice(entries, returned) {
			if filter != nil && filter.subLayerKey == subLayer {
				ids = append(ids, filter.filterId)
			}
		}
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
		if returned < batch {
			return ids, nil
		}
	}
}

// deleteFiltersInSublayer removes every filter in the sublayer, tracked or not.
func (e *wfpEngine) deleteFiltersInSublayer(subLayer windows.GUID) (int, error) {
	ids, err := e.sublayerFilterIds(subLayer)
	if err != nil {
		return 0, err
	}
	var errs []string
	deleted := 0
	for _, id := range ids {
		if err := e.deleteFilter(id); err != nil {
			errs = append(errs, fmt.Sprintf("%d: %v", id, err))
			continue
		}
		deleted++
	}
	if len(errs) > 0 {
		return deleted, fmt.Errorf("sublayer sweep: %s", strings.Join(errs, "; "))
	}
	return deleted, nil
}

// ---------------------------------------------------------------------------
// Kill switch filter builders
// ---------------------------------------------------------------------------

// addBlockAllOutbound and its inbound/IPv6 counterparts are persistent: they
// are the fail-closed lock itself and must outlive this process.
func (e *wfpEngine) addBlockAllOutbound() (uint64, error) {
	return e.addFilterKeyed(fwpmLayerAleAuthConnectV4, pangeaBlockAllOutboundV4FilterKey, "PangeaVPN Block All Outbound", 1, fwpActionBlock, fwpmFilterFlagPersistent, nil)
}

func (e *wfpEngine) addBlockAllInbound() (uint64, error) {
	return e.addFilterKeyed(fwpmLayerAleAuthRecvAcceptV4, pangeaBlockAllInboundV4FilterKey, "PangeaVPN Block All Inbound", 1, fwpActionBlock, fwpmFilterFlagPersistent, nil)
}

func (e *wfpEngine) addPermitLoopbackAt(layer, filterKey windows.GUID, filterName string) (uint64, error) {
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
	return e.addFilterKeyed(layer, filterKey, filterName, 10, fwpActionPermit, fwpmFilterFlagPersistent, conditions)
}

func (e *wfpEngine) addPermitLoopback() (uint64, error) {
	return e.addPermitLoopbackAt(fwpmLayerAleAuthConnectV4, pangeaPermitLoopbackV4FilterKey, "PangeaVPN Allow Loopback")
}

// addPermitLoopbackInboundV4 mirrors addPermitLoopbackInboundV6: without it
// the inbound block drops the server side of every 127.0.0.1 connection.
func (e *wfpEngine) addPermitLoopbackInboundV4() (uint64, error) {
	return e.addPermitLoopbackAt(fwpmLayerAleAuthRecvAcceptV4, pangeaPermitLoopbackInboundV4FilterKey, "PangeaVPN Allow Loopback Inbound")
}

// addPermitLoopbackSubnetAt permits 127.0.0.0/8 by address on the given
// layer — the IS_LOOPBACK flag alone misses fresh inter-process connects.
func (e *wfpEngine) addPermitLoopbackSubnetAt(layer, filterKey windows.GUID, filterName string) (uint64, error) {
	addrMask := fwpV4AddrAndMask{
		addr: uint32(127) << 24,
		mask: 0xFF000000, // /8
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
	id, err := e.addFilterKeyed(layer, filterKey, filterName, 10, fwpActionPermit, fwpmFilterFlagPersistent, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

func (e *wfpEngine) addPermitLoopbackSubnet() (uint64, error) {
	return e.addPermitLoopbackSubnetAt(fwpmLayerAleAuthConnectV4, pangeaPermitLoopbackNetV4FilterKey, "PangeaVPN Allow Loopback Subnet")
}

func (e *wfpEngine) addPermitLoopbackSubnetInboundV4() (uint64, error) {
	return e.addPermitLoopbackSubnetAt(fwpmLayerAleAuthRecvAcceptV4, pangeaPermitLoopbackNetInV4FilterKey, "PangeaVPN Allow Loopback Subnet Inbound")
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

// parseV4CIDRAddrMask converts an IPv4 CIDR string to the WFP condition's
// host-byte-order address/mask pair.
func parseV4CIDRAddrMask(cidr string) (fwpV4AddrAndMask, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return fwpV4AddrAndMask{}, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}
	ip := network.IP.To4()
	if ip == nil {
		return fwpV4AddrAndMask{}, fmt.Errorf("CIDR %s is not IPv4", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return fwpV4AddrAndMask{}, fmt.Errorf("CIDR %s has non-IPv4 mask", cidr)
	}
	var maskUint uint32
	if ones == 0 {
		maskUint = 0
	} else {
		maskUint = uint32(0xFFFFFFFF) << uint32(32-ones)
	}
	return fwpV4AddrAndMask{
		addr: uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3]),
		mask: maskUint,
	}, nil
}

// addPermitIPv4Subnet permits outbound to cidr — used for "Allow LAN".
func (e *wfpEngine) addPermitIPv4Subnet(cidr string) (uint64, error) {
	addrMask, err := parseV4CIDRAddrMask(cidr)
	if err != nil {
		return 0, err
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

// dhcpClientPortConditions matches the DHCP client's exchange on the wire:
// UDP between the client port (68, ours) and the server port (67, theirs).
func dhcpClientPortConditions() []fwpmFilterCondition0 {
	return []fwpmFilterCondition0{
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
}

// dhcpReplyConditions matches an OFFER/ACK arriving from any server or relay:
// it never comes from the broadcast address the request was sent to.
func dhcpReplyConditions() []fwpmFilterCondition0 {
	return dhcpClientPortConditions()
}

// addPermitDHCP permits UDP 68->67 scoped to remoteCIDR. Unscoped, this
// outranks block-all for ANY remote IP on port 67 — must stay scoped.
func (e *wfpEngine) addPermitDHCP(remoteCIDR string) (uint64, error) {
	addrMask, err := parseV4CIDRAddrMask(remoteCIDR)
	if err != nil {
		return 0, err
	}
	conditions := append(dhcpClientPortConditions(), fwpmFilterCondition0{
		fieldKey:  fwpmConditionIpRemoteAddress,
		matchType: fwpMatchEqual,
		conditionValue: fwpValue0{
			valueType: fwpV4AddrMask,
			value:     uintptr(unsafe.Pointer(&addrMask)),
		},
	})
	id, err := e.addFilter(fwpmLayerAleAuthConnectV4, "PangeaVPN Allow DHCP "+remoteCIDR, 10, fwpActionPermit, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

// addPermitDHCPInbound lets the lease reply through the inbound block. The
// request's flow cannot cover it: the reply's source is the server, not broadcast.
func (e *wfpEngine) addPermitDHCPInbound() (uint64, error) {
	return e.addFilter(fwpmLayerAleAuthRecvAcceptV4, "PangeaVPN Allow DHCP Reply", 10, fwpActionPermit, dhcpReplyConditions())
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
	return e.addFilterKeyed(fwpmLayerAleAuthConnectV6, pangeaBlockAllOutboundV6FilterKey, "PangeaVPN Block All Outbound IPv6", 1, fwpActionBlock, fwpmFilterFlagPersistent, nil)
}

func (e *wfpEngine) addBlockAllInboundV6() (uint64, error) {
	return e.addFilterKeyed(fwpmLayerAleAuthRecvAcceptV6, pangeaBlockAllInboundV6FilterKey, "PangeaVPN Block All Inbound IPv6", 1, fwpActionBlock, fwpmFilterFlagPersistent, nil)
}

func (e *wfpEngine) addPermitLoopbackV6() (uint64, error) {
	return e.addPermitLoopbackAt(fwpmLayerAleAuthConnectV6, pangeaPermitLoopbackV6FilterKey, "PangeaVPN Allow Loopback IPv6")
}

// addPermitLoopbackInboundV6 permits IPv6 traffic carrying the IS_LOOPBACK
// flag at the recv/accept layer. The inbound V6 block otherwise drops the
// server side of every [::1] connection — localhost resolves to ::1 first on
// Windows, so local web servers become unreachable while the kill switch is
// active.
func (e *wfpEngine) addPermitLoopbackInboundV6() (uint64, error) {
	return e.addPermitLoopbackAt(fwpmLayerAleAuthRecvAcceptV6, pangeaPermitLoopbackInboundV6FilterKey, "PangeaVPN Allow Loopback Inbound IPv6")
}

// addPermitLoopbackSubnetV6 permits ::1/128 by remote address on the given
// layer. Complements the IS_LOOPBACK flag permits, which are not reliably set
// for fresh inter-process TCP connects — the same quirk that required
// addPermitLoopbackSubnet on IPv4. Loopback is non-routable, so there is no
// leak risk.
func (e *wfpEngine) addPermitLoopbackSubnetV6(layer, filterKey windows.GUID, filterName string) (uint64, error) {
	addrMask := fwpV6AddrAndMask{prefixLength: 128}
	addrMask.addr[15] = 1 // ::1
	conditions := []fwpmFilterCondition0{
		{
			fieldKey:  fwpmConditionIpRemoteAddress,
			matchType: fwpMatchEqual,
			conditionValue: fwpValue0{
				valueType: fwpV6AddrMask,
				value:     uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	id, err := e.addFilterKeyed(layer, filterKey, filterName, 10, fwpActionPermit, fwpmFilterFlagPersistent, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}
