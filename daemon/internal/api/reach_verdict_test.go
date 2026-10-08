package api

import (
	"context"
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reach"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	testHubIP     = "203.0.113.7"
	testNetwork   = "eth0:192.0.2.10" // newTestServiceFull's pinned networkKey
	directRouteID = reachRouteDirect + ":" + testHubIP
	testNodeIP    = "198.51.100.20"
	testProxyNode = "198.51.100.9"
)

// fakeReach answers each route by id; a route it was not told about is silent.
type fakeReach struct {
	mu       sync.Mutex
	outcomes map[string]reach.Outcome
	probed   []string
	delay    time.Duration
	// duringProbe runs mid-probe, standing in for a user operation that lands
	// while the probe holds no lock.
	duringProbe func()
	// blocking routes hang until their probe is cancelled; cancelled lists them.
	blocking  map[string]bool
	cancelled []string
}

func newFakeReach() *fakeReach { return &fakeReach{outcomes: map[string]reach.Outcome{}} }

func (f *fakeReach) probe(ctx context.Context, route reachRoute) reach.Outcome {
	time.Sleep(f.delay)
	if f.duringProbe != nil {
		f.duringProbe()
	}
	f.mu.Lock()
	blocks := f.blocking[route.id()]
	f.mu.Unlock()
	if blocks {
		select {
		case <-ctx.Done():
			f.mu.Lock()
			f.cancelled = append(f.cancelled, route.id())
			f.mu.Unlock()
		case <-time.After(3 * time.Second):
		}
		return reach.Silent
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.probed = append(f.probed, route.id())
	return f.outcomes[route.id()]
}

func (f *fakeReach) set(id string, outcome reach.Outcome) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.outcomes[id] = outcome
}

func (f *fakeReach) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.probed)
}

func (f *fakeReach) probedIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.probed)
}

type fakeShadowsocksProxy struct{ remote string }

func (f *fakeShadowsocksProxy) Start(context.Context, state.ShadowsocksProfile) (int, error) {
	return 41001, nil
}
func (f *fakeShadowsocksProxy) Stop(context.Context) error    { return nil }
func (f *fakeShadowsocksProxy) Port() int                     { return 41001 }
func (f *fakeShadowsocksProxy) Credentials() (string, string) { return "u", "p" }
func (f *fakeShadowsocksProxy) HubRemote() string             { return f.remote }
func (f *fakeShadowsocksProxy) DialHub(context.Context, string, string, int) (net.Conn, error) {
	return nil, errors.New("fake shadowsocks proxy does not dial")
}

// reachProfile bypasses the hub IP and runs its transports on testNodeIP.
func reachProfile() state.Profile {
	profile := testProfile()
	profile.WireGuard.BypassHosts = []string{testHubIP}
	profile.TransportEndpointIPs = []string{testNodeIP}
	return profile
}

// vouchingProfile is a second stored profile whose node vouches for testProxyNode.
func vouchingProfile() state.Profile {
	profile := testProfile()
	profile.ID = "test-profile-2"
	profile.TransportEndpointIPs = []string{testProxyNode}
	return profile
}

func verdictTestService(t *testing.T, ks *fakeKillSwitch, profiles ...state.Profile) (*Service, *fakeReach) {
	t.Helper()
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profiles...)
	svc.physicalRoute = func() (string, string, error) { return "eth0", "192.0.2.1", nil }
	hub := newFakeReach()
	svc.reachProbe = hub.probe
	return svc, hub
}

func TestReachVerdict_Unknowns(t *testing.T) {
	t.Run("no probe", func(t *testing.T) {
		svc, _ := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
		svc.reachProbe = nil
		if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachUnknown {
			t.Fatalf("verdict = %v, want unknown", v)
		}
	})
	t.Run("no physical route", func(t *testing.T) {
		svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
		svc.physicalRoute = func() (string, string, error) { return "", "", platform.ErrNoDefaultRoute }
		if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachUnknown || hub.calls() != 0 {
			t.Fatalf("verdict = %v after %d probes, want unknown with none", v, hub.calls())
		}
	})
	t.Run("no hub address", func(t *testing.T) {
		svc, _ := verdictTestService(t, &fakeKillSwitch{}, testProfile())
		if v, _ := svc.reachVerdictFor(context.Background(), testProfile()); v != reachUnknown {
			t.Fatalf("verdict = %v, want unknown", v)
		}
	})
}

