//go:build windows

package api

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isOversizedDatagram reports a reply that overflowed the read buffer. Windows
// fails the whole recvfrom with WSAEMSGSIZE and drops the datagram; Unix copies
// what fits and reports success, so only this platform needs the check.
func isOversizedDatagram(err error) bool {
	return errors.Is(err, windows.WSAEMSGSIZE)
}
