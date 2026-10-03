//go:build unix

package egress

import (
	"syscall"

	"golang.org/x/sys/unix"
)

var unreachableErrnos = []syscall.Errno{
	unix.ENETUNREACH,
	unix.EHOSTUNREACH,
	unix.ENETDOWN,
	unix.EADDRNOTAVAIL,
	unix.ENXIO,
}
