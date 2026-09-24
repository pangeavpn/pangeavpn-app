//go:build darwin

package platform

import (
	"context"
	"os"

	"golang.org/x/sys/unix"
)

// WatchSystemEvents streams network-change signals from the kernel's routing
// socket. Resume is left to the health loop's wall-clock gap detection.
func WatchSystemEvents(ctx context.Context) (<-chan SystemEvent, error) {
	fd, err := unix.Socket(unix.AF_ROUTE, unix.SOCK_RAW, unix.AF_UNSPEC)
	if err != nil {
		return nil, err
	}
	if err := unix.SetNonblock(fd, true); err != nil {
		unix.Close(fd)
		return nil, err
	}
	// os.NewFile hands the fd to the runtime poller so Close unblocks Read.
	f := os.NewFile(uintptr(fd), "route-socket")
	return watchRouteEvents(ctx, f, routeEventMinGap), nil
}
