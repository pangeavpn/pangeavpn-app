package api

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// Two profiles differing only by ID / node host, every transport configured.
func switchProfilePair() (state.Profile, state.Profile) {
	mk := func(id, host string) state.Profile {
		return state.Profile{
			ID:        id,
			Name:      id,
			Cloak:     state.CloakProfile{RemoteHost: host, RemotePort: 443, LocalPort: 51821},
			Naive:     &state.NaiveProfile{RemoteHost: "naive-" + id + ".example.com", RemotePort: 8443, Username: "u", Password: "p"},
			Reality:   &state.RealityProfile{RemoteHost: host, RemotePort: 8444, UUID: "u", PublicKey: "k", ShortID: "ab12"},
			Hysteria2: &state.Hysteria2Profile{RemoteHost: host, RemotePort: 443, Password: "p", ObfsPassword: "o"},
			WireGuard: state.WireGuardProfile{
				TunnelName: "pangea0",
				ConfigText: "[Interface]\nPrivateKey=x\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
			},
		}
	}
	return mk("srv-a", "10.0.0.1"), mk("srv-b", "10.0.0.2")
}

// Connect to A on a pinned transport, switch to B, expect Connected on B.
func TestSwitch_PerTransport(t *testing.T) {
	for _, kind := range []string{"cloak", "reality", "hysteria2", "naive"} {
		t.Run(kind, func(t *testing.T) {
			a, b := switchProfilePair()

			cloakMgr := &fakeCloakManager{}
			naiveMgr := &fakeNaiveManager{}
			realityMgr := &fakeRealityManager{}
			hy2Mgr := &fakeHysteria2Manager{}
			sfMgr := &fakeSnowflakeManager{}
			wgMgr := &fakeWGManager{}
			ks := &fakeKillSwitch{}
			svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hy2Mgr, &fakeShadowsocksManager{}, sfMgr, wgMgr, ks, a, b)

			opts := ConnectOptions{PreferredTransport: kind}
			if err := svc.Connect(context.Background(), a.ID, opts); err != nil {
				t.Fatalf("Connect(%s): %v", a.ID, err)
			}
			if got := svc.Status(context.Background()); got.State != state.StateConnected {
				t.Fatalf("after Connect: state = %s (%s)", got.State, got.Detail)
			}

			if err := svc.Switch(context.Background(), b.ID, opts); err != nil {
				t.Fatalf("Switch(%s -> %s) on %s: %v", a.ID, b.ID, kind, err)
			}

			status := svc.Status(context.Background())
			if status.State != state.StateConnected {
				t.Fatalf("after Switch: state = %s (%s)", status.State, status.Detail)
			}
			if status.ActiveTransport != kind {
				t.Fatalf("after Switch: ActiveTransport = %q, want %q", status.ActiveTransport, kind)
			}
			active, ok := svc.getCurrentProfile()
			if !ok || active.ID != b.ID {
				t.Fatalf("after Switch: current profile = %+v, want %s", active.ID, b.ID)
			}
		})
	}
}

// Re-arming after teardown stranded the user offline when Enable failed (in the
// field: a permit host the lock's own DNS block could not resolve).
func TestSwitch_KillSwitchReEnableFailure_KeepsWorkingSession(t *testing.T) {
	a, b := switchProfilePair()

	cloakMgr := &fakeCloakManager{}
	naiveMgr := &fakeNaiveManager{}
	realityMgr := &fakeRealityManager{}
	hy2Mgr := &fakeHysteria2Manager{}
	sfMgr := &fakeSnowflakeManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, cloakMgr, naiveMgr, realityMgr, hy2Mgr, &fakeShadowsocksManager{}, sfMgr, wgMgr, ks, a, b)

	opts := ConnectOptions{PreferredTransport: "hysteria2"}
	if err := svc.Connect(context.Background(), a.ID, opts); err != nil {
		t.Fatalf("Connect(%s): %v", a.ID, err)
	}

	wgMgr.mu.Lock()
	stopsBefore := wgMgr.stopCount
	wgMgr.mu.Unlock()

	ks.mu.Lock()
	ks.enableErr = errors.New("resolve endpoint naive-b.example.com: no such host")
	ks.mu.Unlock()

	if err := svc.Switch(context.Background(), b.ID, opts); err == nil {
		t.Fatal("expected Switch to fail when the kill switch cannot be re-armed")
	}

	wgMgr.mu.Lock()
	stopsAfter := wgMgr.stopCount
	wgMgr.mu.Unlock()
	if stopsAfter != stopsBefore {
		t.Fatalf("wireguard was torn down (%d -> %d stops) before the kill switch was re-armed; "+
			"a failed re-arm must not cost the user their working session", stopsBefore, stopsAfter)
	}

	hy2Mgr.mu.Lock()
	hy2Stopped := hy2Mgr.stopCalled
	hy2Running := hy2Mgr.running
	hy2Mgr.mu.Unlock()
	if hy2Stopped || !hy2Running {
		t.Fatal("the active transport was stopped before the kill switch was re-armed")
	}

	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %s (%s), want still CONNECTED on the original server", status.State, status.Detail)
	}
	if status.ActiveTransport != "hysteria2" {
		t.Fatalf("ActiveTransport = %q, want hysteria2", status.ActiveTransport)
	}
	active, ok := svc.getCurrentProfile()
	if !ok || active.ID != a.ID {
		t.Fatalf("current profile = %q, want the original %s", active.ID, a.ID)
	}
}

