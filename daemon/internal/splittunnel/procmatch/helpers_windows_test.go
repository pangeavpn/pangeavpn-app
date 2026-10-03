//go:build windows

package procmatch

import (
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func reuseAddrControl(network, address string, c syscall.RawConn) error {
	var serr error
	if err := c.Control(func(fd uintptr) {
		serr = windows.SetsockoptInt(windows.Handle(fd), windows.SOL_SOCKET, windows.SO_REUSEADDR, 1)
	}); err != nil {
		return err
	}
	return serr
}

// longTempDir expands t.TempDir to the long-name form the kernel reports; CI's TEMP is C:\Users\RUNNER~1\...
func longTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	long, ok := longPath(dir)
	if !ok {
		t.Fatalf("GetLongPathName(%q) failed", dir)
	}
	return long
}
