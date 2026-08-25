//go:build windows

package platform

import (
	"context"
	"testing"
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
