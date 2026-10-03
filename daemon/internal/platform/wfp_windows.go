//go:build windows && (amd64 || arm64)

package platform

import (
	"fmt"
	"net"
	"runtime"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// fwpuclnt.dll — Windows Filtering Platform user-mode API.
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
	procFwpmFilterGetByKey0      = modFwpuclnt.NewProc("FwpmFilterGetByKey0")

	procFwpmFilterCreateEnumHandle0  = modFwpuclnt.NewProc("FwpmFilterCreateEnumHandle0")
	procFwpmFilterEnum0              = modFwpuclnt.NewProc("FwpmFilterEnum0")
	procFwpmFilterDestroyEnumHandle0 = modFwpuclnt.NewProc("FwpmFilterDestroyEnumHandle0")
	procFwpmFreeMemory0              = modFwpuclnt.NewProc("FwpmFreeMemory0")

	procFwpmGetAppIdFromFileName0 = modFwpuclnt.NewProc("FwpmGetAppIdFromFileName0")
)

// fwperror.h codes; the vendored types stop at fwpmtypes.h and fwptypes.h.
const (
	fwpEAlreadyExists    uint32 = 0x80320009 // FWP_E_ALREADY_EXISTS
	fwpEFilterNotFound   uint32 = 0x80320003 // FWP_E_FILTER_NOT_FOUND
	fwpESublayerNotFound uint32 = 0x80320007 // FWP_E_SUBLAYER_NOT_FOUND
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

// DNS and DHCP are part of the lock itself: a locked boot must still get a
// lease, and must never resolve names outside the tunnel.
var (
	pangeaBlockDNSUDPV4FilterKey   = windows.GUID{Data1: 0xa9d3e8fe, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x7c}}
	pangeaBlockDNSTCPV4FilterKey   = windows.GUID{Data1: 0xa9d3e8ff, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x7d}}
	pangeaPermitDHCPOutV4FilterKey = windows.GUID{Data1: 0xa9d3e900, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x7e}}
	pangeaPermitDHCPInV4FilterKey  = windows.GUID{Data1: 0xa9d3e901, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x7f}}
)

// DNS over TLS or QUIC to a LAN resolver is the same Allow-LAN hole as plain DNS.
var (
	pangeaBlockDoTUDPV4FilterKey = windows.GUID{Data1: 0xa9d3e904, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x82}}
	pangeaBlockDoTTCPV4FilterKey = windows.GUID{Data1: 0xa9d3e905, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x83}}
)

// Traffic the host forwards for WSL2, Hyper-V NAT or ICS guests never reaches
// the ALE layers, so the lock has to block it at the IPFORWARD layers too.
var (
	pangeaBlockForwardV4FilterKey = windows.GUID{Data1: 0xa9d3e902, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x80}}
	pangeaBlockForwardV6FilterKey = windows.GUID{Data1: 0xa9d3e903, Data2: 0x4b7c, Data3: 0x4d2a, Data4: [8]byte{0x9e, 0x6f, 0x1a, 0x2b, 0x3c, 0x4d, 0x5e, 0x81}}
)

// bootTimeKeyData2 replaces Data2 (0x4b7c in every persistent key) so the
// boot-time twin of a filter gets a key BFE will accept beside the original.
const bootTimeKeyData2 = 0x4b7d

// bootTimeVariant derives the boot-time twin of a persistent filter, which is
// enforced from stack start until BFE loads the persistent set.
func bootTimeVariant(key windows.GUID, flags wtFwpmFilterFlags) (windows.GUID, wtFwpmFilterFlags) {
	key.Data2 = bootTimeKeyData2
	return key, (flags &^ cFWPM_FILTER_FLAG_PERSISTENT) | cFWPM_FILTER_FLAG_BOOTTIME
}

// Filter weights inside the sublayer; higher wins. Trusted permits sit above
// the DNS block so loopback, the endpoints and the tunnel still resolve.
const (
	weightBlockAll      uint8 = 1
	weightLANPermit     uint8 = 10
	weightSplitPermit   uint8 = 10
	weightDNSBlock      uint8 = 11
	weightTrustedPermit uint8 = 12
)

const (
	dnsPort = 53
	dotPort = 853
)

// wfpEngine wraps a WFP engine handle.
type wfpEngine struct {
	handle windows.Handle
	// bootTime makes every keyed add write the filter's boot-time twin instead.
	bootTime bool
	// calls is nil for the real BFE; tests put an in-memory engine here.
	calls wfpCalls
}

