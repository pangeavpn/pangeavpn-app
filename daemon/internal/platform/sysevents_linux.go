//go:build linux && !android

package platform

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

// WatchSystemEvents streams network-change signals from a netlink route
// socket. Resume is left to the health loop's wall-clock gap detection.
func WatchSystemEvents(ctx context.Context) (<-chan SystemEvent, error) {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_CLOEXEC|unix.SOCK_NONBLOCK, unix.NETLINK_ROUTE)
	if err != nil {
		return nil, err
	}
	sa := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: unix.RTMGRP_LINK | unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR |
			unix.RTMGRP_IPV4_ROUTE | unix.RTMGRP_IPV6_ROUTE,
	}
	if err := unix.Bind(fd, sa); err != nil {
		unix.Close(fd)
		return nil, err
	}
	// os.NewFile hands the fd to the runtime poller so Close unblocks Read.
	f := os.NewFile(uintptr(fd), "netlink-route")
	return watchRouteEvents(ctx, f), nil
}
