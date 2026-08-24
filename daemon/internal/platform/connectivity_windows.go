//go:build windows

package platform

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modIphlpapi                    = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetNetworkConnectivityHint = modIphlpapi.NewProc("GetNetworkConnectivityHint")
)

// NL_NETWORK_CONNECTIVITY_LEVEL_HINT values (0=Unknown, 5=Hidden fall through
// to the default case, so they aren't named here).
const (
	nlConnectivityLevelNone                int32 = 1
	nlConnectivityLevelLocalAccess         int32 = 2
	nlConnectivityLevelInternetAccess      int32 = 3
	nlConnectivityLevelConstrainedInternet int32 = 4
)

// NL_NETWORK_CONNECTIVITY_HINT: two enum ints followed by three BOOLEANs. Only
// ConnectivityLevel is read; the rest is present so the layout matches the API.
type nlNetworkConnectivityHint struct {
	ConnectivityLevel    int32
	ConnectivityCost     int32
	ApproachingDataLimit byte
	OverDataLimit        byte
	Roaming              byte
}

// HostInternet reports the OS's own aggregate connectivity verdict — the same
// signal behind the Windows "no internet" tray indicator. online is meaningful
// only when known is true; known is false on pre-2004 Windows (no such API) or
// an Unknown/Hidden verdict, so callers fall back to their interface heuristic.
func HostInternet() (online bool, known bool) {
	if err := procGetNetworkConnectivityHint.Find(); err != nil {
		return false, false
	}
	var hint nlNetworkConnectivityHint
	r, _, _ := procGetNetworkConnectivityHint.Call(uintptr(unsafe.Pointer(&hint)))
	if r != 0 {
		return false, false
	}
	switch hint.ConnectivityLevel {
	case nlConnectivityLevelInternetAccess, nlConnectivityLevelConstrainedInternet:
		return true, true
	case nlConnectivityLevelNone, nlConnectivityLevelLocalAccess:
		return false, true
	default:
		return false, false
	}
}
