package api

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// Once Connect has armed the lock, nothing but a user Disconnect may lower it:
// not a failed bring-up, not a daemon exit, not a restart with no session.

func TestConnect_FailedBringUpKeepsSessionForRecovery(t *testing.T) {
	profile := testProfile()
	ks := &fakeKillSwitch{}
	wgMgr := &fakeWGManager{startErr: errors.New("wg failed")}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, ks, profile)
	stub := stubSessionRecordStore(t)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{AllowLAN: true, PreferredTransport: "cloak"}); err == nil {
		t.Fatal("connect succeeded with a failing wireguard start")
	}

	if !ks.Active() || ks.clearCount != 0 {
		t.Fatalf("kill switch active=%v clears=%d, want armed and never cleared", ks.Active(), ks.clearCount)
	}
	current, ok := svc.getCurrentProfile()
	if !ok || current.ID != profile.ID {
		t.Fatalf("current profile = %q (ok=%v), want %q held for the retry loop", current.ID, ok, profile.ID)
	}
	rec, has := stub.get()
	if !has || rec.ProfileID != profile.ID || !rec.AllowLAN || rec.PreferredTransport != "cloak" {
		t.Fatalf("session record = %+v (has=%v), want the requested session persisted before bring-up", rec, has)
	}
	if currentState, _ := svc.machine.Get(); currentState != state.StateError {
		t.Fatalf("state = %v, want StateError so healthLoop keeps rebuilding", currentState)
	}
}

func TestShutdown_KeepsKillSwitchAndSessionRecordWhileConnected(t *testing.T) {
	profile := testProfile()
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	stub := stubSessionRecordStore(t)
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{"198.51.100.9"}})

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	enablesAfterConnect := ks.enableCount

	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}

	if ks.clearCount != 0 {
		t.Errorf("Clear() called %d times on shutdown, want 0: the user never pressed Disconnect", ks.clearCount)
	}
	if ks.enableCount != enablesAfterConnect {
		t.Errorf("Enable() called %d extra times on shutdown, want the permit set left for the reconnect", ks.enableCount-enablesAfterConnect)
	}
	if _, has := stub.get(); !has {
		t.Error("session record removed on shutdown, want it kept so the next start reconnects")
	}
}

func TestShutdown_KeepsLeftoverLockWithoutSession(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubSessionRecordStore(t)
	stubKillSwitchState(t, platform.KillSwitchState{Active: true})

	if err := svc.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
	if ks.clearCount != 0 {
		t.Errorf("Clear() called %d times, want 0: only a user Disconnect lowers the lock", ks.clearCount)
	}
}

func TestReconcileStartup_CrashWithoutSessionRecordHoldsKillSwitch(t *testing.T) {
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubSessionRecordStore(t)
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{"198.51.100.9"}})

	svc.reconcileStartup(context.Background())

	if ks.clearCount != 0 {
		t.Errorf("Clear() called %d times, want 0", ks.clearCount)
	}
	if ks.enableCount != 1 || !slices.Contains(ks.enableEndpoints, "198.51.100.9") {
		t.Errorf("Enable() calls=%d endpoints=%v, want one re-arm from persisted IPs", ks.enableCount, ks.enableEndpoints)
	}
	if currentState, _ := svc.machine.Get(); currentState != state.StateError {
		t.Errorf("state = %v, want StateError so the UI offers Connect or Disconnect", currentState)
	}
	if _, ok := svc.getCurrentProfile(); ok {
		t.Error("current profile set with no recorded session; nothing should be redialled")
	}
}

func TestReconcileStartup_CrashWithUnknownProfileHoldsKillSwitch(t *testing.T) {
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stub := stubSessionRecordStore(t)
	stub.set(sessionRecord{ProfileID: "gone-profile"})
	stubKillSwitchState(t, platform.KillSwitchState{Active: true})

	svc.reconcileStartup(context.Background())

	if ks.clearCount != 0 {
		t.Errorf("Clear() called %d times, want 0", ks.clearCount)
	}
	if !ks.Active() {
		t.Error("kill switch not re-armed after a crash")
	}
	if currentState, _ := svc.machine.Get(); currentState != state.StateError {
		t.Errorf("state = %v, want StateError", currentState)
	}
}

func TestReconcileStartup_LiveLockWithoutStateFileIsHeld(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubSessionRecordStore(t)
	stubKillSwitchState(t, platform.KillSwitchState{})

	svc.reconcileStartup(context.Background())

	if ks.clearCount != 0 {
		t.Errorf("Clear() called %d times, want 0: live rules with no state file are a lock, not litter", ks.clearCount)
	}
	if currentState, _ := svc.machine.Get(); currentState != state.StateError {
		t.Errorf("state = %v, want StateError", currentState)
	}
}

