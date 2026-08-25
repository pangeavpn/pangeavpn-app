//go:build windows

package platform

import (
	"context"
	"errors"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modPowrprof                                = windows.NewLazySystemDLL("powrprof.dll")
	procPowerRegisterSuspendResumeNotification = modPowrprof.NewProc("PowerRegisterSuspendResumeNotification")

	modIphlpapiEvents           = windows.NewLazySystemDLL("iphlpapi.dll")
	procNotifyRouteChange2      = modIphlpapiEvents.NewProc("NotifyRouteChange2")
	procNotifyIpInterfaceChange = modIphlpapiEvents.NewProc("NotifyIpInterfaceChange")
)

const (
	deviceNotifyCallbackFlag = 2

	pbtAPMResumeSuspend   = 0x0007
	pbtAPMResumeAutomatic = 0x0012
)

// deviceNotifySubscribeParams is DEVICE_NOTIFY_SUBSCRIBE_PARAMETERS. Kept
// alive in a package var for as long as the registration exists.
type deviceNotifySubscribeParams struct {
	callback uintptr
	context  uintptr
}

// The callbacks run on system threads and must return fast, so they hand the
// event to whichever subscriber is current and never block.
var (
	sysEventsMu   sync.Mutex
	sysEventsSink chan<- SystemEvent
)

func dispatchSystemEvent(event SystemEvent) {
	sysEventsMu.Lock()
	sink := sysEventsSink
	sysEventsMu.Unlock()
	if sink == nil {
		return
	}
	select {
	case sink <- event:
	default:
	}
}

func powerNotifyCallback(_, notifyType, _ uintptr) uintptr {
	if notifyType == pbtAPMResumeSuspend || notifyType == pbtAPMResumeAutomatic {
		dispatchSystemEvent(SystemEventResumed)
	}
	return 0
}

// netChangeMinGap rate-limits the burst a single link flap produces; the
// consumer only needs one kick, not one per table mutation.
const netChangeMinGap = time.Second

var (
	netChangeMu   sync.Mutex
	lastNetChange time.Time
)

// netChangeCallback serves NotifyRouteChange2 and NotifyIpInterfaceChange;
// both pass (context, row pointer, notification type), all uintptr-sized.
func netChangeCallback(_, _, _ uintptr) uintptr {
	netChangeMu.Lock()
	now := time.Now()
	fire := now.Sub(lastNetChange) >= netChangeMinGap
	if fire {
		lastNetChange = now
	}
	netChangeMu.Unlock()
	if fire {
		dispatchSystemEvent(SystemEventNetworkChanged)
	}
	return 0
}

// OS registrations are process-lifetime: windows.NewCallback slots can never
// be released, so re-subscribing swaps the sink rather than re-registering.
var (
	registerOnce       sync.Once
	registerErr        error
	powerNotifyParams  *deviceNotifySubscribeParams
	powerNotifyHandle  uintptr
	routeChangeHandle  windows.Handle
	ifaceChangeHandle  windows.Handle
	powerRegistered    bool
	connectivityHooked bool
)

func registerSystemEventSources() error {
	powerNotifyParams = &deviceNotifySubscribeParams{
		callback: windows.NewCallback(powerNotifyCallback),
	}
	ret, _, _ := procPowerRegisterSuspendResumeNotification.Call(
		deviceNotifyCallbackFlag,
		uintptr(unsafe.Pointer(powerNotifyParams)),
		uintptr(unsafe.Pointer(&powerNotifyHandle)),
	)
	powerRegistered = ret == 0

	// Route and interface notifications, not the connectivity hint: the hint
	// callback takes a struct by value, which NewCallback cannot carry (arm64).
	const afUnspec = 0
	netCallback := windows.NewCallback(netChangeCallback)
	if ret, _, _ := procNotifyRouteChange2.Call(afUnspec, netCallback, 0, 0, uintptr(unsafe.Pointer(&routeChangeHandle))); ret == 0 {
		connectivityHooked = true
	}
	if ret, _, _ := procNotifyIpInterfaceChange.Call(afUnspec, netCallback, 0, 0, uintptr(unsafe.Pointer(&ifaceChangeHandle))); ret == 0 {
		connectivityHooked = true
	}

	if !powerRegistered && !connectivityHooked {
		return errors.New("no system event source could be registered")
	}
	return nil
}

// WatchSystemEvents delivers resume and network-change signals until ctx ends.
// Only one subscriber is served at a time; a new call displaces the previous
// sink. Events are best-effort: a full buffer drops rather than blocks.
func WatchSystemEvents(ctx context.Context) (<-chan SystemEvent, error) {
	registerOnce.Do(func() {
		registerErr = registerSystemEventSources()
	})
	if registerErr != nil {
		return nil, registerErr
	}

	events := make(chan SystemEvent, 4)
	sysEventsMu.Lock()
	sysEventsSink = events
	sysEventsMu.Unlock()

	go func() {
		<-ctx.Done()
		sysEventsMu.Lock()
		if sysEventsSink == (chan<- SystemEvent)(events) {
			sysEventsSink = nil
		}
		sysEventsMu.Unlock()
	}()
	return events, nil
}
