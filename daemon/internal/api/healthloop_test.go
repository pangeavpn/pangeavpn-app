package api

import (
	"context"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
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

// Another operation holding opMu (the app's PermitHosts, say) only means this
// tick could not run; stamping ERROR over it started a needless rebuild.
func TestHealthCheck_BusyLockSkipsTheTick(t *testing.T) {
	svc, naive, _, _ := recoveryTestService(t)
	naive.mu.Lock()
	naive.running = false
	naive.mu.Unlock()
	svc.recoveryMu.Lock()
	svc.offlineHeld = true
	svc.recoveryMu.Unlock()

	held, release := make(chan struct{}), make(chan struct{})
	go func() {
		svc.opMu.Lock()
		close(held)
		<-release
		svc.opMu.Unlock()
	}()
	awaitOrFail(t, held, "another operation to take opMu")
	svc.runHealthCheck(context.Background())
	close(release)

	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %s (%s), want CONNECTED: a busy lock is not a failed transport", status.State, status.Detail)
	}
	for _, entry := range svc.logs.Since(0) {
		if entry.Level == state.LogError {
			t.Errorf("busy tick logged an error: %s", entry.Msg)
		}
	}
	if !status.Offline {
		t.Error("busy tick cleared the offline state it never re-checked")
	}
	if svc.offlineHoldActive() {
		t.Error("busy tick armed an offline hold")
	}
}