func TestReachVerdict_AnEchoIsOnlineAndProvesTheRoute(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
	hub.set(directRouteID, reach.Answered)
	v, summary := svc.reachVerdictFor(context.Background(), reachProfile())
	if v != reachOnline {
		t.Fatalf("verdict = %v (%s), want online", v, summary)
	}
	if !svc.reachBaseline.proven(testNetwork, directRouteID) {
		t.Fatal("an answered route was not recorded as proven")
	}
}

func TestReachVerdict_SilenceOnAnUnprovenRouteIsUnknown(t *testing.T) {
	svc, _ := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
	if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachUnknown {
		t.Fatalf("verdict = %v, want unknown: this network never proved the hub reachable", v)
	}
}

func TestReachVerdict_SilenceOnAProvenRouteIsOffline(t *testing.T) {
	svc, _ := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
	svc.reachBaseline.record(testNetwork, directRouteID)
	v, summary := svc.reachVerdictFor(context.Background(), reachProfile())
	if v != reachOffline {
		t.Fatalf("verdict = %v, want offline", v)
	}
	if summary != directRouteID+"=silent" {
		t.Fatalf("summary = %q", summary)
	}
}

func TestReachVerdict_ARefusalAnywhereIsUnknown(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile(), vouchingProfile())
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
	svc.reachBaseline.record(testNetwork, directRouteID)
	hub.set(reachRouteReality+":"+testProxyNode, reach.Refused)
	if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachUnknown {
		t.Fatalf("verdict = %v, want unknown: something on the network answered", v)
	}
}

func TestReachVerdict_FateSharedRoutes(t *testing.T) {
	sharedID := reachRouteReality + ":" + testNodeIP
	t.Run("silence proves nothing", func(t *testing.T) {
		svc, _ := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
		svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testNodeIP})
		svc.reachBaseline.record(testNetwork, sharedID)
		if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachUnknown {
			t.Fatalf("verdict = %v, want unknown: that route runs through the dead session's own node", v)
		}
	})
	t.Run("an answer still counts", func(t *testing.T) {
		svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
		svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testNodeIP})
		hub.set(sharedID, reach.Answered)
		if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachOnline {
			t.Fatalf("verdict = %v, want online", v)
		}
	})
}

func TestReachVerdict_ProxyRoutesAnswerForABlockedDirectPath(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile(), vouchingProfile())
	svc.SetShadowsocksProxy(&fakeShadowsocksProxy{remote: testProxyNode})
	hub.set(reachRouteShadowsocks+":"+testProxyNode, reach.Answered)
	if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachOnline {
		t.Fatalf("verdict = %v, want online via the shadowsocks hub route", v)
	}
}

func TestReachVerdict_StoppedProxyIsNotARoute(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
	svc.SetRealityProxy(&fakeRealityProxy{})
	svc.reachVerdictFor(context.Background(), reachProfile())
	if got := hub.probedIDs(); !slices.Equal(got, []string{directRouteID}) {
		t.Fatalf("probed %v, want only the direct route", got)
	}
}

func TestReachVerdict_ProxyRoutesNeedAnIPLiteral(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: "hub-node.example.com"})
	svc.reachVerdictFor(context.Background(), reachProfile())
	if got := hub.probedIDs(); len(got) != 0 {
		t.Fatalf("probed %v; a hostname would need DNS, which runs through the dead tunnel", got)
	}
}

// The user keeps the hub off the physical link with HubInTunnel, or reaches it
// only through a hub proxy; the probe must not contact the hub IP behind their back.
func TestReachVerdict_DirectRouteRespectsHubPrivacy(t *testing.T) {
	t.Run("hub in tunnel", func(t *testing.T) {
		svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
		profile := reachProfile()
		profile.WireGuard.HubInTunnel = true
		svc.reachVerdictFor(context.Background(), profile)
		if got := hub.probedIDs(); len(got) != 0 {
			t.Fatalf("probed %v with HubInTunnel set", got)
		}
	})
	t.Run("hub reached through a proxy", func(t *testing.T) {
		svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile(), vouchingProfile())
		svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
		svc.reachVerdictFor(context.Background(), reachProfile())
		if got := hub.probedIDs(); !slices.Equal(got, []string{reachRouteReality + ":" + testProxyNode}) {
			t.Fatalf("probed %v, want only the proxy route the app itself uses", got)
		}
	})
}