// The other pre-teardown refusal: a bad new profile must cost nothing.
func TestSwitch_PreflightFailure_KeepsWorkingSession(t *testing.T) {
	a, b := switchProfilePair()

	hy2Mgr := &fakeHysteria2Manager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		hy2Mgr, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, ks, a, b)

	opts := ConnectOptions{PreferredTransport: "hysteria2"}
	if err := svc.Connect(context.Background(), a.ID, opts); err != nil {
		t.Fatalf("Connect(%s): %v", a.ID, err)
	}

	wgMgr.mu.Lock()
	stopsBefore := wgMgr.stopCount
	wgMgr.preflightErr = errors.New("ipv6 [interface] address is not supported")
	wgMgr.mu.Unlock()

	if err := svc.Switch(context.Background(), b.ID, opts); err == nil {
		t.Fatal("expected Switch to fail when the new profile fails preflight")
	}

	wgMgr.mu.Lock()
	stopsAfter := wgMgr.stopCount
	wgMgr.mu.Unlock()
	if stopsAfter != stopsBefore {
		t.Fatalf("wireguard was torn down (%d -> %d stops) on a preflight refusal", stopsBefore, stopsAfter)
	}
	if status := svc.Status(context.Background()); status.State != state.StateConnected {
		t.Fatalf("state = %s (%s), want still CONNECTED — StateError would stop the health "+
			"loop watching a live tunnel and block the retry", status.State, status.Detail)
	}
	// The retry the Connected state exists to allow must actually be accepted.
	wgMgr.mu.Lock()
	wgMgr.preflightErr = nil
	wgMgr.mu.Unlock()
	if err := svc.Switch(context.Background(), b.ID, opts); err != nil {
		t.Fatalf("retry after a refused switch: %v", err)
	}
	if active, ok := svc.getCurrentProfile(); !ok || active.ID != b.ID {
		t.Fatalf("after retry: current profile = %q, want %s", active.ID, b.ID)
	}
}

// Re-arming with the old server's permits would block the new transport.
func TestSwitch_ArmsKillSwitchForNewServer(t *testing.T) {
	a, b := switchProfilePair()

	ks := &fakeKillSwitch{}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		&fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, &fakeWGManager{}, ks, a, b)

	opts := ConnectOptions{PreferredTransport: "hysteria2"}
	if err := svc.Connect(context.Background(), a.ID, opts); err != nil {
		t.Fatalf("Connect(%s): %v", a.ID, err)
	}
	if err := svc.Switch(context.Background(), b.ID, opts); err != nil {
		t.Fatalf("Switch: %v", err)
	}

	ks.mu.Lock()
	endpoints := append([]string(nil), ks.enableEndpoints...)
	ks.mu.Unlock()

	if !slices.Contains(endpoints, b.Cloak.RemoteHost) {
		t.Fatalf("kill switch armed with %v, missing the new server's endpoint %s", endpoints, b.Cloak.RemoteHost)
	}
	if !slices.Contains(endpoints, b.Hysteria2.RemoteHost) {
		t.Fatalf("kill switch armed with %v, missing the new server's hysteria2 endpoint %s", endpoints, b.Hysteria2.RemoteHost)
	}
}

// fakeInPlaceWGManager models a manager (Windows) whose live device can be
// re-pointed at a new server without being rebuilt.
type fakeInPlaceWGManager struct {
	fakeWGManager
	pinCount int
}

func (f *fakeInPlaceWGManager) PinEndpointRoutes(_ context.Context, _ state.WireGuardProfile) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pinCount++
	return nil
}

// TestSwitch_InPlaceManagerKeepsDevice proves a switch neither stops the
// device nor skips pre-routing the new endpoints when the manager can
// reconfigure in place.
func TestSwitch_InPlaceManagerKeepsDevice(t *testing.T) {
	a, b := switchProfilePair()
	wgMgr := &fakeInPlaceWGManager{}
	ks := &fakeKillSwitch{}
	machine := state.NewMachine()
	logs := state.NewLogStore(100)
	config := testConfigStore(t, a, b)
	svc := NewService(machine, logs, config, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{}, &fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, ks)
	svc.handshakeTimeout = 200 * time.Millisecond
	svc.networkRepair = func(context.Context, []string) ([]string, error) { return nil, nil }

	opts := ConnectOptions{PreferredTransport: "cloak"}
	if err := svc.Connect(context.Background(), a.ID, opts); err != nil {
		t.Fatalf("Connect(%s): %v", a.ID, err)
	}
	wgMgr.mu.Lock()
	stopsBefore := wgMgr.stopCount
	wgMgr.mu.Unlock()

	if err := svc.Switch(context.Background(), b.ID, opts); err != nil {
		t.Fatalf("Switch(%s -> %s): %v", a.ID, b.ID, err)
	}

	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	if wgMgr.pinCount != 1 {
		t.Errorf("PinEndpointRoutes called %d times, want 1", wgMgr.pinCount)
	}
	if wgMgr.stopCount != stopsBefore {
		t.Errorf("switch stopped the device (%d -> %d stops); in-place manager should keep it", stopsBefore, wgMgr.stopCount)
	}
}
