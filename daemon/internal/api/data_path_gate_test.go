package api

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// cascadeProfile configures reality, cloak and shadowsocks with resolvers, so
// the data-path gate has something to probe on every candidate.
func cascadeProfile() state.Profile {
	return state.Profile{
		ID:          "p1",
		Name:        "p1",
		Cloak:       state.CloakProfile{RemoteHost: "example.com", RemotePort: 443, LocalPort: 51821},
		Reality:     &state.RealityProfile{RemoteHost: "r.example.com", RemotePort: 8443, UUID: "u", PublicKey: "k", ShortID: "ab12"},
		Shadowsocks: &state.ShadowsocksProfile{RemoteHost: "s.example.com", RemotePort: 8488, Password: "p"},
		WireGuard: state.WireGuardProfile{
			TunnelName: "pangea0",
			ConfigText: "[Interface]\nPrivateKey=x\nDNS=10.0.0.53\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n",
		},
	}
}

// gatedProbe records the order transport kinds were probed in — the order the
// cascade attempted them, since only one candidate runs at a time.
type gatedProbe struct {
	mu          sync.Mutex
	reality     *fakeRealityManager
	cloak       *fakeCloakManager
	shadowsocks *fakeShadowsocksManager
	passing     map[string]bool
	attempts    []string
}

func (g *gatedProbe) runningKind() string {
	switch {
	case g.reality.Status().Running:
		return "reality"
	case g.shadowsocks.Status().Running:
		return "shadowsocks"
	case g.cloak.Status().Running:
		return "cloak"
	default:
		return ""
	}
}

func (g *gatedProbe) probe(context.Context, string, string) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	kind := g.runningKind()
	g.attempts = append(g.attempts, kind)
	if g.passing[kind] {
		return nil
	}
	return errors.New("no reply over the tunnel")
}

func (g *gatedProbe) order() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]string(nil), g.attempts...)
}

// recoveryOrder is the order the cascade was walked in after the live transport
// died, with the health rounds that ran on the dying transport dropped.
func (g *gatedProbe) recoveryOrder(dying string) []string {
	order := g.order()
	for len(order) > 0 && order[0] == dying {
		order = order[1:]
	}
	return order
}

func (g *gatedProbe) reset(passing map[string]bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.passing = passing
	g.attempts = nil
}

func cascadeTestService(t *testing.T, passing map[string]bool) (*Service, *gatedProbe) {
	t.Helper()
	cloak := &fakeCloakManager{}
	reality := &fakeRealityManager{}
	shadowsocks := &fakeShadowsocksManager{}
	svc := newTestServiceFull(t, cloak, &fakeNaiveManager{}, reality,
		&fakeHysteria2Manager{}, shadowsocks, &fakeSnowflakeManager{},
		&fakeWGManager{}, &fakeKillSwitch{}, cascadeProfile())
	svc.recoveryDelays = []time.Duration{0}
	svc.networkKey = func() string { return "eth0:192.0.2.10" }
	probe := &gatedProbe{reality: reality, cloak: cloak, shadowsocks: shadowsocks, passing: passing}
	svc.probeResolver = probe.probe
	return svc, probe
}

// TestConnect_TransportThatCarriesNoTrafficIsRejected is the shipped bug: a
// transport DPI kills right after connect still completes a handshake.
func TestConnect_TransportThatCarriesNoTrafficIsRejected(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if got := svc.activeTransportKindSnapshot(); got != "shadowsocks" {
		t.Fatalf("active transport = %q, want shadowsocks", got)
	}
	if order := probe.order(); len(order) < 2 || order[0] != "reality" {
		t.Fatalf("cascade did not gate every candidate in order: %v", order)
	}
}

func TestConnect_NoTransportCarriesTrafficExhaustsTheCascade(t *testing.T) {
	svc, _ := cascadeTestService(t, map[string]bool{})

	err := svc.Connect(context.Background(), "p1", ConnectOptions{})
	if !errors.Is(err, ErrTransportExhausted) {
		t.Fatalf("Connect error = %v, want ErrTransportExhausted", err)
	}
	if !strings.Contains(err.Error(), "carry traffic") {
		t.Fatalf("error should name the dead data path: %v", err)
	}
}

