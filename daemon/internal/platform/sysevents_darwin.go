//go:build darwin

package platform

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// routeEventMinGap rate-limits the burst a single link flap produces; the
// consumer only needs one kick, not one per routing-table mutation.
const routeEventMinGap = time.Second

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

	events := make(chan SystemEvent, 4)
	go func() {
		<-ctx.Done()
		f.Close()
	}()
	go func() {
		defer close(events)
		defer f.Close()
		// Any unsolicited routing-socket traffic means the network moved;
		// decoding which message arrived would not change the reaction.
		buf := make([]byte, 4096)
		var lastEmit time.Time
		for {
			if _, err := f.Read(buf); err != nil {
				return
			}
			if now := time.Now(); now.Sub(lastEmit) >= routeEventMinGap {
				lastEmit = now
				select {
				case events <- SystemEventNetworkChanged:
				default:
				}
			}
		}
	}()
	return events, nil
}
