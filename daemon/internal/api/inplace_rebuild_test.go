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

// deadSessionKeeps are the attempts that fail offline and keep their device up
// for a session that is not.
var deadSessionKeeps = []struct {
	name    string
	profile string
	keep    func(t *testing.T, svc *Service)
}{
	{"offline rebuild", "p1", func(t *testing.T, svc *Service) {
		runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)
	}},
	{"offline switch", "p2", func(t *testing.T, svc *Service) {
		if err := svc.Switch(context.Background(), "p2", ConnectOptions{}); !errors.Is(err, ErrHostOffline) {
			t.Fatalf("switch error = %v, want ErrHostOffline", err)
		}
	}},
}

// keepDeviceForDeadSession connects, lets keep fail offline with the device
// kept, then brings the network back without telling the service.
func keepDeviceForDeadSession(t *testing.T, keep func(t *testing.T, svc *Service)) (*Service, *fakeInPlaceWGManager) {
	t.Helper()
	svc, probe, wgMgr, shadowsocks := inPlaceCascadeService(t)
	addProfile(t, svc, secondCascadeProfile())
	connectOverReality(t, svc, probe, wgMgr)
	probe.reset(map[string]bool{})
	shadowsocks.mu.Lock()
	shadowsocks.startErr = errUnreachableNetwork
	shadowsocks.mu.Unlock()
	keep(t, svc)
	if _, _, running := deviceCounts(wgMgr); !running {
		t.Fatal("expected the device kept through the offline hold")
	}

	shadowsocks.mu.Lock()
	shadowsocks.startErr = nil
	shadowsocks.mu.Unlock()
	probe.reset(map[string]bool{"shadowsocks": true})
	ageHandshake(wgMgr)
	return svc, wgMgr
}

// TestConnect_NeverAdoptsADeviceKeptForADeadSession: a network change zeroes the
// hold timer, but the device it kept still carries no session.
func TestConnect_NeverAdoptsADeviceKeptForADeadSession(t *testing.T) {
	for _, tc := range deadSessionKeeps {
		t.Run(tc.name, func(t *testing.T) {
			svc, wgMgr := keepDeviceForDeadSession(t, tc.keep)
			svc.onNetworkChanged()

			stops, starts, _ := deviceCounts(wgMgr)
			if err := svc.Connect(context.Background(), tc.profile, ConnectOptions{}); err != nil {
				t.Fatalf("Connect: %v", err)
			}
			assertCleanBringUp(t, svc, wgMgr, stops, starts)
			if got := svc.activeTransportKindSnapshot(); got != "shadowsocks" {
				t.Errorf("active transport = %q, want shadowsocks from a fresh cascade", got)
			}
		})
	}
}

// TestConnect_AdoptsAKeptDeviceOnceItsSessionIsBack: the dead-session mark must
// not outlive the session, or a Connect over a transient ERROR tears down a live tunnel.
func TestConnect_AdoptsAKeptDeviceOnceItsSessionIsBack(t *testing.T) {
	recoveries := []struct {
		name      string
		bringBack func(t *testing.T, svc *Service, profile string)
	}{
		{"rebuild re-points it", func(t *testing.T, svc *Service, _ string) {
			svc.onNetworkChanged()
			svc.runHealthCheck(context.Background())
		}},
		{"disconnect then connect", func(t *testing.T, svc *Service, profile string) {
			if err := svc.Disconnect(context.Background(), false); err != nil {
				t.Fatalf("Disconnect: %v", err)
			}
			if err := svc.Connect(context.Background(), profile, ConnectOptions{}); err != nil {
				t.Fatalf("Connect: %v", err)
			}
		}},
	}
	ctx := context.Background()
	for _, keep := range deadSessionKeeps {
		for _, rec := range recoveries {
			t.Run(keep.name+"/"+rec.name, func(t *testing.T) {
				svc, wgMgr := keepDeviceForDeadSession(t, keep.keep)
				rec.bringBack(t, svc, keep.profile)
				if status := svc.Status(ctx); status.State != state.StateConnected {
					t.Fatalf("state = %s (%s), want the session back", status.State, status.Detail)
				}
				other := "p1"
				if keep.profile == other {
					other = "p2"
				}
				if err := svc.Connect(ctx, other, ConnectOptions{}); err == nil {
					t.Fatalf("Connect(%s) over the live %s session succeeded, want it refused", other, keep.profile)
				}
				if status := svc.Status(ctx); status.State != state.StateError {
					t.Fatalf("state = %s (%s), want the refused Connect's ERROR", status.State, status.Detail)
				}

				stops, _, _ := deviceCounts(wgMgr)
				if err := svc.Connect(ctx, keep.profile, ConnectOptions{}); err != nil {
					t.Fatalf("Connect: %v", err)
				}
				if !logMentions(svc, "adopting existing wireguard tunnel") {
					t.Error("Connect did not adopt the live tunnel")
				}
				if gotStops, _, running := deviceCounts(wgMgr); gotStops != stops || !running {
					t.Errorf("stops %d -> %d, running=%v; want the live device left up", stops, gotStops, running)
				}
				if status := svc.Status(ctx); status.State != state.StateConnected {
					t.Errorf("state = %s (%s), want CONNECTED on the adopted tunnel", status.State, status.Detail)
				}
			})
		}
	}
}

