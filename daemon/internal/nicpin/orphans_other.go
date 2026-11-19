//go:build !linux || android

package nicpin

// RemoveOrphans is a no-op off Linux: macOS scoped host routes are inert once the daemon that
// owned them is gone, since only sockets bound to the NIC consult them and pf blocks all but its broker.
func RemoveOrphans() {}
