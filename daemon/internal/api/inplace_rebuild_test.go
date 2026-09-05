package api

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// inPlaceCascadeService is cascadeTestService on a manager that re-points its
// live device instead of rebuilding it, as Windows and macOS do.
func inPlaceCascadeService(t *testing.T) (*Service, *gatedProbe, *fakeInPlaceWGManager, *fakeShadowsocksManager) {
	t.Helper()
	cloak := &fakeCloakManager{}
	reality := &fakeRealityManager{}
	shadowsocks := &fakeShadowsocksManager{}
	wgMgr := &fakeInPlaceWGManager{}
	config := testConfigStore(t, cascadeProfile())
	svc := NewService(state.NewMachine(), state.NewLogStore(100), config, cloak, &fakeNaiveManager{}, reality,
		&fakeHysteria2Manager{}, shadowsocks, &fakeSnowflakeManager{}, wgMgr, &fakeKillSwitch{})
	stubSessionRecordStore(t)
	svc.handshakeTimeout = 200 * time.Millisecond
	svc.networkRepair = func(context.Context, []string) ([]string, error) { return nil, nil }
	svc.networkKey = func() string { return "eth0:192.0.2.10" }
	svc.hostInternet = func() (bool, bool) { return false, false }
	svc.recoveryDelays = []time.Duration{0}
	probe := &gatedProbe{reality: reality, cloak: cloak, shadowsocks: shadowsocks}
	svc.probeResolver = probe.probe
	return svc, probe, wgMgr, shadowsocks
}

// connectOverReality brings the session up over reality and ages its handshake
// a little, so a re-pointed device must show a newer one to count.
func connectOverReality(t *testing.T, svc *Service, probe *gatedProbe, wgMgr *fakeInPlaceWGManager) (stops, starts int) {
	t.Helper()
	probe.reset(map[string]bool{"reality": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if got := svc.activeTransportKindSnapshot(); got != "reality" {
		t.Fatalf("active transport = %q, want reality", got)
	}
	ageHandshake(wgMgr)
	stops, starts, _ = deviceCounts(wgMgr)
	return stops, starts
}

func ageHandshake(wgMgr *fakeInPlaceWGManager) {
	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	wgMgr.handshakeUnix = time.Now().Add(-time.Minute).Unix()
}

func deviceCounts(wgMgr *fakeInPlaceWGManager) (stops, starts int, running bool) {
	wgMgr.mu.Lock()
	defer wgMgr.mu.Unlock()
	return wgMgr.stopCount, wgMgr.startCount, wgMgr.running
}

var errUnreachableNetwork = errors.New("dial tcp 203.0.113.9:8488: connectex: A socket operation was attempted to an unreachable network.")

// TestHealthCheck_InPlaceRebuildKeepsDeviceAcrossFailedCandidate: a candidate
// that handshakes but carries nothing must not cost the adapter.
func TestHealthCheck_InPlaceRebuildKeepsDeviceAcrossFailedCandidate(t *testing.T) {
	svc, probe, wgMgr, _ := inPlaceCascadeService(t)
	stops, starts := connectOverReality(t, svc, probe, wgMgr)

	probe.reset(map[string]bool{"shadowsocks": true})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED after the rebuild", st)
	}
	if got := svc.activeTransportKindSnapshot(); got != "shadowsocks" {
		t.Fatalf("active transport = %q, want shadowsocks", got)
	}
	if order := probe.recoveryOrder("reality"); len(order) < 2 || order[0] != "cloak" {
		t.Fatalf("cascade order = %v, want cloak rejected before shadowsocks", order)
	}
	gotStops, gotStarts, _ := deviceCounts(wgMgr)
	if gotStops != stops {
		t.Errorf("failed candidate tore the device down (%d -> %d stops); the adapter should stay up", stops, gotStops)
	}
	if gotStarts != starts+2 {
		t.Errorf("wireguard starts during the rebuild = %d, want 2 (one re-point per candidate)", gotStarts-starts)
	}
}

// TestHealthCheck_InPlaceRebuildKeepsDeviceThroughOfflineHold replays the
// wake-up drop: the hold must keep the adapter so recovery re-points it.
func TestHealthCheck_InPlaceRebuildKeepsDeviceThroughOfflineHold(t *testing.T) {
	svc, probe, wgMgr, shadowsocks := inPlaceCascadeService(t)
	stops, starts := connectOverReality(t, svc, probe, wgMgr)

	probe.reset(map[string]bool{})
	shadowsocks.mu.Lock()
	shadowsocks.startErr = errUnreachableNetwork
	shadowsocks.mu.Unlock()
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	status := svc.Status(context.Background())
	if status.State != state.StateError || !status.Offline {
		t.Fatalf("state = %q offline=%v, want ERROR in the offline hold", status.State, status.Offline)
	}
	gotStops, _, running := deviceCounts(wgMgr)
	if gotStops != stops || !running {
		t.Fatalf("offline hold dropped the device (stops %d -> %d, running=%v); the adapter should stay up", stops, gotStops, running)
	}

	shadowsocks.mu.Lock()
	shadowsocks.startErr = nil
	shadowsocks.mu.Unlock()
	probe.reset(map[string]bool{"shadowsocks": true})
	ageHandshake(wgMgr)
	svc.onNetworkChanged()
	svc.runHealthCheck(context.Background())

	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED once the link is back", st)
	}
	if got := svc.activeTransportKindSnapshot(); got != "shadowsocks" {
		t.Fatalf("active transport = %q, want shadowsocks", got)
	}
	gotStops, gotStarts, _ := deviceCounts(wgMgr)
	if gotStops != stops {
		t.Errorf("recovery recreated the device (%d -> %d stops); it should have been re-pointed in place", stops, gotStops)
	}
	if gotStarts <= starts {
		t.Errorf("recovery never re-pointed the device (starts %d -> %d)", starts, gotStarts)
	}
}