// bootTimeView shares the handle; never close it, close the owner instead.
func (e *wfpEngine) bootTimeView() *wfpEngine {
	return &wfpEngine{handle: e.handle, bootTime: true, calls: e.calls}
}

func (e *wfpEngine) sys() wfpCalls {
	if e.calls != nil {
		return e.calls
	}
	return bfeCalls{}
}

// wfpCalls is the raw BFE surface the engine drives, returning the API's status
// codes, so the kill switch can be tested without ever reaching the real BFE.
type wfpCalls interface {
	engineClose(h windows.Handle) uintptr
	transactionBegin(h windows.Handle) uintptr
	transactionCommit(h windows.Handle) uintptr
	transactionAbort(h windows.Handle) uintptr
	subLayerAdd(h windows.Handle, sublayer *wtFwpmSublayer0) uintptr
	subLayerDeleteByKey(h windows.Handle, key *windows.GUID) uintptr
	filterAdd(h windows.Handle, filter *wtFwpmFilter0, id *uint64) uintptr
	filterDeleteByID(h windows.Handle, id uint64) uintptr
	filterDeleteByKey(h windows.Handle, key *windows.GUID) uintptr
	filterExistsByKey(h windows.Handle, key *windows.GUID) (bool, uintptr)
	// filters lists every installed filter with only its ID, sublayer and flags set.
	filters(h windows.Handle) ([]wtFwpmFilter0, error)
}

type bfeCalls struct{}

func (bfeCalls) engineClose(h windows.Handle) uintptr {
	r, _, _ := procFwpmEngineClose0.Call(uintptr(h))
	return r
}

func (bfeCalls) transactionBegin(h windows.Handle) uintptr {
	r, _, _ := procFwpmTransactionBegin0.Call(uintptr(h), 0)
	return r
}

func (bfeCalls) transactionCommit(h windows.Handle) uintptr {
	r, _, _ := procFwpmTransactionCommit0.Call(uintptr(h))
	return r
}

func (bfeCalls) transactionAbort(h windows.Handle) uintptr {
	r, _, _ := procFwpmTransactionAbort0.Call(uintptr(h))
	return r
}

func (bfeCalls) subLayerAdd(h windows.Handle, sublayer *wtFwpmSublayer0) uintptr {
	r, _, _ := procFwpmSubLayerAdd0.Call(uintptr(h), uintptr(unsafe.Pointer(sublayer)), 0)
	return r
}

func (bfeCalls) subLayerDeleteByKey(h windows.Handle, key *windows.GUID) uintptr {
	r, _, _ := procFwpmSubLayerDeleteByKey0.Call(uintptr(h), uintptr(unsafe.Pointer(key)))
	return r
}

func (bfeCalls) filterAdd(h windows.Handle, filter *wtFwpmFilter0, id *uint64) uintptr {
	r, _, _ := procFwpmFilterAdd0.Call(uintptr(h), uintptr(unsafe.Pointer(filter)), 0, uintptr(unsafe.Pointer(id)))
	return r
}

func (bfeCalls) filterDeleteByID(h windows.Handle, id uint64) uintptr {
	r, _, _ := procFwpmFilterDeleteById0.Call(uintptr(h), uintptr(id))
	return r
}

func (bfeCalls) filterDeleteByKey(h windows.Handle, key *windows.GUID) uintptr {
	r, _, _ := procFwpmFilterDeleteByKey0.Call(uintptr(h), uintptr(unsafe.Pointer(key)))
	return r
}

func (bfeCalls) filterExistsByKey(h windows.Handle, key *windows.GUID) (bool, uintptr) {
	var filter *wtFwpmFilter0
	r, _, _ := procFwpmFilterGetByKey0.Call(uintptr(h), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&filter)))
	if r != 0 {
		return false, r
	}
	if filter != nil {
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&filter)))
	}
	return true, 0
}

