//go:build unix

package procmatch

import (
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

func reuseAddrControl(network, address string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
		if serr == nil {
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		}
	}); err != nil {
		return err
	}
	return serr
}

func longTempDir(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}