// TestHealthCheck_DeadDataPathRestartsTheCascadeAtTheTop: when the connected
// transport stops carrying traffic, recovery walks the whole cascade again.
func TestHealthCheck_DeadDataPathRestartsTheCascadeAtTheTop(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	probe.reset(map[string]bool{"reality": true})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	if got := svc.activeTransportKindSnapshot(); got != "reality" {
		t.Fatalf("active transport after recovery = %q, want reality", got)
	}
	if order := probe.recoveryOrder("shadowsocks"); len(order) == 0 || order[0] != "reality" {
		t.Fatalf("recovery did not restart the cascade at the top: %v", order)
	}
}

func TestHealthCheck_DeadDataPathDemotesTheFailedTransport(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	probe.reset(map[string]bool{})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	order := probe.recoveryOrder("shadowsocks")
	if len(order) == 0 {
		t.Fatal("recovery attempted no transports")
	}
	if order[0] != "reality" {
		t.Fatalf("recovery started at %q, want reality: %v", order[0], order)
	}
	if last := order[len(order)-1]; last != "shadowsocks" {
		t.Fatalf("the transport that died was retried at %q, want it last: %v", last, order)
	}
}

// TestStatus_ReportsTransportsExhausted is what tells the app to stop waiting on
// this server and rotate to another one.
func TestStatus_ReportsTransportsExhausted(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	if svc.Status(context.Background()).TransportsExhausted {
		t.Fatal("a healthy session must not report exhausted transports")
	}

	probe.reset(map[string]bool{})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	if !svc.Status(context.Background()).TransportsExhausted {
		t.Fatal("a session that ran out of transports must say so")
	}
}

// TestConnect_ClearsTransportsExhausted: the app rotates with a plain Connect,
// so a flag left over from the old server must not trigger another rotation.
func TestConnect_ClearsTransportsExhausted(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	probe.reset(map[string]bool{})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)
	if !svc.Status(context.Background()).TransportsExhausted {
		t.Fatal("precondition: transports should be exhausted")
	}

	probe.reset(map[string]bool{"shadowsocks": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("reconnect failed: %v", err)
	}
	if st := svc.Status(context.Background()); st.State != state.StateConnected || st.TransportsExhausted {
		t.Fatalf("status = %s exhausted=%v, want CONNECTED and not exhausted after a new session", st.State, st.TransportsExhausted)
	}
}

func TestNextDNSProbeDelay_IsJitteredWithinBounds(t *testing.T) {
	seen := make(map[time.Duration]int)
	for range 200 {
		delay := nextDNSProbeDelay()
		if delay < dnsProbeIntervalMin || delay > dnsProbeIntervalMax {
			t.Fatalf("delay %s outside [%s, %s]", delay, dnsProbeIntervalMin, dnsProbeIntervalMax)
		}
		seen[delay]++
	}
	if len(seen) < 20 {
		t.Fatalf("probe cadence is too regular to hide in a flow: %d distinct delays", len(seen))
	}
}

func TestRootProbeQuery_VariesInSize(t *testing.T) {
	sizes := make(map[int]int)
	for range 100 {
		query, _, _, err := rootProbeQuery()
		if err != nil {
			t.Fatalf("rootProbeQuery: %v", err)
		}
		sizes[len(query)]++
	}
	if len(sizes) < 5 {
		t.Fatalf("probe packets are a constant size: %v", sizes)
	}
}

// TestRootProbeQuery_VariesWhatTheReplyWillLookLike covers the other half of
// the shape: the question and buffer size decide the reply's size.
func TestRootProbeQuery_VariesWhatTheReplyWillLookLike(t *testing.T) {
	qtypes := make(map[uint16]int)
	payloads := make(map[uint16]int)
	dnssec := make(map[bool]int)
	for range 200 {
		query, questionEnd, _, err := rootProbeQuery()
		if err != nil {
			t.Fatalf("rootProbeQuery: %v", err)
		}
		qtypes[binary.BigEndian.Uint16(query[13:15])]++
		payloads[binary.BigEndian.Uint16(query[questionEnd+3:questionEnd+5])]++
		dnssec[binary.BigEndian.Uint32(query[questionEnd+5:questionEnd+9])&0x8000 != 0]++
	}

	if len(qtypes) != len(rootProbeQTypes) {
		t.Errorf("asked %d of the %d root questions: %v", len(qtypes), len(rootProbeQTypes), qtypes)
	}
	if len(payloads) != len(rootProbePayloadSizes) {
		t.Errorf("advertised %d of the %d buffer sizes: %v", len(payloads), len(rootProbePayloadSizes), payloads)
	}
	// DO stays clear: its ~1 KB answers are what a constrained path drops.
	if dnssec[true] != 0 {
		t.Errorf("the DNSSEC OK bit was set on %d queries", dnssec[true])
	}
	if slices.Contains(rootProbeQTypes, 48) {
		t.Error("root DNSKEY is back in the probe question set")
	}
}