func (bfeCalls) filters(h windows.Handle) ([]wtFwpmFilter0, error) {
	var enumHandle windows.Handle
	r, _, _ := procFwpmFilterCreateEnumHandle0.Call(
		uintptr(h),
		0, // no template: every layer, filtered by sublayer by the caller
		uintptr(unsafe.Pointer(&enumHandle)),
	)
	if r != 0 {
		return nil, fmt.Errorf("FwpmFilterCreateEnumHandle0: %w", windows.Errno(r))
	}
	defer procFwpmFilterDestroyEnumHandle0.Call(uintptr(h), uintptr(enumHandle))

	const batch = 256
	var out []wtFwpmFilter0
	for {
		var entries **wtFwpmFilter0
		var returned uint32
		r, _, _ = procFwpmFilterEnum0.Call(
			uintptr(h),
			uintptr(enumHandle),
			batch,
			uintptr(unsafe.Pointer(&entries)),
			uintptr(unsafe.Pointer(&returned)),
		)
		if r != 0 {
			return nil, fmt.Errorf("FwpmFilterEnum0: %w", windows.Errno(r))
		}
		if returned == 0 {
			return out, nil
		}
		// Copied out field by field: the entries die with FwpmFreeMemory0 below.
		for _, filter := range unsafe.Slice(entries, returned) {
			if filter != nil {
				out = append(out, wtFwpmFilter0{filterID: filter.filterID, subLayerKey: filter.subLayerKey, flags: filter.flags})
			}
		}
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
		if returned < batch {
			return out, nil
		}
	}
}

// wfpAppID is replaced by tests; the real one needs the image to exist on disk.
var wfpAppID = appIDFromFileName

// appIDFromFileName returns BFE's app id for an image, its lower-cased NT device
// path. free releases the BFE-allocated blob once every filter using it is added.
func appIDFromFileName(path string) (blob *wtFwpByteBlob, free func(), err error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, nil, fmt.Errorf("app id path: %w", err)
	}
	r, _, _ := procFwpmGetAppIdFromFileName0.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&blob)))
	runtime.KeepAlive(name)
	if r != 0 {
		return nil, nil, fmt.Errorf("FwpmGetAppIdFromFileName0: %w", windows.Errno(r))
	}
	return blob, func() { procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&blob))) }, nil
}

func wfpOpen() (*wfpEngine, error) {
	name, err := windows.UTF16PtrFromString("PangeaVPN Kill Switch")
	if err != nil {
		return nil, fmt.Errorf("session display name: %w", err)
	}

	// Static (non-dynamic) session: filters survive this process dying, so a
	// crash/kill/OOM doesn't silently open the firewall.
	session := wtFwpmSession0{}
	session.displayData.name = name

	var handle windows.Handle
	open := func() uintptr {
		r, _, _ := procFwpmEngineOpen0.Call(
			0,
			uintptr(cRPC_C_AUTHN_WINNT),
			0,
			uintptr(unsafe.Pointer(&session)),
			uintptr(unsafe.Pointer(&handle)),
		)
		runtime.KeepAlive(name)
		runtime.KeepAlive(&session)
		return r
	}
	r := open()
	if r != 0 {
		// The usual cause is a stopped or disabled BFE; bring it back and retry once.
		started, bfeErr := ensureBFERunning()
		if bfeErr != nil {
			return nil, fmt.Errorf("FwpmEngineOpen0: %w; the Windows Base Filtering Engine (BFE) service is not running and could not be started: %v", windows.Errno(r), bfeErr)
		}
		if started {
			KillSwitchWarn("kill switch: Base Filtering Engine service was stopped or disabled; started it")
			r = open()
		}
	}
	if r != 0 {
		return nil, fmt.Errorf("FwpmEngineOpen0: %w", windows.Errno(r))
	}

	return &wfpEngine{handle: handle}, nil
}

