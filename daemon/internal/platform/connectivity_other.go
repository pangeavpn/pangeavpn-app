//go:build !windows && !darwin && (!linux || android)

package platform

import "errors"

// HostInternet has no OS-level connectivity oracle on android, so it
// reports "unknown" and callers fall back to their interface heuristic.
func HostInternet() (online bool, known bool) { return false, false }

func PhysicalDefaultRoute() (iface, gateway string, err error) {
	return "", "", errors.ErrUnsupported
}
