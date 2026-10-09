//go:build linux && !android

package platform

import (
	"context"
	"testing"
	"time"
)

func TestWatchSystemEventsOpensAndClosesCleanly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	events, err := WatchSystemEvents(ctx)
	if err != nil {
		t.Fatalf("WatchSystemEvents: %v", err)
	}
	if events == nil {
		t.Fatal("WatchSystemEvents returned a nil channel with no error")
	}
	cancel()
	select {
	case _, ok := <-events:
		if ok {
			// A real network event racing the cancel is fine; drain to close.
			for range events {
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("events channel did not close after context cancellation")
	}
}