func (e *wfpEngine) close() error {
	if e.handle == 0 {
		return nil
	}
	r := e.sys().engineClose(e.handle)
	e.handle = 0
	if r != 0 {
		return fmt.Errorf("FwpmEngineClose0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) beginTransaction() error {
	if r := e.sys().transactionBegin(e.handle); r != 0 {
		return fmt.Errorf("FwpmTransactionBegin0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) commitTransaction() error {
	if r := e.sys().transactionCommit(e.handle); r != 0 {
		return fmt.Errorf("FwpmTransactionCommit0: %w", windows.Errno(r))
	}
	return nil
}

func (e *wfpEngine) abortTransaction() {
	e.sys().transactionAbort(e.handle)
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

	sublayer := wtFwpmSublayer0{
		subLayerKey: pangeaVPNSublayerKey,
		displayData: wtFwpmDisplayData0{
			name:        name,
			description: desc,
		},
		flags:  cFWPM_SUBLAYER_FLAG_PERSISTENT,
		weight: 0xFFFF, // highest priority sublayer
	}

	r := e.sys().subLayerAdd(e.handle, &sublayer)
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
	if r := e.sys().subLayerDeleteByKey(e.handle, &key); r != 0 {
		if uint32(r) == fwpESublayerNotFound {
			return nil
		}
		return fmt.Errorf("FwpmSubLayerDeleteByKey0: %w", windows.Errno(r))
	}
	return nil
}

// addFilter adds an ephemeral, engine-assigned-key filter. See addFilterKeyed
// for filters that must be idempotent and outlive this process.
func (e *wfpEngine) addFilter(layer windows.GUID, filterName string, weight uint8, action wtFwpActionType, conditions []wtFwpmFilterCondition0) (uint64, error) {
	return e.addFilterKeyed(layer, windows.GUID{}, filterName, weight, action, 0, conditions)
}

// addFilterKeyed adds a filter under a caller-chosen key so a later process
// can find/delete it by that key. Re-adding an existing key returns (0, nil).
func (e *wfpEngine) addFilterKeyed(layer, filterKey windows.GUID, filterName string, weight uint8, action wtFwpActionType, flags wtFwpmFilterFlags, conditions []wtFwpmFilterCondition0) (uint64, error) {
	namePtr, err := windows.UTF16PtrFromString(filterName)
	if err != nil {
		return 0, fmt.Errorf("filter name %q: %w", filterName, err)
	}
	if e.bootTime && filterKey != (windows.GUID{}) {
		filterKey, flags = bootTimeVariant(filterKey, flags)
	}

	filter := wtFwpmFilter0{
		filterKey: filterKey,
		displayData: wtFwpmDisplayData0{
			name: namePtr,
		},
		flags:       flags,
		layerKey:    layer,
		subLayerKey: pangeaVPNSublayerKey,
		weight: wtFwpValue0{
			_type: cFWP_UINT8,
			value: uintptr(weight),
		},
		action: wtFwpmAction0{
			_type: action,
		},
		numFilterConditions: uint32(len(conditions)),
	}

	if len(conditions) > 0 {
		filter.filterCondition = &conditions[0]
	}

	var filterId uint64
	r := e.sys().filterAdd(e.handle, &filter, &filterId)
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
	if r := e.sys().filterDeleteByKey(e.handle, &key); r != 0 {
		if uint32(r) == fwpEFilterNotFound {
			return nil
		}
		return fmt.Errorf("FwpmFilterDeleteByKey0: %w", windows.Errno(r))
	}
	return nil
}

// filterExistsByKey asks BFE whether a keyed filter is installed, which is
// how a fresh process learns the lock a previous one left is still live.
func (e *wfpEngine) filterExistsByKey(key windows.GUID) (bool, error) {
	found, r := e.sys().filterExistsByKey(e.handle, &key)
	if r != 0 {
		if uint32(r) == fwpEFilterNotFound {
			return false, nil
		}
		return false, fmt.Errorf("FwpmFilterGetByKey0: %w", windows.Errno(r))
	}
	return found, nil
}

func (e *wfpEngine) deleteFilter(filterId uint64) error {
	if r := e.sys().filterDeleteByID(e.handle, filterId); r != 0 {
		// Already gone is the outcome the caller wanted. Without this a
		// sublayer sweep would make every later delete look like a failure.
		if uint32(r) == fwpEFilterNotFound {
			return nil
		}
		return fmt.Errorf("FwpmFilterDeleteById0: %w", windows.Errno(r))
	}
	return nil
}

// sublayerFilterIds lists every filter in one of our sublayers, including
// engine-keyed ones a dead process left behind that only enumeration can name.
func (e *wfpEngine) sublayerFilterIds(subLayer windows.GUID, keep func(*wtFwpmFilter0) bool) ([]uint64, error) {
	all, err := e.sys().filters(e.handle)
	if err != nil {
		return nil, err
	}
	var ids []uint64
	for i := range all {
		if all[i].subLayerKey == subLayer && keep(&all[i]) {
			ids = append(ids, all[i].filterID)
		}
	}
	return ids, nil
}

// anyFilter keeps every filter in the sublayer; Clear tears the lot down.
func anyFilter(*wtFwpmFilter0) bool { return true }

// isEphemeralFilter is true for engine-keyed permits (endpoint, LAN, DHCP,
// tunnel): what a dead process leaves behind, unlike the persistent lock.
func isEphemeralFilter(f *wtFwpmFilter0) bool {
	return f.flags&(cFWPM_FILTER_FLAG_PERSISTENT|cFWPM_FILTER_FLAG_BOOTTIME) == 0
}

// deleteFiltersInSublayer removes every filter in the sublayer that keep
// accepts, tracked by this process or not.
func (e *wfpEngine) deleteFiltersInSublayer(subLayer windows.GUID, keep func(*wtFwpmFilter0) bool) (int, error) {
	ids, err := e.sublayerFilterIds(subLayer, keep)
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

// addBlockAllOutbound and its inbound/IPv6 counterparts are persistent: they
// are the fail-closed lock itself and must outlive this process.
func (e *wfpEngine) addBlockAllOutbound() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaBlockAllOutboundV4FilterKey, "PangeaVPN Block All Outbound", weightBlockAll, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, nil)
}

func (e *wfpEngine) addBlockAllInbound() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, pangeaBlockAllInboundV4FilterKey, "PangeaVPN Block All Inbound", weightBlockAll, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, nil)
}

func (e *wfpEngine) addPermitLoopbackAt(layer, filterKey windows.GUID, filterName string) (uint64, error) {
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_FLAGS,
			matchType: cFWP_MATCH_FLAGS_ALL_SET,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT32,
				value: uintptr(cFWP_CONDITION_FLAG_IS_LOOPBACK),
			},
		},
	}
	return e.addFilterKeyed(layer, filterKey, filterName, weightTrustedPermit, cFWP_ACTION_PERMIT, cFWPM_FILTER_FLAG_PERSISTENT, conditions)
}