// TestConnect_ReleasesAKeptDeviceADisconnectCouldNotStop: a teardown that never
// saw the device go must not forget it was kept for a dead session.
func TestConnect_ReleasesAKeptDeviceADisconnectCouldNotStop(t *testing.T) {
	svc, wgMgr := keepDeviceForDeadSession(t, deadSessionKeeps[0].keep)
	wgMgr.mu.Lock()
	wgMgr.stopErr = errors.New("device busy")
	wgMgr.statusErr = errors.New("status unavailable")
	wgMgr.mu.Unlock()
	_ = svc.Disconnect(context.Background(), false)
	wgMgr.mu.Lock()
	wgMgr.stopErr, wgMgr.statusErr = nil, nil
	wgMgr.mu.Unlock()
	if _, _, running := deviceCounts(wgMgr); !running {
		t.Fatal("expected the failed stop to leave the device up")
	}

	stops, starts, _ := deviceCounts(wgMgr)
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	assertCleanBringUp(t, svc, wgMgr, stops, starts)
}

// TestAttach_StillAdoptsAGenuinelyRunningTunnel: with nothing kept by recovery,
// a tunnel found running at startup is the live session and is adopted.
func TestAttach_StillAdoptsAGenuinelyRunningTunnel(t *testing.T) {
	a, _ := switchProfilePair()
	wgMgr := &fakeInPlaceWGManager{}
	wgMgr.running = true
	svc := newInPlaceTestService(t, wgMgr, &fakeKillSwitch{}, a)
	cloak := svc.cloak.(*fakeCloakManager)
	cloak.running = true

	// reconcileStartup's call, without its host-wide stale-adapter sweep.
	adopted, err := svc.attachToRunningSession(context.Background(), a, "")
	if err != nil || !adopted {
		t.Fatalf("attach = (%v, %v), want the running tunnel adopted", adopted, err)
	}
	if stops, _, running := deviceCounts(wgMgr); stops != 0 || !running {
		t.Errorf("stops=%d running=%v, want the adopted device left up", stops, running)
	}
	if status := svc.Status(context.Background()); status.State != state.StateConnected {
		t.Errorf("state = %s (%s), want CONNECTED on the adopted tunnel", status.State, status.Detail)
	}
}

// assertCleanBringUp checks a Connect released the kept device and built its
// own session instead of adopting the dead one.
func assertCleanBringUp(t *testing.T, svc *Service, wgMgr *fakeInPlaceWGManager, stops, starts int) {
	t.Helper()
	if logMentions(svc, "adopting existing wireguard tunnel") {
		t.Fatal("Connect adopted a device kept for a dead session")
	}
	gotStops, gotStarts, running := deviceCounts(wgMgr)
	if gotStops <= stops || gotStarts <= starts || !running {
		t.Errorf("stops %d -> %d, starts %d -> %d, running=%v; want the kept device released and a fresh one started",
			stops, gotStops, starts, gotStarts, running)
	}
	if status := svc.Status(context.Background()); status.State != state.StateConnected {
		t.Errorf("state = %s (%s), want CONNECTED", status.State, status.Detail)
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