// The shipped macOS bug: scanning for the first up utun* finds the system's
// own utun0, so the gate probed an interface the tunnel never ran on.
func TestProveDataPath_ProbesTheLiveTunnelInterface(t *testing.T) {
	wgMgr := &fakeWGManager{interfaceName: "utun7"}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		&fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, &fakeKillSwitch{}, cascadeProfile())

	var mu sync.Mutex
	var probedIfaces []string
	svc.probeResolver = func(_ context.Context, iface, _ string) error {
		mu.Lock()
		defer mu.Unlock()
		probedIfaces = append(probedIfaces, iface)
		return nil
	}

	if err := svc.proveDataPath(context.Background(), cascadeProfile().WireGuard); err != nil {
		t.Fatalf("proveDataPath failed: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(probedIfaces) == 0 {
		t.Fatal("probe was never called")
	}
	for _, iface := range probedIfaces {
		if iface != "utun7" {
			t.Fatalf("probe bound to %q, want the live tunnel interface utun7", iface)
		}
	}
}

// TestHealthCheck_DeadDataPathWaitsForUsableNetwork is the idle-laptop case:
// dialling the cascade while the NIC is briefly down would wrongly blame the server.
func TestHealthCheck_DeadDataPathWaitsForUsableNetwork(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	var networkKeyMu sync.Mutex
	networkKey := ""
	svc.networkKey = func() string {
		networkKeyMu.Lock()
		defer networkKeyMu.Unlock()
		return networkKey
	}

	probe.reset(map[string]bool{})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	if order := probe.recoveryOrder("shadowsocks"); len(order) != 0 {
		t.Fatalf("cascade ran with no host network: %v", order)
	}
	if st := svc.Status(context.Background()); st.State != state.StateConnected || st.TransportsExhausted {
		t.Fatalf("status = %s exhausted=%v, want CONNECTED and not exhausted while waiting", st.State, st.TransportsExhausted)
	}

	networkKeyMu.Lock()
	networkKey = "eth0:192.0.2.10"
	networkKeyMu.Unlock()
	probe.reset(map[string]bool{"reality": true})
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	if got := svc.activeTransportKindSnapshot(); got != "reality" {
		t.Fatalf("active transport once the network is back = %q, want reality (rebuild must not wait out the cooldown)", got)
	}
}

// TestStatus_HostNetworkOutageIsNotExhaustion: a cascade that failed because the
// host had no route is not this server being blocked, so the app must not rotate.
func TestStatus_HostNetworkOutageIsNotExhaustion(t *testing.T) {
	svc, probe := cascadeTestService(t, map[string]bool{"shadowsocks": true})
	if err := svc.Connect(context.Background(), "p1", ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	probe.reset(map[string]bool{})
	svc.probeResolver = func(context.Context, string, string) error {
		return errors.New("dial udp 10.0.0.53:53: connect: A socket operation was attempted to an unreachable network.")
	}
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	st := svc.Status(context.Background())
	if st.State != state.StateError {
		t.Fatalf("state = %s, want ERROR after a failed rebuild", st.State)
	}
	if st.TransportsExhausted {
		t.Fatal("an outage on the host must not be reported as exhausted transports")
	}
	if !st.Offline {
		t.Fatal("an outage on the host must be reported as no internet")
	}
	if st.Reconnecting {
		t.Fatal("an outage is a hold, not a booked reconnect attempt")
	}
	if _, kept := svc.getCurrentProfile(); !kept {
		t.Fatal("the daemon must keep the session so recovery re-dials when a link returns")
	}
}

func TestHandshakeTimeoutFor_RebuildsGetTheLongerBudget(t *testing.T) {
	ctx := context.Background()
	if got := handshakeTimeoutFor(ctx, 0); got != defaultWireGuardHandshakeTimeout {
		t.Fatalf("default = %s", got)
	}
	if got := handshakeTimeoutFor(withHandshakeBudget(ctx, rebuildHandshakeTimeout), 0); got != rebuildHandshakeTimeout {
		t.Fatalf("rebuild budget = %s, want %s", got, rebuildHandshakeTimeout)
	}
	if got := handshakeTimeoutFor(withHandshakeBudget(ctx, rebuildHandshakeTimeout), time.Millisecond); got != time.Millisecond {
		t.Fatalf("configured timeout must win, got %s", got)
	}
}

func TestHostNetworkUnreachable(t *testing.T) {
	cases := map[string]bool{
		"all configured transports failed: reality: dial tcp: connectex: A socket operation was attempted to an unreachable network.": true,
		"all configured transports failed: cloak: no wireguard handshake within 10s; reality: dial: no route to host":                  true,
		"all configured transports failed: cloak: no wireguard handshake within 10s":                                                   false,
	}
	for msg, want := range cases {
		if got := hostNetworkUnreachable(errors.New(msg)); got != want {
			t.Errorf("%q: got %v want %v", msg, got, want)
		}
	}
	if hostNetworkUnreachable(nil) {
		t.Error("nil error must not be an outage")
	}
}

// TestProveDataPath_OversizedReplyProvesTheTunnelCarriesTraffic covers Windows
// WSAEMSGSIZE: the reply crossed the tunnel, only the userspace copy failed.
func TestProveDataPath_OversizedReplyProvesTheTunnelCarriesTraffic(t *testing.T) {
	wgMgr := &fakeWGManager{interfaceName: "utun7"}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		&fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, &fakeKillSwitch{}, cascadeProfile())

	svc.probeResolver = func(context.Context, string, string) error { return errDNSProbeOversizedReply }

	if err := svc.proveDataPath(context.Background(), cascadeProfile().WireGuard); err != nil {
		t.Fatalf("proveDataPath rejected a transport that carried an oversized reply: %v", err)
	}
}