func (e *wfpEngine) addPermitLoopback() (uint64, error) {
	return e.addPermitLoopbackAt(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaPermitLoopbackV4FilterKey, "PangeaVPN Allow Loopback")
}

// addPermitLoopbackInboundV4 mirrors addPermitLoopbackInboundV6: without it
// the inbound block drops the server side of every 127.0.0.1 connection.
func (e *wfpEngine) addPermitLoopbackInboundV4() (uint64, error) {
	return e.addPermitLoopbackAt(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, pangeaPermitLoopbackInboundV4FilterKey, "PangeaVPN Allow Loopback Inbound")
}

// addPermitLoopbackSubnetAt permits 127.0.0.0/8 by address on the given
// layer — the IS_LOOPBACK flag alone misses fresh inter-process connects.
func (e *wfpEngine) addPermitLoopbackSubnetAt(layer, filterKey windows.GUID, filterName string) (uint64, error) {
	addrMask := wtFwpV4AddrAndMask{
		addr: uint32(127) << 24,
		mask: 0xFF000000, // /8
	}
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_V4_ADDR_MASK,
				value: uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	id, err := e.addFilterKeyed(layer, filterKey, filterName, weightTrustedPermit, cFWP_ACTION_PERMIT, cFWPM_FILTER_FLAG_PERSISTENT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

func (e *wfpEngine) addPermitLoopbackSubnet() (uint64, error) {
	return e.addPermitLoopbackSubnetAt(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaPermitLoopbackNetV4FilterKey, "PangeaVPN Allow Loopback Subnet")
}

func (e *wfpEngine) addPermitLoopbackSubnetInboundV4() (uint64, error) {
	return e.addPermitLoopbackSubnetAt(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, pangeaPermitLoopbackNetInV4FilterKey, "PangeaVPN Allow Loopback Subnet Inbound")
}

// addPermitEndpointIP permits the endpoint at both ALE layers: a UDP
// transport's replies are classified at RECV_ACCEPT_V4, not just CONNECT_V4.
func (e *wfpEngine) addPermitEndpointIP(ipStr string) ([]uint64, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid IPv4 address: %s", ipStr)
	}

	addrMask := wtFwpV4AddrAndMask{
		addr: uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3]),
		mask: 0xFFFFFFFF,
	}

	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_V4_ADDR_MASK,
				value: uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}

	layers := []struct {
		layer windows.GUID
		name  string
	}{
		{cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "PangeaVPN Allow Endpoint " + ipStr},
		{cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, "PangeaVPN Allow Endpoint Inbound " + ipStr},
	}
	ids := make([]uint64, 0, len(layers))
	for _, l := range layers {
		id, err := e.addFilter(l.layer, l.name, weightTrustedPermit, cFWP_ACTION_PERMIT, conditions)
		if err != nil {
			runtime.KeepAlive(&addrMask)
			return ids, err
		}
		ids = append(ids, id)
	}
	runtime.KeepAlive(&addrMask)
	return ids, nil
}

