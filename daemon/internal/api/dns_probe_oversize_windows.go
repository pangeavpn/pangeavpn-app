//go:build windows

package api

import (
	"errors"

	"golang.org/x/sys/windows"
)

// isOversizedDatagram reports a reply that overflowed the read buffer: Windows
// fails recvfrom with WSAEMSGSIZE where Unix just truncates.
func isOversizedDatagram(err error) bool {
	return errors.Is(err, windows.WSAEMSGSIZE)
}
