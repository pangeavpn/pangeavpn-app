//go:build windows

package platform

import (
	"net/netip"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
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

type windowsRoute struct {
	name    string
	up      bool
	gateway netip.Addr
	metric  uint64
}

// PhysicalDefaultRoute names the physical interface holding the lowest-metric
// IPv4 default route and its gateway. Unlike HostInternet it needs no probe,
// so it still answers behind the kill switch.
func PhysicalDefaultRoute() (iface, gateway string, err error) {
	table, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return "", "", err
	}
	rows := make([]windowsRoute, 0, 4)
	for i := range table {
		row := &table[i]
		prefix := row.DestinationPrefix.Prefix()
		if !prefix.IsValid() || prefix.Bits() != 0 || row.Loopback {
			continue
		}
		r := windowsRoute{gateway: row.NextHop.Addr(), metric: uint64(row.Metric)}
		if ifc, ifErr := row.InterfaceLUID.Interface(); ifErr == nil {
			r.name = ifc.Alias()
			r.up = ifc.OperStatus == winipcfg.IfOperStatusUp
		}
		if ipif, ipErr := row.InterfaceLUID.IPInterface(windows.AF_INET); ipErr == nil {
			r.metric += uint64(ipif.Metric)
		}
		rows = append(rows, r)
	}
	return physicalDefaultRoute(rows)
}

func physicalDefaultRoute(rows []windowsRoute) (iface, gateway string, err error) {
	var best *windowsRoute
	for i := range rows {
		r := &rows[i]
		// An on-link default belongs to a tunnel, not a gateway.
		if !r.up || r.name == "" || isVirtualInterface(r.name) || !r.gateway.IsValid() ||
			r.gateway.IsUnspecified() || r.gateway.IsLoopback() || r.gateway.IsMulticast() {
			continue
		}
		if best == nil || r.metric < best.metric {
			best = r
		}
	}
	if best == nil {
		return "", "", ErrNoDefaultRoute
	}
	return best.name, best.gateway.String(), nil
}

func isVirtualInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, prefix := range []string{"pangea", "wg", "tun", "tap", "loopback"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	return false
}
