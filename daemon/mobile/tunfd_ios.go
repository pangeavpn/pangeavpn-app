//go:build ios

package mobile

import "golang.org/x/sys/unix"

const (
	sysprotoControl = 2
	utunOptIfname   = 2
	maxScanFD       = 1024
)

// FindTunFD returns the extension's utun fd, or -1 when none is open.
func FindTunFD() int {
	for fd := 0; fd < maxScanFD; fd++ {
		name, err := unix.GetsockoptString(fd, sysprotoControl, utunOptIfname)
		if err == nil && isUtunName(name) {
			return fd
		}
	}
	return -1
}