// TestMaxProbeReplySize_CoversTheLargestAdvertisedPayload keeps the read buffer
// at or above the advertised EDNS0 ceiling, or Windows fails the whole read.
func TestMaxProbeReplySize_CoversTheLargestAdvertisedPayload(t *testing.T) {
	for _, size := range rootProbePayloadSizes {
		if int(size) > maxProbeReplySize {
			t.Fatalf("advertised EDNS0 payload size %d exceeds the %d byte read buffer", size, maxProbeReplySize)
		}
	}
}

// gateProbeCounter counts probe calls and fails every one, so a test measures
// how many attempts the gate spends before giving up.
func gateProbeCounter(svc *Service) *atomic.Int32 {
	var calls atomic.Int32
	svc.probeResolver = func(context.Context, string, string) error {
		calls.Add(1)
		return errors.New("i/o timeout")
	}
	return &calls
}

func gateTestService(t *testing.T) (*Service, *fakeWGManager) {
	t.Helper()
	wgMgr := &fakeWGManager{interfaceName: "utun7"}
	svc := newTestServiceFull(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeRealityManager{},
		&fakeHysteria2Manager{}, &fakeShadowsocksManager{}, &fakeSnowflakeManager{}, wgMgr, &fakeKillSwitch{}, cascadeProfile())
	return svc, wgMgr
}

// TestProveDataPath_BlockedTunnelStillFailsFast proves the slow-host budget
// isn't a blanket extension: a silent peer keeps the original attempt count.
func TestProveDataPath_BlockedTunnelStillFailsFast(t *testing.T) {
	svc, _ := gateTestService(t)
	calls := gateProbeCounter(svc)

	if err := svc.proveDataPath(context.Background(), cascadeProfile().WireGuard); err == nil {
		t.Fatal("proveDataPath accepted a transport that never carried a round trip")
	}
	if got := calls.Load(); got != dataPathGateAttempts {
		t.Errorf("probe attempts = %d, want %d — a flat rx counter must not buy extra tries", got, dataPathGateAttempts)
	}
}