// parseV4CIDRAddrMask converts an IPv4 CIDR string to the WFP condition's
// host-byte-order address/mask pair.
func parseV4CIDRAddrMask(cidr string) (wtFwpV4AddrAndMask, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return wtFwpV4AddrAndMask{}, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
	}
	ip := network.IP.To4()
	if ip == nil {
		return wtFwpV4AddrAndMask{}, fmt.Errorf("CIDR %s is not IPv4", cidr)
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return wtFwpV4AddrAndMask{}, fmt.Errorf("CIDR %s has non-IPv4 mask", cidr)
	}
	var maskUint uint32
	if ones == 0 {
		maskUint = 0
	} else {
		maskUint = uint32(0xFFFFFFFF) << uint32(32-ones)
	}
	return wtFwpV4AddrAndMask{
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

	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_V4_ADDR_MASK,
				value: uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	id, err := e.addFilter(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "PangeaVPN Allow LAN "+cidr, weightLANPermit, cFWP_ACTION_PERMIT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

// dhcpClientPortConditions matches the DHCP client's exchange on the wire:
// UDP between the client port (68, ours) and the server port (67, theirs).
func dhcpClientPortConditions() []wtFwpmFilterCondition0 {
	return []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_PROTOCOL,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT8,
				value: uintptr(cIPPROTO_UDP),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(67),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_LOCAL_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(68),
			},
		},
	}
}

// dhcpReplyConditions matches an OFFER/ACK arriving from any server or relay:
// it never comes from the broadcast address the request was sent to.
func dhcpReplyConditions() []wtFwpmFilterCondition0 {
	return dhcpClientPortConditions()
}

// addPermitDHCP permits UDP 68->67 scoped to remoteCIDR. Unscoped, this
// outranks block-all for ANY remote IP on port 67 — must stay scoped.
func (e *wfpEngine) addPermitDHCP(remoteCIDR string) (uint64, error) {
	addrMask, err := parseV4CIDRAddrMask(remoteCIDR)
	if err != nil {
		return 0, err
	}
	conditions := append(dhcpClientPortConditions(), wtFwpmFilterCondition0{
		fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
		matchType: cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_V4_ADDR_MASK,
			value: uintptr(unsafe.Pointer(&addrMask)),
		},
	})
	id, err := e.addFilter(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "PangeaVPN Allow DHCP "+remoteCIDR, weightLANPermit, cFWP_ACTION_PERMIT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

// addPermitDHCPBroadcast is the persistent request-side permit: UDP 68->67 to
// 255.255.255.255 only, so a locked boot can still take a lease.
func (e *wfpEngine) addPermitDHCPBroadcast() (uint64, error) {
	addrMask, err := parseV4CIDRAddrMask("255.255.255.255/32")
	if err != nil {
		return 0, err
	}
	conditions := append(dhcpClientPortConditions(), wtFwpmFilterCondition0{
		fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
		matchType: cFWP_MATCH_EQUAL,
		conditionValue: wtFwpConditionValue0{
			_type: cFWP_V4_ADDR_MASK,
			value: uintptr(unsafe.Pointer(&addrMask)),
		},
	})
	id, err := e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaPermitDHCPOutV4FilterKey, "PangeaVPN Allow DHCP Broadcast", weightLANPermit, cFWP_ACTION_PERMIT, cFWPM_FILTER_FLAG_PERSISTENT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

// addPermitDHCPInbound lets the lease reply through the inbound block. The
// request's flow cannot cover it: the reply's source is the server, not broadcast.
func (e *wfpEngine) addPermitDHCPInbound() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, pangeaPermitDHCPInV4FilterKey, "PangeaVPN Allow DHCP Reply", weightLANPermit, cFWP_ACTION_PERMIT, cFWPM_FILTER_FLAG_PERSISTENT, dhcpReplyConditions())
}

// dnsBlockConditions matches one resolver port over one transport protocol. Only
// the permits above weightDNSBlock (loopback, endpoint, tunnel) get past it.
func dnsBlockConditions(proto wtIPProto, port uint16) []wtFwpmFilterCondition0 {
	return []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_PROTOCOL,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT8,
				value: uintptr(proto),
			},
		},
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_PORT,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT16,
				value: uintptr(port),
			},
		},
	}
}

// addBlockDNSUDP and addBlockDNSTCP close the Allow-LAN DNS hole: the router
// is a resolver, and Windows will happily ask it beside the tunnel's.
func (e *wfpEngine) addBlockDNSUDP() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaBlockDNSUDPV4FilterKey, "PangeaVPN Block DNS UDP", weightDNSBlock, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, dnsBlockConditions(cIPPROTO_UDP, dnsPort))
}

func (e *wfpEngine) addBlockDNSTCP() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaBlockDNSTCPV4FilterKey, "PangeaVPN Block DNS TCP", weightDNSBlock, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, dnsBlockConditions(cIPPROTO_TCP, dnsPort))
}

