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

// The connectivity-hint source reuses netChangeCallback, which ignores the
// by-value hint struct; this proves it still dispatches a network-changed event.
func TestNetChangeCallbackDispatchesNetworkChanged(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events, err := WatchSystemEvents(ctx)
	if err != nil {
		t.Fatalf("WatchSystemEvents: %v", err)
	}
	netChangeMu.Lock()
	lastNetChange = time.Time{}
	netChangeMu.Unlock()

	netChangeCallback(0, 0, 0)
	select {
	case ev := <-events:
		if ev != SystemEventNetworkChanged {
			t.Fatalf("dispatched %v, want SystemEventNetworkChanged", ev)
		}
	case <-time.After(time.Second):
		t.Fatal("callback dispatched no network-changed event")
	}
}