// The slow-host case: the handshake lands before the address is out of DAD and
// the routes are published, so the gate must wait rather than spend attempts.
func TestProveDataPath_WaitsForALateAdapter(t *testing.T) {
	svc, wgMgr := gateTestService(t)
	wgMgr.notReadyPolls = 2

	var mu sync.Mutex
	var probes, probesBeforeReady int
	svc.probeResolver = func(context.Context, string, string) error {
		wgMgr.mu.Lock()
		ready := wgMgr.readyPolls > wgMgr.notReadyPolls
		wgMgr.mu.Unlock()
		mu.Lock()
		defer mu.Unlock()
		probes++
		if !ready {
			probesBeforeReady++
		}
		return nil
	}

	if err := svc.proveDataPath(context.Background(), cascadeProfile().WireGuard); err != nil {
		t.Fatalf("proveDataPath rejected a tunnel whose adapter arrived late: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if probesBeforeReady != 0 {
		t.Errorf("gate spent %d attempts on an adapter that was not ready yet", probesBeforeReady)
	}
	if probes == 0 {
		t.Error("gate passed without ever probing the ready adapter")
	}
}

// A peer's passive keepalives move the received-byte counter on a tunnel that
// carries nothing, so no extension may be bought with it.
func TestProveDataPath_KeepaliveInflatedCounterStillFailsFast(t *testing.T) {
	svc, wgMgr := gateTestService(t)

	var calls atomic.Int32
	svc.probeResolver = func(context.Context, string, string) error {
		calls.Add(1)
		wgMgr.addBytesIn(1500)
		return errors.New("i/o timeout")
	}

	if err := svc.proveDataPath(context.Background(), cascadeProfile().WireGuard); err == nil {
		t.Fatal("proveDataPath accepted a blocked tunnel whose peer only sent keepalives")
	}
	if got := calls.Load(); got != dataPathGateAttempts {
		t.Errorf("probe attempts = %d, want %d — keepalives must not buy extra tries", got, dataPathGateAttempts)
	}
	if wgMgr.readyPolls == 0 {
		t.Error("gate never checked adapter readiness")
	}
}

// TestProveDataPath_LateRouteIsRetriedNotTreatedAsAVerdict: a route of ours that
// is still being published must neither pass the gate nor consume an attempt.
func TestProveDataPath_LateRouteIsRetriedNotTreatedAsAVerdict(t *testing.T) {
	svc, _ := gateTestService(t)
	svc.dataPathBudget = 3 * dataPathGateRetryDelay

	var calls atomic.Int32
	svc.probeResolver = func(context.Context, string, string) error {
		calls.Add(1)
		return fmt.Errorf("%w: the tunnel's route is not published yet", errDNSProbeNotReady)
	}

	err := svc.proveDataPath(context.Background(), cascadeProfile().WireGuard)
	if err == nil {
		t.Fatal("proveDataPath passed a tunnel nothing ever left the host on")
	}
	if hostNetworkUnreachable(err) {
		t.Errorf("a late route of ours was reported as the host having no internet: %v", err)
	}
	if got := calls.Load(); int(got) <= dataPathGateAttempts {
		t.Errorf("probe calls = %d, want more than the %d base attempts — a not-ready round must not consume one", got, dataPathGateAttempts)
	}
}

// TestProbeResolverOverUDP_MissingInterfaceIsNotReady: a local setup failure is
// the adapter lagging, and passing the gate on it proves nothing crossed.
func TestProbeResolverOverUDP_MissingInterfaceIsNotReady(t *testing.T) {
	err := probeResolverOverUDP(context.Background(), "pangea-does-not-exist0", "10.0.0.53")
	if err == nil {
		t.Fatal("probe reported success against an interface that does not exist")
	}
	if runtime.GOOS == "linux" {
		return // SO_BINDTODEVICE fails at dial, not at bind.
	}
	if !errors.Is(err, errDNSProbeNotReady) {
		t.Fatalf("error = %v, want errDNSProbeNotReady", err)
	}
	if hostNetworkUnreachable(err) {
		t.Errorf("a missing adapter was reported as the host having no internet: %v", err)
	}
}

// TestProbeResolverWithDialer_RetransmitsInsideOneAttempt: one datagram dropped
// right after the handshake must cost a resend, not the whole attempt.
func TestProbeResolverWithDialer_RetransmitsInsideOneAttempt(t *testing.T) {
	var seen atomic.Int32
	server, stop := stubDNSServer(t, func(query []byte) []byte {
		if seen.Add(1) == 1 {
			return nil
		}
		reply := make([]byte, len(query))
		copy(reply, query)
		reply[2] = 0x80
		return reply
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), dnsProbeTimeout)
	defer cancel()
	if err := probeResolverWithDialer(ctx, &net.Dialer{}, server); err != nil {
		t.Fatalf("probe failed after one dropped query: %v", err)
	}
	if got := seen.Load(); got != 2 {
		t.Errorf("queries sent = %d, want 2 (the original and one retransmit)", got)
	}
}
