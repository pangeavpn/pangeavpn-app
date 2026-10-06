package api

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reach"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// holdTestService is a connected naive session with a stubbed resolver probe and
// hub, where the direct hub route is already proven on this network.
func holdTestService(t *testing.T) (*Service, *fakeProbe, *fakeReach, *fakeNaiveManager) {
	t.Helper()
	profile := deadDataPathProfile()
	profile.WireGuard.BypassHosts = []string{testHubIP}
	naive := &fakeNaiveManager{}
	svc := newTestService(t, &fakeCloakManager{}, naive, &fakeWGManager{}, &fakeKillSwitch{}, profile)
	svc.recoveryDelays = []time.Duration{0}
	svc.physicalRoute = func() (string, string, error) { return "eth0", "192.0.2.1", nil }
	probe := &fakeProbe{}
	svc.probeResolver = probe.probe
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	probe.forget()
	hub := newFakeReach()
	svc.reachProbe = hub.probe
	svc.reachBaseline.record(testNetwork, directRouteID)
	return svc, probe, hub, naive
}

// killDataPath fails the tunnel's resolver probes until the health loop acts.
func killDataPath(svc *Service, probe *fakeProbe) {
	probe.setErr(errors.New("i/o timeout"))
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)
}

func forceUpstreamProbeDue(svc *Service) {
	svc.recoveryMu.Lock()
	defer svc.recoveryMu.Unlock()
	svc.upstreamProbeAt = time.Time{}
}

func transportRestarted(naive *fakeNaiveManager) bool {
	naive.mu.Lock()
	defer naive.mu.Unlock()
	return naive.stopCalled
}

func TestHealthCheck_SilentNetworkHoldsInsteadOfRebuilding(t *testing.T) {
	svc, probe, hub, naive := holdTestService(t)
	killDataPath(svc, probe)
	if transportRestarted(naive) {
		t.Fatal("the transport was torn down; a silent network should park the session, not burn the cascade")
	}
	status := svc.Status(context.Background())
	if !status.Offline {
		t.Error("status.Offline = false; the app must show it is waiting for the network")
	}
	if status.TransportsExhausted {
		t.Error("TransportsExhausted = true; the app would rotate away from a working server")
	}
	if status.State != state.StateConnected {
		t.Errorf("state = %q, want CONNECTED while holding", status.State)
	}
	if !svc.upstreamHoldActive() || hub.calls() == 0 {
		t.Fatalf("hold active = %v after %d hub probes", svc.upstreamHoldActive(), hub.calls())
	}
}

func TestHealthCheck_HoldKeepsWaitingWhileTheHubIsSilent(t *testing.T) {
	svc, probe, hub, naive := holdTestService(t)
	killDataPath(svc, probe)
	before := hub.calls()
	for range 3 {
		forceUpstreamProbeDue(svc)
		svc.runHealthCheck(context.Background())
	}
	if transportRestarted(naive) || !svc.upstreamHoldActive() {
		t.Fatal("the hold ended while the hub was still silent")
	}
	if hub.calls() <= before {
		t.Fatal("the hold stopped asking the hub")
	}
}

func TestHealthCheck_HoldRebuildsTheMomentTheHubAnswers(t *testing.T) {
	svc, probe, hub, naive := holdTestService(t)
	killDataPath(svc, probe)
	hub.set(directRouteID, reach.Answered)
	probe.setErr(nil)
	forceUpstreamProbeDue(svc)
	svc.runHealthCheck(context.Background())
	if !transportRestarted(naive) {
		t.Fatal("the hub answered but the tunnel was not rebuilt")
	}
	status := svc.Status(context.Background())
	if svc.upstreamHoldActive() || status.Offline || status.State != state.StateConnected {
		t.Fatalf("hold=%v offline=%v state=%q, want a rebuilt CONNECTED session", svc.upstreamHoldActive(), status.Offline, status.State)
	}
}

func TestHealthCheck_HoldGivesUpAfterTheTimeBox(t *testing.T) {
	svc, probe, _, naive := holdTestService(t)
	killDataPath(svc, probe)
	svc.recoveryMu.Lock()
	svc.upstreamHoldUntil = time.Now().Add(-time.Second)
	svc.recoveryMu.Unlock()
	probe.setErr(nil)
	svc.runHealthCheck(context.Background())
	if !transportRestarted(naive) {
		t.Fatal("the time box passed but the cascade never ran")
	}
	if svc.reachBaseline.proven(testNetwork, directRouteID) {
		t.Fatal("the baseline survived the time box, so the next outage would hold again")
	}
	if svc.upstreamHoldActive() {
		t.Fatal("hold still active after the time box")
	}
}

