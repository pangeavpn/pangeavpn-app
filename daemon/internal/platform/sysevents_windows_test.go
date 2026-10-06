//go:build windows

package platform

import (
	"context"
	"testing"
	"time"
)

func TestWatchSystemEventsRegisters(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := WatchSystemEvents(ctx)
	if err != nil {
		t.Fatalf("WatchSystemEvents: %v", err)
	}
	if events == nil {
		t.Fatal("WatchSystemEvents returned a nil channel with no error")
	}
}

// resetNetChanges waits out a trailing emit left by real OS events and clears the
// last emit; call it after subscribing, so a real event can only add to the test's.
func resetNetChanges(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * netChangeMinGap)
	for {
		netChanges.mu.Lock()
		idle := netChanges.pending == nil
		if idle {
			netChanges.lastEmit = time.Time{}
		}
		netChanges.mu.Unlock()
		if idle {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("net-change coalescer never went idle")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The connectivity-hint source reuses netChangeCallback, which ignores the
// by-value hint struct; this proves it still dispatches a network-changed event.
func TestNetChangeCallbackDispatchesNetworkChanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := WatchSystemEvents(ctx)
	if err != nil {
		t.Fatalf("WatchSystemEvents: %v", err)
	}
	resetNetChanges(t)

	netChangeCallback(0, 0, 0)
	select {
	case ev := <-events:
		if ev != SystemEventNetworkChanged {
			t.Fatalf("dispatched %v, want SystemEventNetworkChanged", ev)
		}
	case <-time.After(2 * netChangeMinGap):
		t.Fatal("callback dispatched no network-changed event")
	}
}

// A route add landing just after an interface-up must still reach the consumer.
func TestNetChangeCallbackDeliversTrailingEvent(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := WatchSystemEvents(ctx)
	if err != nil {
		t.Fatalf("WatchSystemEvents: %v", err)
	}
	resetNetChanges(t)

	netChangeCallback(0, 0, 0)
	netChangeCallback(0, 0, 0)
	for i := 1; i <= 2; i++ {
		select {
		case ev := <-events:
			if ev != SystemEventNetworkChanged {
				t.Fatalf("event %d: dispatched %v, want SystemEventNetworkChanged", i, ev)
			}
		case <-time.After(2 * netChangeMinGap):
			t.Fatalf("event %d never dispatched; a change inside the gap was dropped", i)
		}
	}
}