func TestReconcileStartup_LockdownCrashAlsoReconnectsRecordedSession(t *testing.T) {
	profile := testProfile()
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	stub := stubSessionRecordStore(t)
	stub.set(sessionRecord{ProfileID: profile.ID, Lockdown: true})
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true, EndpointIPs: []string{"198.51.100.9"}})

	svc.reconcileStartup(context.Background())

	if ks.clearCount != 0 || !ks.enableLocked {
		t.Errorf("clears=%d locked=%v, want the lockdown lock re-armed untouched", ks.clearCount, ks.enableLocked)
	}
	current, ok := svc.getCurrentProfile()
	if !ok || current.ID != profile.ID {
		t.Fatalf("current profile = %q (ok=%v), want the recorded session redialled under lockdown too", current.ID, ok)
	}
	if currentState, _ := svc.machine.Get(); currentState != state.StateError {
		t.Errorf("state = %v, want StateError so the health loop redials", currentState)
	}
}

func TestClearKillSwitch_RefusedWhileSessionHeld(t *testing.T) {
	profile := testProfile()
	ks := &fakeKillSwitch{}
	wgMgr := &fakeWGManager{startErr: errors.New("wg failed")}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, wgMgr, ks, profile)
	stubSessionRecordStore(t)

	_ = svc.Connect(context.Background(), profile.ID, ConnectOptions{})

	if err := svc.ClearKillSwitch(context.Background()); err == nil {
		t.Fatal("ClearKillSwitch succeeded while a session is held; want a refusal pointing at Disconnect")
	}
	if ks.clearCount != 0 || !ks.Active() {
		t.Errorf("clears=%d active=%v, want the lock untouched", ks.clearCount, ks.Active())
	}
}

func TestClearKillSwitch_AllowedWhenIdle(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubSessionRecordStore(t)

	if err := svc.ClearKillSwitch(context.Background()); err != nil {
		t.Fatalf("ClearKillSwitch() error = %v, want an idle lockdown lock cleared on request", err)
	}
	if ks.clearCount != 1 {
		t.Errorf("Clear() called %d times, want 1", ks.clearCount)
	}
}

// A resolver the profile names is routed into the tunnel (Allow LAN re-includes
// it), so an interface-wide permit only ever lets queries out once the tunnel is down.
func TestKillSwitchPermits_NeverPermitsAResolverOutsideTheTunnel(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.DNS = []string{"192.168.1.2", "10.8.0.1", "1.1.1.1"}

	for _, allowLAN := range []bool{true, false} {
		permits := killSwitchPermitsFor(profile, allowLAN)
		for _, resolver := range profile.WireGuard.DNS {
			if slices.Contains(permits, resolver) {
				t.Errorf("allowLAN=%v: permits %v include resolver %s; with the tunnel down its queries would leave on the physical NIC", allowLAN, permits, resolver)
			}
		}
	}
}

// Under Lockdown the lock outlives the session, but the tunnel permit must not:
// macOS hands the same utunN to the next creator, Windows can reuse a LUID index.
func TestDisconnect_LockdownDropsTheTunnelPermit(t *testing.T) {
	profile := testProfile()
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{Lockdown: true}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if ks.updateCount == 0 {
		t.Fatal("connect never permitted the tunnel; the test would prove nothing")
	}
	if err := svc.Disconnect(context.Background(), true); err != nil {
		t.Fatalf("lockdown disconnect failed: %v", err)
	}

	if ks.dropTunnelCount != 1 {
		t.Errorf("DropTunnelPermit() called %d times, want 1: the dead tunnel's permit stays in the idle lock", ks.dropTunnelCount)
	}
	if ks.clearCount != 0 || !ks.Active() {
		t.Errorf("clears=%d active=%v, want the lock itself kept", ks.clearCount, ks.Active())
	}
}

// While a session runs the hub is reachable through the tunnel, so a permit for
// an address no stored profile vouches for has no honest use: it is an exfil hole.
func TestPermitHosts_RefusesAnUnknownIPWhileASessionRuns(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.BypassHosts = []string{"198.51.100.1"}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{Lockdown: true}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true, EndpointIPs: []string{"198.51.100.1"}})
	enables := ks.enableCount

	if err := svc.PermitHosts(context.Background(), []string{"203.0.113.9"}); err == nil {
		t.Fatal("PermitHosts() accepted an address no profile vouches for while connected")
	}
	if ks.enableCount != enables {
		t.Errorf("Enable() called %d extra times, want the lock untouched", ks.enableCount-enables)
	}
}

// The hub's own address, as recorded at provisioning, stays permittable at any time.
func TestPermitHosts_AcceptsTheStoredHubIPWhileASessionRuns(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.BypassHosts = []string{"198.51.100.1"}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{Lockdown: true}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true, EndpointIPs: []string{"192.0.2.50"}})

	if err := svc.PermitHosts(context.Background(), []string{"198.51.100.1"}); err != nil {
		t.Fatalf("PermitHosts() refused the stored hub IP: %v", err)
	}
	if !slices.Contains(ks.enableEndpoints, "198.51.100.1") {
		t.Errorf("Enable() endpoints = %v, want the hub IP permitted", ks.enableEndpoints)
	}
}