// addBlockDoTUDP and addBlockDoTTCP do the same for DNS over QUIC and TLS.
func (e *wfpEngine) addBlockDoTUDP() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaBlockDoTUDPV4FilterKey, "PangeaVPN Block DoT UDP", weightDNSBlock, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, dnsBlockConditions(cIPPROTO_UDP, dotPort))
}

func (e *wfpEngine) addBlockDoTTCP() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaBlockDoTTCPV4FilterKey, "PangeaVPN Block DoT TCP", weightDNSBlock, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, dnsBlockConditions(cIPPROTO_TCP, dotPort))
}

// addPermitTunnelInterface permits the tunnel at both ALE layers, like the
// endpoint permit: a filter change anywhere re-authorises inbound UDP at RECV_ACCEPT.
func (e *wfpEngine) addPermitTunnelInterface(luid uint64) ([]uint64, error) {
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_LOCAL_INTERFACE,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT64,
				value: uintptr(unsafe.Pointer(&luid)),
			},
		},
	}
	layers := []struct {
		layer windows.GUID
		name  string
	}{
		{cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "PangeaVPN Allow Tunnel Interface"},
		{cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, "PangeaVPN Allow Tunnel Interface Inbound"},
	}
	ids := make([]uint64, 0, len(layers))
	for _, l := range layers {
		id, err := e.addFilter(l.layer, l.name, weightTrustedPermit, cFWP_ACTION_PERMIT, conditions)
		if err != nil {
			runtime.KeepAlive(&luid)
			return ids, err
		}
		ids = append(ids, id)
	}
	runtime.KeepAlive(&luid)
	return ids, nil
}

func (e *wfpEngine) addBlockAllOutboundV6() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V6, pangeaBlockAllOutboundV6FilterKey, "PangeaVPN Block All Outbound IPv6", weightBlockAll, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, nil)
}

func (e *wfpEngine) addBlockAllInboundV6() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6, pangeaBlockAllInboundV6FilterKey, "PangeaVPN Block All Inbound IPv6", weightBlockAll, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, nil)
}

func (e *wfpEngine) addPermitLoopbackV6() (uint64, error) {
	return e.addPermitLoopbackAt(cFWPM_LAYER_ALE_AUTH_CONNECT_V6, pangeaPermitLoopbackV6FilterKey, "PangeaVPN Allow Loopback IPv6")
}

// addPermitLoopbackInboundV6 mirrors addPermitLoopbackInboundV4: without it
// the inbound V6 block drops the server side of every [::1] connection.
func (e *wfpEngine) addPermitLoopbackInboundV6() (uint64, error) {
	return e.addPermitLoopbackAt(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6, pangeaPermitLoopbackInboundV6FilterKey, "PangeaVPN Allow Loopback Inbound IPv6")
}

// addPermitLoopbackSubnetV6 permits ::1/128 by remote address, the IPv6 twin
// of addPermitLoopbackSubnet — the IS_LOOPBACK flag alone misses fresh connects.
func (e *wfpEngine) addPermitLoopbackSubnetV6(layer, filterKey windows.GUID, filterName string) (uint64, error) {
	addrMask := wtFwpV6AddrAndMask{prefixLength: 128}
	addrMask.addr[15] = 1 // ::1
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_V6_ADDR_MASK,
				value: uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	id, err := e.addFilterKeyed(layer, filterKey, filterName, weightTrustedPermit, cFWP_ACTION_PERMIT, cFWPM_FILTER_FLAG_PERSISTENT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

// addBlockAllForwardV4 and its IPv6 twin close the path the ALE blocks never
// see: packets the host forwards for WSL2, Hyper-V NAT or ICS guests.
func (e *wfpEngine) addBlockAllForwardV4() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_IPFORWARD_V4, pangeaBlockForwardV4FilterKey, "PangeaVPN Block Forwarded IPv4", weightBlockAll, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, nil)
}

func (e *wfpEngine) addBlockAllForwardV6() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_IPFORWARD_V6, pangeaBlockForwardV6FilterKey, "PangeaVPN Block Forwarded IPv6", weightBlockAll, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, nil)
}

// forwardToInterfaceConditions matches a forwarded packet about to leave via
// the interface with this index; only the tunnel's is ever permitted.
func forwardToInterfaceConditions(ifIndex uint32) []wtFwpmFilterCondition0 {
	return []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_DESTINATION_INTERFACE_INDEX,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT32,
				value: uintptr(ifIndex),
			},
		},
	}
}

