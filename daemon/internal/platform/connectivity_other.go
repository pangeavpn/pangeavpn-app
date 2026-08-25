//go:build !windows && !darwin && (!linux || android)

package platform

// HostInternet has no OS-level connectivity oracle on android, so it
// reports "unknown" and callers fall back to their interface heuristic.
func HostInternet() (online bool, known bool) { return false, false }
