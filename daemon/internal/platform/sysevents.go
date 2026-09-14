package platform

import (
	"context"
	"os"
	"time"
)

// SystemEvent is a host-level signal the recovery logic should react to
// immediately instead of waiting for its next timer.
type SystemEvent int

const (
	// SystemEventResumed fires when the host wakes from sleep or hibernation.
	SystemEventResumed SystemEvent = iota
	// SystemEventNetworkChanged fires when the host's network connectivity
	// changes in any direction; the consumer re-evaluates, it does not trust it.
	SystemEventNetworkChanged
)

// routeEventMinGap rate-limits the burst a single link flap produces; the
// consumer only needs one kick, not one per routing-table mutation.
const routeEventMinGap = time.Second

// watchRouteEvents turns raw traffic on a routing socket/link into
// rate-limited SystemEventNetworkChanged signals, closing on ctx or a read error.
func watchRouteEvents(ctx context.Context, f *os.File) <-chan SystemEvent {
	events := make(chan SystemEvent, 4)
	go func() {
		<-ctx.Done()
		f.Close()
	}()
	go func() {
		defer close(events)
		defer f.Close()
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
	return events
}
