//go:build !windows && !unix

package egress

import "syscall"

var unreachableErrnos []syscall.Errno
