//go:build (linux && !android) || darwin

package platform

import (
	"errors"
	"net"
)

// hostInternetFromRoute maps PhysicalDefaultRoute's error into the (online,
// known) verdict HostInternet reports on the route-table platforms.
func hostInternetFromRoute(err error) (online bool, known bool) {
	switch {
	case err == nil:
		return true, true
	case errors.Is(err, ErrNoDefaultRoute):
		return false, true
	}
	return false, false
}

func interfaceNameByIndex(index int) string {
	ifi, err := net.InterfaceByIndex(index)
	if err != nil {
		return ""
	}
	return ifi.Name
}
