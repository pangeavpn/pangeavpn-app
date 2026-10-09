//go:build !windows

package api

// isOversizedDatagram is Windows-only: every other platform truncates an
// oversized datagram into the buffer and reports a successful read.
func isOversizedDatagram(error) bool { return false }
