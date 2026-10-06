package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

func newSysEventsTestService(t *testing.T) (*Service, *fakeWGManager) {
	t.Helper()
	wgMgr := &fakeWGManager{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, &fakeKillSwitch{})
	return svc, wgMgr
}

func TestResumeArmsSingleFailureRebuildAndHoldsHealth(t *testing.T) {
	svc, _ := newSysEventsTestService(t)

	svc.onSystemResume(context.Background(), "test resume")

	if !svc.healthHeld() {
		t.Fatal("health checks are not held right after a resume")
	}
	if failures, rebuild := svc.recordDNSProbeFailure(); !rebuild {
		t.Fatalf("first probe failure after resume did not trigger a rebuild (failures=%d)", failures)
	}
}

func TestProbeFailuresNeedDebounceOutsideResumeWindow(t *testing.T) {
	svc, _ := newSysEventsTestService(t)

	if failures, rebuild := svc.recordDNSProbeFailure(); rebuild || failures != 1 {
		t.Fatalf("first failure outside a resume window: got (%d, %v), want (1, false)", failures, rebuild)
	}
	if failures, rebuild := svc.recordDNSProbeFailure(); !rebuild || failures != 2 {
		t.Fatalf("second failure outside a resume window: got (%d, %v), want (2, true)", failures, rebuild)
	}
}

func TestResumeClearsProbeRebuildCooldown(t *testing.T) {
	svc, _ := newSysEventsTestService(t)

	svc.recoveryMu.Lock()
	svc.dnsProbeQuietUntil = time.Now().Add(time.Hour)
	svc.recoveryMu.Unlock()

	svc.onSystemResume(context.Background(), "test resume")

	if _, rebuild := svc.recordDNSProbeFailure(); !rebuild {
		t.Fatal("a pre-sleep rebuild cooldown still suppressed the post-resume rebuild")
	}
}

func TestResumeRebindsDeviceSocketsOnce(t *testing.T) {
	svc, wgMgr := newSysEventsTestService(t)

	svc.onSystemResume(context.Background(), "test resume")
	// The second notification within the dedupe window (RESUMEAUTOMATIC and
	// RESUMESUSPEND both fire on one wake) must not rebind or re-extend again.
	svc.onSystemResume(context.Background(), "duplicate resume")

	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	if wgMgr.rebindCount != 1 {
		t.Fatalf("RebindDeviceSockets called %d times, want 1", wgMgr.rebindCount)
	}
}

func TestNetworkChangeReleasesHoldAndBackoff(t *testing.T) {
	svc, _ := newSysEventsTestService(t)
	svc.networkKey = func() string { return "net-a" }

	svc.onSystemResume(context.Background(), "test resume")
	svc.scheduleNextRecovery(3)

	svc.networkKey = func() string { return "net-b" }
	svc.onNetworkChanged()

	if svc.healthHeld() {
		t.Fatal("health hold survived a usable-network event")
	}
	if !svc.recoveryDue() {
		t.Fatal("recovery backoff survived a usable-network event")
	}
	if !svc.dnsProbeDue() {
		t.Fatal("probe schedule survived a usable-network event")
	}
}

// A failed bring-up's own teardown fires route events a gap after Connect set its
// retry grace; they must not start a rebuild that races the app's server cascade.
func TestNetworkChangeFromOwnTeardownKeepsConnectRetryGrace(t *testing.T) {
	naive := &fakeNaiveManager{startErr: errors.New("naive: handshake did not complete")}
	svc := newTestService(t, &fakeCloakManager{}, naive, &fakeWGManager{}, &fakeKillSwitch{}, silentTunnelProfile())
	ctx := context.Background()
	err := svc.Connect(ctx, "p1", ConnectOptions{PreferredTransport: "naive"})
	if err == nil || errors.Is(err, ErrHostOffline) {
		t.Fatalf("Connect error = %v, want a failed transport", err)
	}
	restoreNetwork(naive)

	svc.onNetworkChanged()
	svc.runHealthCheck(ctx)
	if transportStarted(naive) {
		t.Fatal("a network event on the unchanged network started a rebuild inside connectRetryGrace")
	}
}

// Only a move off the network a retry was booked on cuts its wait short.
func TestNetworkChangeCutsBookedRetryOnlyOnARealMove(t *testing.T) {
	bookings := []struct {
		name string
		book func(*Service)
	}{
		{"connect retry grace", func(svc *Service) { svc.deferRecovery(connectRetryGrace) }},
		{"rebuild backoff", func(svc *Service) { svc.scheduleNextRecovery(6) }},
	}
	cases := []struct {
		name    string
		events  []string
		wantDue bool
	}{
		{"same network", []string{"net-a", "net-a"}, false},
		{"moved network", []string{"net-b"}, true},
		{"lost then back", []string{"", "net-a"}, true},
	}
	for _, booking := range bookings {
		for _, tc := range cases {
			t.Run(booking.name+"/"+tc.name, func(t *testing.T) {
				svc, _ := newSysEventsTestService(t)
				network := "net-a"
				svc.networkKey = func() string { return network }
				booking.book(svc)

				for _, next := range tc.events {
					network = next
					svc.onNetworkChanged()
				}
				if due := svc.recoveryDue(); due != tc.wantDue {
					t.Fatalf("recovery due = %v after events on %q, want %v", due, tc.events, tc.wantDue)
				}
			})
		}
	}
}

func TestNetworkChangeIgnoredWhileHostHasNoNetwork(t *testing.T) {
	svc, _ := newSysEventsTestService(t)
	svc.networkKey = func() string { return "" }

	svc.onSystemResume(context.Background(), "test resume")
	svc.onNetworkChanged()

	if !svc.healthHeld() {
		t.Fatal("health hold was released even though the host has no usable network")
	}
}

func TestWatchSystemEventsRoutesResumeToHandler(t *testing.T) {
	svc, wgMgr := newSysEventsTestService(t)
	events := make(chan platform.SystemEvent, 1)
	svc.systemEvents = func(context.Context) (<-chan platform.SystemEvent, error) {
		return events, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		svc.watchSystemEvents(ctx)
		close(done)
	}()

	events <- platform.SystemEventResumed
	deadline := time.After(2 * time.Second)
	for {
		wgMgr.mu.Lock()
		rebinds := wgMgr.rebindCount
		wgMgr.mu.Unlock()
		if rebinds == 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("resume event was not routed to the resume handler")
		case <-time.After(10 * time.Millisecond):
		}
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("watchSystemEvents did not stop on context cancellation")
	}
}