func TestHealthCheck_HubAnswerMeansTheCascadeRunsNow(t *testing.T) {
	svc, probe, hub, naive := holdTestService(t)
	hub.set(directRouteID, reach.Answered)
	killDataPath(svc, probe)
	if svc.upstreamHoldActive() || !transportRestarted(naive) {
		t.Fatal("the network works, so the server is at fault: the cascade must run straight away")
	}
}

func TestHealthCheck_UnprovenSilenceFallsBackToTheCascade(t *testing.T) {
	svc, probe, _, naive := holdTestService(t)
	svc.reachBaseline.forget(testNetwork)
	killDataPath(svc, probe)
	if svc.upstreamHoldActive() || !transportRestarted(naive) {
		t.Fatal("silence on a never-proven route must not hold; that is a censored network's normal state")
	}
}

func TestHealthCheck_HoldSkipsProbesWithoutAPhysicalRoute(t *testing.T) {
	svc, probe, hub, naive := holdTestService(t)
	killDataPath(svc, probe)
	before := hub.calls()
	svc.physicalRoute = func() (string, string, error) { return "", "", platform.ErrNoDefaultRoute }
	forceUpstreamProbeDue(svc)
	svc.runHealthCheck(context.Background())
	if hub.calls() != before || transportRestarted(naive) || !svc.upstreamHoldActive() {
		t.Fatal("with no route out the hold must keep waiting quietly")
	}
}

func TestHealthCheck_DisconnectEndsTheHold(t *testing.T) {
	svc, probe, _, _ := holdTestService(t)
	killDataPath(svc, probe)
	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if svc.upstreamHoldActive() || svc.Status(context.Background()).Offline {
		t.Fatal("a disconnect left the hold, and its offline flag, behind")
	}
}

func TestHealthCheck_SwitchEndsTheHold(t *testing.T) {
	svc, probe, _, naive := holdTestService(t)
	killDataPath(svc, probe)
	other, _ := svc.getCurrentProfile()
	other.ID = "p2"
	svc.setCurrentProfile(other)
	svc.runHealthCheck(context.Background())
	if svc.upstreamHoldActive() {
		t.Fatal("the hold outlived the session it was holding")
	}
	if transportRestarted(naive) {
		t.Fatal("a stale hold fired a rebuild for the old session")
	}
}

func TestHealthCheck_RoamingMidHoldFallsBackToTheCascade(t *testing.T) {
	svc, probe, _, naive := holdTestService(t)
	killDataPath(svc, probe)
	svc.networkKey = func() string { return "wlan0:198.51.100.10" }
	probe.setErr(nil)
	forceUpstreamProbeDue(svc)
	svc.runHealthCheck(context.Background())
	if svc.upstreamHoldActive() || !transportRestarted(naive) {
		t.Fatal("a new network with no baseline must end the hold and run the cascade")
	}
}

func TestOnNetworkChanged_BringsTheHubProbeForward(t *testing.T) {
	svc, probe, _, _ := holdTestService(t)
	killDataPath(svc, probe)
	svc.onNetworkChanged()
	svc.recoveryMu.Lock()
	due := svc.upstreamProbeAt.IsZero()
	svc.recoveryMu.Unlock()
	if !due {
		t.Fatal("a network change must re-ask the hub on the next tick")
	}
}

func TestHoldIfNetworkSilent(t *testing.T) {
	exhausted := fmt.Errorf("%w: %w", ErrTransportExhausted, errors.New("dial tcp 198.51.100.1:443: i/o timeout"))
	unreachable := fmt.Errorf("%w: %w", ErrTransportExhausted, errors.New("connectex: A socket operation was attempted to an unreachable network."))
	cases := []struct {
		name     string
		err      error
		hub      reach.Outcome
		wantHeld bool
	}{
		{"exhausted on a silent network", exhausted, reach.Silent, true},
		{"exhausted while the hub answers", exhausted, reach.Answered, false},
		{"no route out", unreachable, reach.Silent, false},
		{"a single transport's failure", errors.New("naive: no wireguard handshake within 10s"), reach.Silent, false},
		{"success", nil, reach.Silent, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, hub, _ := holdTestService(t)
			hub.set(directRouteID, tc.hub)
			profile, _ := svc.getCurrentProfile()
			if got := svc.holdIfNetworkSilent(context.Background(), profile, tc.err); got != tc.wantHeld {
				t.Fatalf("held = %v, want %v", got, tc.wantHeld)
			}
			if !tc.wantHeld {
				return
			}
			status := svc.Status(context.Background())
			if status.State != state.StateError || status.Detail != offlineHoldDetail || !status.Offline || status.TransportsExhausted {
				t.Fatalf("status = %+v, want ERROR/%q, offline, not exhausted", status, offlineHoldDetail)
			}
		})
	}
}