// TestHealthCheck_InPlaceRebuildExhaustedTearsDeviceDown: with every transport
// rejected the adapter comes down once at the end, never per candidate.
func TestHealthCheck_InPlaceRebuildExhaustedTearsDeviceDown(t *testing.T) {
	svc, probe, wgMgr, _ := inPlaceCascadeService(t)
	stops, _ := connectOverReality(t, svc, probe, wgMgr)

	probe.reset(map[string]bool{})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	if st := svc.Status(context.Background()).State; st != state.StateError {
		t.Fatalf("state = %q, want ERROR after exhaustion", st)
	}
	if !svc.transportsAreExhausted() {
		t.Error("transports not reported exhausted")
	}
	gotStops, _, running := deviceCounts(wgMgr)
	if running {
		t.Error("device still up after exhaustion; the next connect would adopt a dead tunnel")
	}
	if gotStops != stops+1 {
		t.Errorf("device stops during an exhausted rebuild = %d, want exactly 1 at the end", gotStops-stops)
	}
}

// TestSessionIsHealthy_RequiresALiveTransport: a kept device with no transport
// behind it is dead, whatever its last handshake says.
func TestSessionIsHealthy_RequiresALiveTransport(t *testing.T) {
	svc, probe, wgMgr, _ := inPlaceCascadeService(t)
	connectOverReality(t, svc, probe, wgMgr)
	profile := cascadeProfile()

	if !svc.sessionIsHealthy(context.Background(), profile) {
		t.Fatal("live session judged unhealthy")
	}
	svc.setActiveTransportKind("")
	if svc.sessionIsHealthy(context.Background(), profile) {
		t.Fatal("a device with no transport behind it was judged healthy")
	}
}

