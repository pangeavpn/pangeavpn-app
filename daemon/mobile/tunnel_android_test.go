//go:build android

package mobile

import "testing"

// checkHostFDAfterRefusedStart: Android hands the fd to Go, so a refused Start closes it.
func checkHostFDAfterRefusedStart(t *testing.T, fd int) {
	t.Helper()
	if fdIsOpen(fd) {
		t.Fatalf("refused Start left Go's fd %d open", fd)
	}
}
