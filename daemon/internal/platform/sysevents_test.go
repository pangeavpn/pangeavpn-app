package platform

import (
	"context"
	"os"
	"testing"
	"time"
)

const testGap = 200 * time.Millisecond

func recordingCoalescer(gap time.Duration) (*eventCoalescer, <-chan time.Time) {
	emits := make(chan time.Time, 16)
	return newEventCoalescer(gap, func() { emits <- time.Now() }), emits
}

func TestEventCoalescerSingleEventEmitsOnce(t *testing.T) {
	c, emits := recordingCoalescer(testGap)
	defer c.stop()

	c.trigger()
	if n := len(emits); n != 1 {
		t.Fatalf("got %d immediate emits, want 1", n)
	}
	time.Sleep(2 * testGap)
	if n := len(emits); n != 1 {
		t.Fatalf("got %d emits after the gap, want 1", n)
	}
}

func TestEventCoalescerBurstEmitsLeadingAndOneTrailing(t *testing.T) {
	c, emits := recordingCoalescer(testGap)
	defer c.stop()

	for i := 0; i < 10; i++ {
		c.trigger()
	}
	if n := len(emits); n != 1 {
		t.Fatalf("got %d immediate emits, want 1", n)
	}
	first := <-emits
	select {
	case trailing := <-emits:
		if d := trailing.Sub(first); d < testGap-testGap/10 {
			t.Fatalf("trailing emit %v after the leading one, want about %v", d, testGap)
		}
	case <-time.After(10 * testGap):
		t.Fatal("burst inside the gap produced no trailing emit")
	}
	time.Sleep(2 * testGap)
	if n := len(emits); n != 0 {
		t.Fatalf("got %d extra emits, want exactly one trailing emit", n)
	}
}

func TestEventCoalescerStopCancelsPendingEmit(t *testing.T) {
	c, emits := recordingCoalescer(testGap)
	c.trigger()
	c.trigger()
	<-emits

	c.stop()
	c.trigger()
	time.Sleep(2 * testGap)
	if n := len(emits); n != 0 {
		t.Fatalf("got %d emits after stop, want 0", n)
	}
}

func awaitEmit(t *testing.T, emits <-chan time.Time) {
	t.Helper()
	select {
	case <-emits:
	case <-time.After(10 * testGap):
		t.Fatal("coalescer never emitted")
	}
}

func timerArmed(c *eventCoalescer) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pending != nil
}

func TestEventCoalescerHoldsNoTimerOnceFiredOrStopped(t *testing.T) {
	fired, firedEmits := recordingCoalescer(testGap)
	defer fired.stop()
	fired.trigger()
	fired.trigger()
	awaitEmit(t, firedEmits)
	awaitEmit(t, firedEmits)
	if timerArmed(fired) {
		t.Error("a timer is still armed after the trailing emit fired")
	}

	stopped, _ := recordingCoalescer(testGap)
	stopped.trigger()
	stopped.trigger()
	stopped.stop()
	if timerArmed(stopped) {
		t.Error("stop left the trailing emit's timer armed")
	}
}

func nextRouteEvent(t *testing.T, events <-chan SystemEvent) {
	t.Helper()
	select {
	case ev, ok := <-events:
		if !ok {
			t.Fatal("events channel closed early")
		}
		if ev != SystemEventNetworkChanged {
			t.Fatalf("got %v, want SystemEventNetworkChanged", ev)
		}
	case <-time.After(10 * testGap):
		t.Fatal("no network-changed event; a change inside the gap was dropped")
	}
}

func writeRouteMessage(t *testing.T, w *os.File) {
	t.Helper()
	if _, err := w.Write([]byte{1}); err != nil {
		t.Fatalf("write: %v", err)
	}
}

func TestWatchRouteEventsDeliversChangeInsideGap(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	defer w.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchRouteEvents(ctx, r, testGap)

	writeRouteMessage(t, w)
	nextRouteEvent(t, events)
	writeRouteMessage(t, w)
	nextRouteEvent(t, events)
}

func TestWatchRouteEventsNeverSendsAfterClose(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	events := watchRouteEvents(ctx, r, testGap)

	writeRouteMessage(t, w)
	nextRouteEvent(t, events)
	writeRouteMessage(t, w)
	w.Close()
	for closed := false; !closed; {
		select {
		case _, ok := <-events:
			closed = !ok
		case <-time.After(10 * testGap):
			t.Fatal("events channel did not close after the reader hit EOF")
		}
	}
	// A trailing emit that outlived the close would panic the test binary here.
	time.Sleep(2 * testGap)
}
