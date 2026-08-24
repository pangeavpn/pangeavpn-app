package api

import (
	"context"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestOfflineForState(t *testing.T) {
	cases := []struct {
		st          state.DaemonState
		hostOffline bool
		want        bool
	}{
		{state.StateConnected, true, true},
		{state.StateConnecting, true, true},
		{state.StateError, true, true},
		{state.StateDisconnected, true, false},
		{state.StateDisconnecting, true, false},
		{state.StateConnected, false, false},
		{state.StateError, false, false},
	}
	for _, c := range cases {
		if got := offlineForState(c.st, c.hostOffline); got != c.want {
			t.Errorf("offlineForState(%q, %v) = %v, want %v", c.st, c.hostOffline, got, c.want)
		}
	}
}

// When the OS reports no internet, a dropped session must hold — not churn
// through rebuild attempts flipping between ERROR/CONNECTED/CONNECTING — and the
// status must carry the offline flag so the UI can say "no internet".
func TestHealthCheck_HoldsWhenHostOffline(t *testing.T) {
	svc, naive, wgMgr, _ := recoveryTestService(t)
	svc.hostInternet = func() (bool, bool) { return false, true }

	dropSession(naive, wgMgr)
	for range 4 {
		svc.runHealthCheck(context.Background())
	}

	status := svc.Status(context.Background())
	if !status.Offline {
		t.Errorf("Offline = false, want true while the OS reports no internet")
	}
	if status.Reconnecting {
		t.Error("Reconnecting = true, want a stable hold rather than churning attempts while offline")
	}
	if svc.recoveryPending() {
		t.Error("recovery attempts accumulated while offline; expected a hold, not a retry loop")
	}

	// Link returns: recovery resumes and the session comes back, offline clears.
	svc.hostInternet = func() (bool, bool) { return true, true }
	restoreNetwork(naive)
	for range 3 {
		svc.runHealthCheck(context.Background())
	}
	status = svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q (%s), want CONNECTED after the link returned", status.State, status.Detail)
	}
	if status.Offline {
		t.Error("Offline = true after reconnecting")
	}
}

// From an existing ERROR state, retryDroppedSession must not rebuild or flip to
// a transient CONNECTED while the host is offline.
func TestRetryDroppedSession_HoldsWhenOffline(t *testing.T) {
	svc, _, _, _ := recoveryTestService(t)
	svc.hostInternet = func() (bool, bool) { return false, true }
	svc.machine.Set(state.StateError, "connection lost")

	svc.retryDroppedSession(context.Background())

	if st, _ := svc.machine.Get(); st != state.StateError {
		t.Fatalf("state = %q, want it held at ERROR while offline", st)
	}
	if svc.recoveryPending() {
		t.Error("a rebuild was booked while offline; expected a hold")
	}
	if !svc.Status(context.Background()).Offline {
		t.Error("Offline = false in ERROR while the OS reports no internet")
	}
}