func TestConnect_ProvesTheHubRouteOncePerRefreshWindow(t *testing.T) {
	profile := deadDataPathProfile()
	profile.WireGuard.BypassHosts = []string{testHubIP}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)
	svc.physicalRoute = func() (string, string, error) { return "eth0", "192.0.2.1", nil }
	svc.probeResolver = (&fakeProbe{}).probe
	hub := newFakeReach()
	hub.set(directRouteID, reach.Answered)
	svc.reachProbe = hub.probe

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for !svc.reachBaseline.proven(testNetwork, directRouteID) {
		if time.Now().After(deadline) {
			t.Fatal("connect never proved the hub route")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := svc.Disconnect(context.Background(), false); err != nil {
		t.Fatalf("disconnect: %v", err)
	}
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if hub.calls() != 1 {
		t.Fatalf("hub probed %d times; a fresh baseline must not be re-proven on every connect", hub.calls())
	}
}

// switchDuringProbe makes the next hub probe land a switch to another server,
// as a user clicking a new server while the tunnel looks dead would.
func switchDuringProbe(svc *Service, hub *fakeReach) {
	hub.duringProbe = func() {
		other, _ := svc.getCurrentProfile()
		other.ID = "p2"
		svc.setCurrentProfile(other)
		hub.duringProbe = nil
	}
}

func TestHealthCheck_SwitchDuringTheHoldProbeRebuildsNothing(t *testing.T) {
	svc, probe, hub, naive := holdTestService(t)
	killDataPath(svc, probe)
	hub.set(directRouteID, reach.Answered)
	switchDuringProbe(svc, hub)
	forceUpstreamProbeDue(svc)
	svc.runHealthCheck(context.Background())
	if transportRestarted(naive) {
		t.Fatal("the hold rebuilt the old server over the one the user just switched to")
	}
	if svc.upstreamHoldActive() {
		t.Fatal("the hold outlived the session it was holding")
	}
}

func TestHealthCheck_SwitchDuringTheEscalationProbeRebuildsNothing(t *testing.T) {
	svc, probe, hub, naive := holdTestService(t)
	hub.set(directRouteID, reach.Answered)
	switchDuringProbe(svc, hub)
	killDataPath(svc, probe)
	if transportRestarted(naive) {
		t.Fatal("the dead-path escalation rebuilt the old server after a switch landed")
	}
}

func TestHoldIfNetworkSilent_SwitchDuringTheProbeBooksNothing(t *testing.T) {
	svc, _, hub, _ := holdTestService(t)
	profile, _ := svc.getCurrentProfile()
	switchDuringProbe(svc, hub)
	exhausted := fmt.Errorf("%w: %w", ErrTransportExhausted, errors.New("i/o timeout"))
	if !svc.holdIfNetworkSilent(context.Background(), profile, exhausted) {
		t.Fatal("a stale cascade result was handed back to be booked against the new session")
	}
	status := svc.Status(context.Background())
	if svc.upstreamHoldActive() || status.State == state.StateError || status.Offline {
		t.Fatalf("hold=%v status=%+v; nothing may be stamped on the session the user switched to", svc.upstreamHoldActive(), status)
	}
}

func TestConnect_SuccessEndsTheHold(t *testing.T) {
	svc, _, _, _ := holdTestService(t)
	profile, _ := svc.getCurrentProfile()
	exhausted := fmt.Errorf("%w: %w", ErrTransportExhausted, errors.New("i/o timeout"))
	if !svc.holdIfNetworkSilent(context.Background(), profile, exhausted) {
		t.Fatal("setup: expected the silent network to be held")
	}
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if svc.upstreamHoldActive() {
		t.Fatal("a working bring-up proves the network is back, yet the hold survived to rebuild it")
	}
}

// The hold owns the health tick, so it must not also hide a kill switch that was
// torn down underneath the session; that check guards against a traffic leak.
func TestHealthCheck_HoldStillCatchesAClearedKillSwitch(t *testing.T) {
	svc, probe, _, _ := holdTestService(t)
	killDataPath(svc, probe)
	if !svc.upstreamHoldActive() {
		t.Fatal("setup: expected a hold")
	}
	ks := svc.killSwitch.(*fakeKillSwitch)
	ks.mu.Lock()
	ks.active = false
	ks.mu.Unlock()
	svc.runHealthCheck(context.Background())
	status := svc.Status(context.Background())
	if status.State != state.StateError || !strings.Contains(status.Detail, "kill switch") {
		t.Fatalf("status = %q / %q; a cleared kill switch must surface even while holding", status.State, status.Detail)
	}
}

// autoHoldTestService is holdTestService in auto mode, so a rebuild whose every
// transport fails comes back as ErrTransportExhausted, the app's rotation cue.
func autoHoldTestService(t *testing.T) (*Service, *fakeCloakManager, *fakeNaiveManager) {
	t.Helper()
	profile := deadDataPathProfile()
	profile.WireGuard.BypassHosts = []string{testHubIP}
	cloak, naive := &fakeCloakManager{}, &fakeNaiveManager{}
	svc := newTestService(t, cloak, naive, &fakeWGManager{}, &fakeKillSwitch{}, profile)
	svc.recoveryDelays = []time.Duration{0}
	svc.physicalRoute = func() (string, string, error) { return "eth0", "192.0.2.1", nil }
	svc.probeResolver = (&fakeProbe{}).probe
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	svc.reachProbe = newFakeReach().probe
	svc.reachBaseline.record(testNetwork, directRouteID)
	timeout := errors.New("dial tcp 198.51.100.1:443: i/o timeout")
	cloak.mu.Lock()
	cloak.startErr = timeout
	cloak.mu.Unlock()
	naive.mu.Lock()
	naive.startErr = timeout
	naive.mu.Unlock()
	return svc, cloak, naive
}

func TestAttemptSessionRebuild_ExhaustionOnASilentNetworkDoesNotRotate(t *testing.T) {
	svc, _, _ := autoHoldTestService(t)
	profile, _ := svc.getCurrentProfile()
	svc.attemptSessionRebuild(context.Background(), profile, "tunnel stopped carrying traffic")
	status := svc.Status(context.Background())
	if status.TransportsExhausted {
		t.Fatal("TransportsExhausted = true; the app would rotate away because the Wi-Fi died")
	}
	if status.State != state.StateError || status.Detail != offlineHoldDetail || !status.Offline {
		t.Fatalf("status = %q / %q offline=%v, want the no-internet hold", status.State, status.Detail, status.Offline)
	}
	if svc.recoveryPending() {
		t.Fatal("a recovery attempt was booked; the hold, not the backoff, owns the next rebuild")
	}
}

func TestHealthCheck_TimeBoxExpiryLetsTheAppRotate(t *testing.T) {
	svc, _, _ := autoHoldTestService(t)
	profile, _ := svc.getCurrentProfile()
	svc.attemptSessionRebuild(context.Background(), profile, "tunnel stopped carrying traffic")
	if !svc.upstreamHoldActive() {
		t.Fatal("setup: expected the exhausted cascade to be held")
	}
	svc.recoveryMu.Lock()
	svc.upstreamHoldUntil = time.Now().Add(-time.Second)
	svc.recoveryMu.Unlock()
	svc.runHealthCheck(context.Background())
	if !svc.Status(context.Background()).TransportsExhausted {
		t.Fatal("after the time box the exhausted cascade must read as a blocked server again, or the app never rotates")
	}
}

// A silent network is not the transport's fault: holding must not mark it dead,
// or the next death would demote a transport that works here.
func TestHealthCheck_SilentNetworkDoesNotMarkTheTransportDead(t *testing.T) {
	svc, _, _ := autoHoldTestService(t)
	probe := &fakeProbe{}
	probe.setErr(errors.New("i/o timeout"))
	svc.probeResolver = probe.probe
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)
	if !svc.upstreamHoldActive() {
		t.Fatal("setup: expected the silent network to be held")
	}
	svc.recoveryMu.Lock()
	lastDead, lead := svc.lastDeadKind, svc.recoveryLead
	svc.recoveryMu.Unlock()
	if lastDead != "" || lead.kind != "" {
		t.Fatalf("lastDeadKind=%q lead=%+v; the hold marked a working transport dead", lastDead, lead)
	}
}