func TestReachVerdict_LockOnlyLetsVouchedProxyNodesOut(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc, hub := verdictTestService(t, ks, reachProfile())
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: "198.51.100.77"})
	svc.reachVerdictFor(context.Background(), reachProfile())
	if slices.Contains(hub.probedIDs(), reachRouteReality+":198.51.100.77") {
		t.Fatal("probed an unvouched node the kill switch never lets out")
	}

	svc2, hub2 := verdictTestService(t, ks, reachProfile(), vouchingProfile())
	svc2.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
	svc2.reachVerdictFor(context.Background(), reachProfile())
	if !slices.Contains(hub2.probedIDs(), reachRouteReality+":"+testProxyNode) {
		t.Fatal("skipped a vouched node the kill switch permits")
	}
}

func TestReachVerdict_ProbesRunConcurrently(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile(), vouchingProfile())
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
	svc.SetShadowsocksProxy(&fakeShadowsocksProxy{remote: "198.51.100.10"})
	hub.delay = 150 * time.Millisecond
	start := time.Now()
	svc.reachVerdictFor(context.Background(), reachProfile())
	if calls := hub.calls(); calls != 2 {
		t.Fatalf("probed %d routes, want 2", calls)
	}
	if elapsed := time.Since(start); elapsed > 280*time.Millisecond {
		t.Fatalf("two routes took %s; probes must run side by side", elapsed)
	}
}

func TestConnect_PermitsAVouchedRunningHubProxyNode(t *testing.T) {
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile(), vouchingProfile())
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
	if err := svc.Connect(context.Background(), "test-profile-1", ConnectOptions{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if !slices.Contains(ks.enableEndpoints, testProxyNode) {
		t.Fatalf("permits %v lack the running hub proxy's node; the reachability probe could not leave", ks.enableEndpoints)
	}
}

func TestConnect_NeverPermitsAnUnvouchedHubProxyNode(t *testing.T) {
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	svc.SetShadowsocksProxy(&fakeShadowsocksProxy{remote: "198.51.100.77"})
	if err := svc.Connect(context.Background(), "test-profile-1", ConnectOptions{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if slices.Contains(ks.enableEndpoints, "198.51.100.77") {
		t.Fatal("permitted a node no stored profile vouches for")
	}
}

// A plain-WireGuard session names its node only in DirectEndpoint; a hub route
// through that node shares the dead tunnel's fate just the same.
func TestReachVerdict_DirectEndpointNodeSharesFate(t *testing.T) {
	profile := testProfile()
	profile.WireGuard.DirectEndpoint = "198.51.100.30:51820"
	svc, _ := verdictTestService(t, &fakeKillSwitch{}, profile)
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: "198.51.100.30"})
	svc.reachBaseline.record(testNetwork, reachRouteReality+":198.51.100.30")
	if v, _ := svc.reachVerdictFor(context.Background(), profile); v != reachUnknown {
		t.Fatalf("verdict = %v, want unknown: the only proven route runs through the session's own node", v)
	}
}

// One echo already decides "online", so a route still hanging (say, through the
// dead node) must not hold the health loop for its whole timeout.
func TestReachVerdict_FirstAnswerEndsTheRound(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile(), vouchingProfile())
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
	svc.SetShadowsocksProxy(&fakeShadowsocksProxy{remote: "198.51.100.10"})
	hub.set(reachRouteReality+":"+testProxyNode, reach.Answered)
	hub.blocking = map[string]bool{reachRouteShadowsocks + ":198.51.100.10": true}
	start := time.Now()
	v, _ := svc.reachVerdictFor(context.Background(), reachProfile())
	if v != reachOnline {
		t.Fatalf("verdict = %v, want online", v)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("round took %s after the first answer", elapsed)
	}
	deadline := time.Now().Add(time.Second)
	for {
		hub.mu.Lock()
		cancelled := slices.Contains(hub.cancelled, reachRouteShadowsocks+":198.51.100.10")
		hub.mu.Unlock()
		if cancelled {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the still-running probe was never cancelled")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Answers gathered while the host moved networks describe neither network, so
// nothing may be recorded and the verdict must not lean on them.
func TestReachVerdict_RoamMidRoundRecordsNothing(t *testing.T) {
	svc, hub := verdictTestService(t, &fakeKillSwitch{}, reachProfile())
	hub.set(directRouteID, reach.Answered)
	hub.duringProbe = func() { svc.networkKey = func() string { return "wlan0:198.51.100.40" } }
	if v, _ := svc.reachVerdictFor(context.Background(), reachProfile()); v != reachUnknown {
		t.Fatalf("verdict = %v, want unknown after a roam mid-round", v)
	}
	if svc.reachBaseline.proven(testNetwork, directRouteID) || svc.reachBaseline.proven("wlan0:198.51.100.40", directRouteID) {
		t.Fatal("an answer from a round that straddled two networks was recorded")
	}
}