// TestConnect_DuringOfflineHoldDoesNotAdoptTheHeldDevice: the device kept for
// recovery carries a dead session; a connect must start clean, not adopt it.
func TestConnect_DuringOfflineHoldDoesNotAdoptTheHeldDevice(t *testing.T) {
	svc, probe, wgMgr, shadowsocks := inPlaceCascadeService(t)
	connectOverReality(t, svc, probe, wgMgr)
	probe.reset(map[string]bool{})
	shadowsocks.mu.Lock()
	shadowsocks.startErr = errUnreachableNetwork
	shadowsocks.mu.Unlock()
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)
	if !svc.offlineHoldActive() {
		t.Fatal("expected the offline hold before connecting again")
	}

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})

	if !errors.Is(err, ErrHostOffline) {
		t.Fatalf("Connect during the hold = %v, want ErrHostOffline from a fresh bring-up", err)
	}
	status := svc.Status(context.Background())
	if status.State != state.StateError || !status.Offline {
		t.Fatalf("state = %q offline=%v, want ERROR in the offline hold, not an adopted dead tunnel", status.State, status.Offline)
	}
}

// secondCascadeProfile is a different server with the same transport set, so a
// switch walks the same cascade against the same fakes.
func secondCascadeProfile() state.Profile {
	profile := cascadeProfile()
	profile.ID = "p2"
	profile.Name = "p2"
	profile.Cloak.RemoteHost = "b.example.com"
	profile.Reality.RemoteHost = "rb.example.com"
	profile.Shadowsocks.RemoteHost = "sb.example.com"
	return profile
}

func addProfile(t *testing.T, svc *Service, profile state.Profile) {
	t.Helper()
	cfg := svc.config.Get()
	cfg.Profiles = append(cfg.Profiles, profile)
	if err := svc.config.Set(cfg); err != nil {
		t.Fatalf("adding profile %s: %v", profile.ID, err)
	}
}

// TestSwitch_InPlaceKeepsDeviceAcrossFailedCandidate: a switch whose first
// candidate carries nothing must fall back without recreating the adapter.
func TestSwitch_InPlaceKeepsDeviceAcrossFailedCandidate(t *testing.T) {
	svc, probe, wgMgr, _ := inPlaceCascadeService(t)
	addProfile(t, svc, secondCascadeProfile())
	stops, starts := connectOverReality(t, svc, probe, wgMgr)

	probe.reset(map[string]bool{"shadowsocks": true})
	if err := svc.Switch(context.Background(), "p2", ConnectOptions{}); err != nil {
		t.Fatalf("switch failed: %v", err)
	}

	if got := svc.activeTransportKindSnapshot(); got != "shadowsocks" {
		t.Fatalf("active transport = %q, want shadowsocks", got)
	}
	gotStops, gotStarts, _ := deviceCounts(wgMgr)
	if gotStops != stops {
		t.Errorf("failed candidate tore the device down (%d -> %d stops); the switch should re-point it", stops, gotStops)
	}
	if gotStarts <= starts+1 {
		t.Errorf("wireguard starts during the switch = %d, want one re-point per candidate", gotStarts-starts)
	}
}

// TestSwitch_InPlaceExhaustedReleasesDevice: a switch with no working transport
// brings the adapter down once at the end, never per candidate.
func TestSwitch_InPlaceExhaustedReleasesDevice(t *testing.T) {
	svc, probe, wgMgr, _ := inPlaceCascadeService(t)
	addProfile(t, svc, secondCascadeProfile())
	stops, _ := connectOverReality(t, svc, probe, wgMgr)

	probe.reset(map[string]bool{})
	err := svc.Switch(context.Background(), "p2", ConnectOptions{})

	if !errors.Is(err, ErrTransportExhausted) {
		t.Fatalf("switch error = %v, want ErrTransportExhausted", err)
	}
	gotStops, _, running := deviceCounts(wgMgr)
	if running {
		t.Error("device still up after an exhausted switch; the next connect would adopt a dead tunnel")
	}
	if gotStops != stops+1 {
		t.Errorf("device stops during an exhausted switch = %d, want exactly 1 at the end", gotStops-stops)
	}
}
