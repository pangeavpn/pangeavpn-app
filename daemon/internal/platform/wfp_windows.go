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
	procFwpmFilterGetByKey0      = modFwpuclnt.NewProc("FwpmFilterGetByKey0")

	procFwpmFilterCreateEnumHandle0  = modFwpuclnt.NewProc("FwpmFilterCreateEnumHandle0")
	procFwpmFilterEnum0              = modFwpuclnt.NewProc("FwpmFilterEnum0")
	procFwpmFilterDestroyEnumHandle0 = modFwpuclnt.NewProc("FwpmFilterDestroyEnumHandle0")
	procFwpmFreeMemory0              = modFwpuclnt.NewProc("FwpmFreeMemory0")
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
	weightDNSBlock      uint8 = 11
	weightTrustedPermit uint8 = 12
)

const dnsPort = 53

// ---------------------------------------------------------------------------
// wfpEngine wraps a WFP engine handle
// ---------------------------------------------------------------------------

type wfpEngine struct {
	handle windows.Handle
	// bootTime makes every keyed add write the filter's boot-time twin instead.
	bootTime bool
}

// bootTimeView shares the handle; never close it, close the owner instead.
func (e *wfpEngine) bootTimeView() *wfpEngine {
	return &wfpEngine{handle: e.handle, bootTime: true}
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
	r, _, _ := procFwpmEngineOpen0.Call(
		0,
		uintptr(cRPC_C_AUTHN_WINNT),
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

	sublayer := wtFwpmSublayer0{
		subLayerKey: pangeaVPNSublayerKey,
		displayData: wtFwpmDisplayData0{
			name:        name,
			description: desc,
		},
		flags:  cFWPM_SUBLAYER_FLAG_PERSISTENT,
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

// filterExistsByKey asks BFE whether a keyed filter is installed, which is
// how a fresh process learns the lock a previous one left is still live.
func (e *wfpEngine) filterExistsByKey(key windows.GUID) (bool, error) {
	var filter *wtFwpmFilter0
	r, _, _ := procFwpmFilterGetByKey0.Call(
		uintptr(e.handle),
		uintptr(unsafe.Pointer(&key)),
		uintptr(unsafe.Pointer(&filter)),
	)
	runtime.KeepAlive(&key)
	if r != 0 {
		if uint32(r) == fwpEFilterNotFound {
			return false, nil
		}
		return false, fmt.Errorf("FwpmFilterGetByKey0: %w", windows.Errno(r))
	}
	if filter != nil {
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&filter)))
	}
	return true, nil
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
func (e *wfpEngine) sublayerFilterIds(subLayer windows.GUID, keep func(*wtFwpmFilter0) bool) ([]uint64, error) {
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
		var entries **wtFwpmFilter0
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
			if filter != nil && filter.subLayerKey == subLayer && keep(filter) {
				ids = append(ids, filter.filterID)
			}
		}
		procFwpmFreeMemory0.Call(uintptr(unsafe.Pointer(&entries)))
		if returned < batch {
			return ids, nil
		}
	}
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

// ---------------------------------------------------------------------------
// Kill switch filter builders
// ---------------------------------------------------------------------------

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

func (e *wfpEngine) addPermitEndpointIP(ipStr string) (uint64, error) {
	ip := net.ParseIP(ipStr).To4()
	if ip == nil {
		return 0, fmt.Errorf("invalid IPv4 address: %s", ipStr)
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
	id, err := e.addFilter(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "PangeaVPN Allow Endpoint "+ipStr, weightTrustedPermit, cFWP_ACTION_PERMIT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
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

// dnsBlockConditions matches port 53 over one transport protocol. Only the
// permits above weightDNSBlock (loopback, endpoint, tunnel) get past it.
func dnsBlockConditions(proto wtIPProto) []wtFwpmFilterCondition0 {
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
				value: uintptr(dnsPort),
			},
		},
	}
}

// addBlockDNSUDP and addBlockDNSTCP close the Allow-LAN DNS hole: the router
// is a resolver, and Windows will happily ask it beside the tunnel's.
func (e *wfpEngine) addBlockDNSUDP() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaBlockDNSUDPV4FilterKey, "PangeaVPN Block DNS UDP", weightDNSBlock, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, dnsBlockConditions(cIPPROTO_UDP))
}

func (e *wfpEngine) addBlockDNSTCP() (uint64, error) {
	return e.addFilterKeyed(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, pangeaBlockDNSTCPV4FilterKey, "PangeaVPN Block DNS TCP", weightDNSBlock, cFWP_ACTION_BLOCK, cFWPM_FILTER_FLAG_PERSISTENT, dnsBlockConditions(cIPPROTO_TCP))
}

func (e *wfpEngine) addPermitTunnelInterface(luid uint64) (uint64, error) {
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
	id, err := e.addFilter(cFWPM_LAYER_ALE_AUTH_CONNECT_V4, "PangeaVPN Allow Tunnel Interface", weightTrustedPermit, cFWP_ACTION_PERMIT, conditions)
	runtime.KeepAlive(&luid)
	return id, err
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

// addPermitLoopbackInboundV6 permits IPv6 traffic carrying the IS_LOOPBACK
// flag at the recv/accept layer. The inbound V6 block otherwise drops the
// server side of every [::1] connection — localhost resolves to ::1 first on
// Windows, so local web servers become unreachable while the kill switch is
// active.
func (e *wfpEngine) addPermitLoopbackInboundV6() (uint64, error) {
	return e.addPermitLoopbackAt(cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6, pangeaPermitLoopbackInboundV6FilterKey, "PangeaVPN Allow Loopback Inbound IPv6")
}

// addPermitLoopbackSubnetV6 permits ::1/128 by remote address on the given
// layer. Complements the IS_LOOPBACK flag permits, which are not reliably set
// for fresh inter-process TCP connects — the same quirk that required
// addPermitLoopbackSubnet on IPv4. Loopback is non-routable, so there is no
// leak risk.
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
	id, err := e.addFilter(cFWPM_LAYER_IPFORWARD_V4, "PangeaVPN Allow Forwarding To LAN "+cidr, weightLANPermit, cFWP_ACTION_PERMIT, conditions)
	runtime.KeepAlive(&addrMask)
	return id, err
}
