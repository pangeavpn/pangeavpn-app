package platform

import (
	"context"
	"os"
	"sync"
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

// eventCoalescer emits at most once per gap but defers, never drops, a change
// inside the gap: the route add that follows an interface-up must still land.
type eventCoalescer struct {
	gap  time.Duration
	emit func()

	mu       sync.Mutex
	lastEmit time.Time
	pending  *time.Timer
	stopped  bool
}

// newEventCoalescer's emit runs under the coalescer's lock, so it must not block.
func newEventCoalescer(gap time.Duration, emit func()) *eventCoalescer {
	return &eventCoalescer{gap: gap, emit: emit}
}

func (c *eventCoalescer) trigger() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped || c.pending != nil {
		return
	}
	now := time.Now()
	if wait := c.gap - now.Sub(c.lastEmit); wait > 0 {
		c.pending = time.AfterFunc(wait, c.emitDeferred)
		return
	}
	c.lastEmit = now
	c.emit()
}

func (c *eventCoalescer) emitDeferred() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pending = nil
	if c.stopped {
		return
	}
	c.lastEmit = time.Now()
	c.emit()
}

// stop guarantees emit never runs again once it returns, so the caller may
// then close whatever emit sends on.
func (c *eventCoalescer) stop() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.stopped = true
	if c.pending != nil {
		c.pending.Stop()
		c.pending = nil
	}
}

// watchRouteEvents turns raw traffic on a routing socket/link into
// rate-limited SystemEventNetworkChanged signals, closing on ctx or a read error.
func watchRouteEvents(ctx context.Context, f *os.File, gap time.Duration) <-chan SystemEvent {
	events := make(chan SystemEvent, 4)
	changes := newEventCoalescer(gap, func() {
		select {
		case events <- SystemEventNetworkChanged:
		default:
		}
	})
	go func() {
		<-ctx.Done()
		f.Close()
	}()
	go func() {
		defer close(events)
		defer changes.stop()
		defer f.Close()
		buf := make([]byte, 4096)
		for {
			if _, err := f.Read(buf); err != nil {
				return
			}
			changes.trigger()
		}
	}()
	return events
}