func (e *wfpEngine) addPermitForwardToInterface(ifIndex uint32) (uint64, error) {
	return e.addFilter(cFWPM_LAYER_IPFORWARD_V4, "PangeaVPN Allow Forwarding To Tunnel", weightTrustedPermit, cFWP_ACTION_PERMIT, forwardToInterfaceConditions(ifIndex))
}

// addPermitForwardIPv4Subnet is the forward-layer half of Allow LAN: a guest
// keeps reaching the local network while the lock holds, as the host does.
func (e *wfpEngine) addPermitForwardIPv4Subnet(cidr string) (uint64, error) {
	return e.addPermitForwardTo(cidr, "PangeaVPN Allow Forwarding To LAN "+cidr, weightLANPermit)
}

// addPermitForwardSplitCIDR lets guests reach an excluded range the way the host
// can. The forward layer has no ports, so guest DNS to it is not blocked.
func (e *wfpEngine) addPermitForwardSplitCIDR(cidr string) (uint64, error) {
	return e.addPermitForwardTo(cidr, "PangeaVPN Allow Forwarding To Split Range "+cidr, weightSplitPermit)
}

func (e *wfpEngine) addPermitForwardTo(cidr, name string, weight uint8) (uint64, error) {
	addrMask, err := parseV4CIDRAddrMask(cidr)
	if err != nil {
		return 0, err
	}
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_DESTINATION_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_V4_ADDR_MASK,
				value: uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	id, err := e.addFilter(cFWPM_LAYER_IPFORWARD_V4, name, weight, cFWP_ACTION_PERMIT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}

// addALEPermitPair adds name at CONNECT_V4 and its twin at RECV_ACCEPT_V4, where
// UDP replies and every flow re-authorised after a filter change are judged.
func (e *wfpEngine) addALEPermitPair(name string, weight uint8, conditions []wtFwpmFilterCondition0) ([]uint64, error) {
	ids := make([]uint64, 0, 2)
	for _, l := range []struct {
		layer windows.GUID
		name  string
	}{
		{cFWPM_LAYER_ALE_AUTH_CONNECT_V4, name},
		{cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, name + " Inbound"},
	} {
		id, err := e.addFilter(l.layer, l.name, weight, cFWP_ACTION_PERMIT, conditions)
		if err != nil {
			return ids, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// splitEgressConditions match the daemon image's sockets anywhere but the tunnel,
// which has its own permit. tunnelLUID 0 leaves the interface unconstrained.
func splitEgressConditions(appID *wtFwpByteBlob, tunnelLUID *uint64) []wtFwpmFilterCondition0 {
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_ALE_APP_ID,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_BYTE_BLOB_TYPE,
				value: uintptr(unsafe.Pointer(appID)),
			},
		},
	}
	if *tunnelLUID != 0 {
		conditions = append(conditions, wtFwpmFilterCondition0{
			fieldKey:  cFWPM_CONDITION_IP_LOCAL_INTERFACE,
			matchType: cFWP_MATCH_NOT_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_UINT64,
				value: uintptr(unsafe.Pointer(tunnelLUID)),
			},
		})
	}
	return conditions
}

// addPermitSplitEgress lets the sockets that carry bypassed flows out the NIC.
// Weight 10 keeps the DNS block over them, as for the LAN permits.
func (e *wfpEngine) addPermitSplitEgress(appID *wtFwpByteBlob, tunnelLUID uint64) ([]uint64, error) {
	luid := &tunnelLUID
	ids, err := e.addALEPermitPair("PangeaVPN Allow Split Tunnel Egress", weightSplitPermit, splitEgressConditions(appID, luid))
	runtime.KeepAlive(appID)
	runtime.KeepAlive(luid)
	return ids, err
}

// addPermitSplitCIDR permits an excluded destination range both ways, under the
// DNS block so a resolver inside it still cannot be reached off-tunnel.
func (e *wfpEngine) addPermitSplitCIDR(cidr string) ([]uint64, error) {
	addrMask, err := parseV4CIDRAddrMask(cidr)
	if err != nil {
		return nil, err
	}
	conditions := []wtFwpmFilterCondition0{
		{
			fieldKey:  cFWPM_CONDITION_IP_REMOTE_ADDRESS,
			matchType: cFWP_MATCH_EQUAL,
			conditionValue: wtFwpConditionValue0{
				_type: cFWP_V4_ADDR_MASK,
				value: uintptr(unsafe.Pointer(&addrMask)),
			},
		},
	}
	ids, err := e.addALEPermitPair("PangeaVPN Allow Split Range "+cidr, weightSplitPermit, conditions)
	runtime.KeepAlive(&addrMask)
	return ids, err
}
