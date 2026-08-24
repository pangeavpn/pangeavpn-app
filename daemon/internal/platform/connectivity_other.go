//go:build !windows

package platform

// HostInternet has no OS-level connectivity oracle on non-Windows yet, so it
// reports "unknown" and callers fall back to their interface heuristic.
func HostInternet() (online bool, known bool) { return false, false }
