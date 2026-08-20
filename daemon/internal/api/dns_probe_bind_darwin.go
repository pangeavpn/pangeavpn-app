package api

import (
	"fmt"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// bindDialerToInterface pins the probe's socket to iface via IP_BOUND_IF, so
// it can neither send nor receive over any other route the host holds.
func bindDialerToInterface(iface string) (*net.Dialer, error) {
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("resolve tunnel interface %q: %w", iface, err)
	}
	return &net.Dialer{
		Control: func(_, _ string, c syscall.RawConn) error {
			var sockErr error
			if err := c.Control(func(fd uintptr) {
				sockErr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, ifi.Index)
			}); err != nil {
				return err
			}
			return sockErr
		},
	}, nil
}
