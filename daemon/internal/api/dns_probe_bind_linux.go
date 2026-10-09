package api

import (
	"net"
	"syscall"
)

// bindDialerToInterface pins the probe's socket to iface via SO_BINDTODEVICE,
// so it can neither send nor receive over any other route the host holds.
func bindDialerToInterface(iface string) (*net.Dialer, error) {
	return &net.Dialer{
		Control: func(_, _ string, c syscall.RawConn) error {
			var sockErr error
			if err := c.Control(func(fd uintptr) {
				sockErr = syscall.SetsockoptString(int(fd), syscall.SOL_SOCKET, syscall.SO_BINDTODEVICE, iface)
			}); err != nil {
				return err
			}
			return sockErr
		},
	}, nil
}
