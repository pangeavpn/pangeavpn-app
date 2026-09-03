package api

import (
	"testing"
	"time"
)

func TestSuspendGapIgnoresSlowHealthChecks(t *testing.T) {
	start := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	gaps := newSuspendGapTracker(start)

	tickAt := start.Add(healthTickInterval)
	if gap, slept := gaps.tick(tickAt); slept {
		t.Fatalf("an on-schedule tick read as sleep (gap %s)", gap)
	}
	// The check this tick ran took a whole transport cascade's worth of time.
	checkEnd := tickAt.Add(2 * suspendGapThreshold)
	gaps = gaps.checkDone(checkEnd)

	if gap, slept := gaps.tick(checkEnd.Add(healthTickInterval)); slept {
		t.Fatalf("a slow health check read as a host suspend (gap %s)", gap)
	}
}

func TestSuspendGapDetectsSleepBetweenChecks(t *testing.T) {
	start := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	gaps := newSuspendGapTracker(start).checkDone(start)

	asleep := suspendGapThreshold + time.Second
	gap, slept := gaps.tick(start.Add(asleep))
	if !slept {
		t.Fatalf("a %s gap between checks was not read as sleep", asleep)
	}
	if gap != asleep {
		t.Fatalf("gap = %s, want %s", gap, asleep)
	}
}

func TestSuspendGapCountsFromTheLastCheckNotTheLastTick(t *testing.T) {
	start := time.Date(2026, 9, 3, 0, 0, 0, 0, time.UTC)
	gaps := newSuspendGapTracker(start)

	// A kick-driven check (no tick) that ends late must still reset the clock.
	gaps = gaps.checkDone(start.Add(suspendGapThreshold))
	if gap, slept := gaps.tick(start.Add(suspendGapThreshold + healthTickInterval)); slept {
		t.Fatalf("gap measured from the tick rather than the check end (gap %s)", gap)
	}
}
