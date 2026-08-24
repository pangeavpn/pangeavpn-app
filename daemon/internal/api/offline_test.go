package api

import (
	"context"
	"errors"
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

// The exact churn a link drop caused: the active transport dies, its restart
// gets "unreachable network", and instead of stamping ERROR every ~3s (which a
// still-recent handshake flips back to CONNECTED) the daemon parks in a stable
// offline hold and reports offline, then recovers when the link returns.
func TestHealthCheck_TransportRestartHoldsOnUnreachableNetwork(t *testing.T) {
	svc, naive, _, _ := recoveryTestService(t)

	// Link drops mid-session: the transport is down and its restart can't route
	// out. hostInternet stays "unknown" so this exercises the instant signal.
	naive.mu.Lock()
	naive.running = false
	naive.startErr = errors.New("dial tcp 95.179.239.1:443: connectex: A socket operation was attempted to an unreachable network.")
	naive.mu.Unlock()

	svc.runHealthCheck(context.Background())

	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED held in the offline hold, not flipped to ERROR", status.State)
	}
	if !status.Offline {
		t.Error("Offline = false, want true after an unreachable-network restart")
	}
	if !svc.offlineHoldActive() {
		t.Error("offline hold not active after an unreachable-network restart")
	}

	// Ticks within the hold must not hammer restart or thrash the state.
	naive.mu.Lock()
	naive.startCalled = false
	naive.mu.Unlock()
	svc.runHealthCheck(context.Background())
	svc.runHealthCheck(context.Background())
	if transportStarted(naive) {
		t.Error("transport restart was hammered while in the offline hold")
	}
	if st, _ := svc.machine.Get(); st != state.StateConnected {
		t.Errorf("state = %q during hold, want a stable CONNECTED (no ERROR thrash)", st)
	}

	// The link returns: a network-change event clears the hold and recovery runs.
	naive.mu.Lock()
	naive.startErr = nil
	naive.running = true
	naive.mu.Unlock()
	svc.onNetworkChanged()
	svc.runHealthCheck(context.Background())
	if svc.Status(context.Background()).Offline {
		t.Error("Offline = true after the link returned")
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
