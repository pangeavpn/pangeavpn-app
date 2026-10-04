package egress

import (
	"syscall"

	"golang.org/x/sys/windows"
)

// ERROR_NETWORK_UNREACHABLE and ERROR_HOST_UNREACHABLE are what overlapped connects can report.
var unreachableErrnos = []syscall.Errno{
	windows.WSAENETUNREACH,
	windows.WSAEHOSTUNREACH,
	windows.WSAEADDRNOTAVAIL,
	windows.WSAENETDOWN,
	windows.ERROR_NETWORK_UNREACHABLE,
	windows.ERROR_HOST_UNREACHABLE,
}
