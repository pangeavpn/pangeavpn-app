package api

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// deadDataPathProfile is silentTunnelProfile with resolvers, so the health
// check has something to probe over the tunnel.
func deadDataPathProfile() state.Profile {
	profile := silentTunnelProfile()
	profile.WireGuard.ConfigText = "[Interface]\nPrivateKey=x\nDNS=10.0.0.53, 10.0.0.54\n[Peer]\nEndpoint=127.0.0.1:51821\nPublicKey=y\nAllowedIPs=0.0.0.0/0\n"
	return profile
}

// fakeProbe counts calls and answers with whatever err is set to.
type fakeProbe struct {
	mu      sync.Mutex
	err     error
	calls   int
	servers []string
}

func (p *fakeProbe) probe(_ context.Context, _, server string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.servers = append(p.servers, server)
	return p.err
}

func (p *fakeProbe) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.err = err
}

func (p *fakeProbe) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// forget drops what the bring-up gate did, so a test counts only the rounds the
// health loop ran.
func (p *fakeProbe) forget() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = 0
	p.servers = nil
}

// dataPathTestService is a connected naive session whose resolvers are answered
// by a stub, with no backoff to wait out.
func dataPathTestService(t *testing.T) (*Service, *fakeProbe, *fakeNaiveManager, *fakeWGManager, *fakeKillSwitch) {
	t.Helper()
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, naive, wgMgr, ks, deadDataPathProfile())
	svc.recoveryDelays = []time.Duration{0}
	svc.networkKey = func() string { return "eth0:192.0.2.10" }

	probe := &fakeProbe{}
	svc.probeResolver = probe.probe

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	probe.forget()
	return svc, probe, naive, wgMgr, ks
}

// forceDNSProbeDue brings the next probe round forward, so a test does not have
// to wait out the probe schedule between health checks.
func forceDNSProbeDue(svc *Service) {
	svc.recoveryMu.Lock()
	defer svc.recoveryMu.Unlock()
	svc.dnsProbeNextAt = time.Time{}
}

// runProbedHealthChecks runs n health checks with a probe round due on each.
func runProbedHealthChecks(svc *Service, n int) {
	for range n {
		forceDNSProbeDue(svc)
		svc.runHealthCheck(context.Background())
	}
}

// TestHealthCheck_DeadDataPathRebuildsSession is the case the handshake check
// cannot see: WireGuard keeps rekeying against the node every two minutes while
// the tunnel carries nothing, which is what a browser reports as a DNS probe
// error on a session the app still calls connected.
func TestHealthCheck_DeadDataPathRebuildsSession(t *testing.T) {
	svc, probe, naive, wgMgr, ks := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	// Fails the health rounds, then answers again: the rebuild's own bring-up
	// has to prove a data path too, so a probe that never recovers can only
	// produce a failed rebuild.
	svc.probeResolver = func(ctx context.Context, _, server string) error {
		wgMgr.mu.Lock()
		rebuilt := wgMgr.startCount > startsAfterConnect
		wgMgr.mu.Unlock()
		if !rebuilt {
			return errors.New("i/o timeout")
		}
		return probe.probe(ctx, "", server)
	}
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	status := svc.Status(context.Background())
	if status.State != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED after the dead data path was rebuilt", status.State)
	}
	naive.mu.Lock()
	naiveStopped := naive.stopCalled
	naive.mu.Unlock()
	if !naiveStopped {
		t.Error("expected the transport to be restarted; only a fresh session re-dials the relay that stopped forwarding")
	}
	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 1 {
		t.Errorf("wireguard restarts = %d, want 1", restarts)
	}
	if !ks.Active() {
		t.Error("expected the kill switch to stay armed across the rebuild")
	}
}

// TestHealthCheck_DeadDataPathToleratesLosingARound proves one lost round trip
// is not a dead tunnel: UDP drops datagrams and rebuilding on the first miss
// would tear down working sessions.
func TestHealthCheck_DeadDataPathToleratesLosingARound(t *testing.T) {
	svc, probe, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	probe.setErr(errors.New("i/o timeout"))
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild-1)

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d after %d failed rounds, want 0 before the %d-round threshold",
			restarts, dnsProbeFailuresBeforeRebuild-1, dnsProbeFailuresBeforeRebuild)
	}
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Errorf("state = %q, want CONNECTED", st)
	}
}

// TestHealthCheck_DataPathProbeRecoveryResetsTheCount proves the failure count
// is consecutive: rounds that fail either side of a success never add up to a
// rebuild.
func TestHealthCheck_DataPathProbeRecoveryResetsTheCount(t *testing.T) {
	svc, probe, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	for range dnsProbeFailuresBeforeRebuild * 2 {
		probe.setErr(errors.New("i/o timeout"))
		runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild-1)
		probe.setErr(nil)
		runProbedHealthChecks(svc, 1)
	}

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d, want 0 — a probe that answers clears the count", restarts)
	}
}

// TestHealthCheck_DataPathProbeSecondResolverRescuesTheRound proves a round is
// only failed once every resolver has been tried, so one dead resolver does not
// look like a dead tunnel.
func TestHealthCheck_DataPathProbeSecondResolverRescuesTheRound(t *testing.T) {
	svc, probe, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	svc.probeResolver = func(ctx context.Context, _, server string) error {
		if server == "10.0.0.53" {
			return errors.New("i/o timeout")
		}
		return probe.probe(ctx, "", server)
	}

	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild+1)

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d, want 0 — the second resolver answered every round", restarts)
	}
	if probe.callCount() == 0 {
		t.Error("expected the probe to fall through to the second resolver")
	}
}

// TestHealthCheck_DataPathProbeRebuildIsRateLimited proves a node that answers
// handshakes but never carries traffic is not rebuilt on a loop: the cooldown
// holds off the second rebuild, and each one drops the tunnel for a moment.
func TestHealthCheck_DataPathProbeRebuildIsRateLimited(t *testing.T) {
	svc, probe, _, wgMgr, _ := dataPathTestService(t)
	// The rebuild fails (nothing carries traffic), so without a backoff to sit
	// in, recovery would retry every tick and hide the cooldown under test.
	svc.recoveryDelays = []time.Duration{time.Hour}

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	probe.setErr(errors.New("i/o timeout"))
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild*4)

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 1 {
		t.Errorf("wireguard restarts = %d across %d failed rounds, want 1 inside the cooldown",
			restarts, dnsProbeFailuresBeforeRebuild*4)
	}
}

// TestHealthCheck_DataPathProbeRunsOnItsOwnSchedule proves the 3s health tick
// does not fire a probe every pass — the round trip costs real time and would
// otherwise be a query every three seconds for as long as the VPN is up.
func TestHealthCheck_DataPathProbeRunsOnItsOwnSchedule(t *testing.T) {
	svc, probe, _, _, _ := dataPathTestService(t)

	forceDNSProbeDue(svc)
	for range 5 {
		svc.runHealthCheck(context.Background())
	}

	if got := probe.callCount(); got != 1 {
		t.Errorf("probe calls = %d across 5 health checks, want 1", got)
	}
}

// TestHealthCheck_NoResolversSkipsTheProbe proves a profile that configures no
// DNS is left alone rather than failing a check it cannot run.
func TestHealthCheck_NoResolversSkipsTheProbe(t *testing.T) {
	naive := &fakeNaiveManager{}
	wgMgr := &fakeWGManager{}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, naive, wgMgr, ks, silentTunnelProfile())
	svc.recoveryDelays = []time.Duration{0}
	probe := &fakeProbe{err: errors.New("i/o timeout")}
	svc.probeResolver = probe.probe

	if err := svc.Connect(context.Background(), "p1", ConnectOptions{PreferredTransport: "naive"}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild*2)

	if got := probe.callCount(); got != 0 {
		t.Errorf("probe calls = %d, want 0 for a profile with no resolvers", got)
	}
	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Errorf("state = %q, want CONNECTED", st)
	}
}

// TestHoldHealthChecks_ClearsProbeFailures proves a resume does not carry the
// previous network's failed rounds into the new one — the host was asleep, the
// rounds that failed say nothing about the link it woke up on.
func TestHoldHealthChecks_ClearsProbeFailures(t *testing.T) {
	svc, probe, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	probe.setErr(errors.New("i/o timeout"))
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild-1)
	svc.holdHealthChecks(0)
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild-1)

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d, want 0 — the resume cleared the count", restarts)
	}
}

// TestHealthCheck_ConnRefusedProvesTheTunnelIsLive proves an ICMP
// port-unreachable arriving as ECONNRESET/ECONNREFUSED on the connected
// socket is not booked as a failure: the round trip happened, which is the
// evidence the probe exists to gather.
func TestHealthCheck_ConnRefusedProvesTheTunnelIsLive(t *testing.T) {
	svc, _, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	svc.probeResolver = func(context.Context, string, string) error { return errDNSProbeConnRefused }
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild*2)

	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d, want 0 — ECONNRESET proves the tunnel carried the round trip", restarts)
	}
}

// TestHealthCheck_InconclusiveRoundIsNotBookedEitherWay proves a round that
// could not complete — its context cancelled by a Switch/Disconnect in
// flight — is neither a success nor a failure.
func TestHealthCheck_InconclusiveRoundIsNotBookedEitherWay(t *testing.T) {
	svc, _, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	svc.probeResolver = func(context.Context, string, string) error {
		return fmt.Errorf("%w: context canceled", errDNSProbeInconclusive)
	}
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild*3)

	svc.recoveryMu.Lock()
	failures := svc.dnsProbeFailures
	svc.recoveryMu.Unlock()
	if failures != 0 {
		t.Errorf("dnsProbeFailures = %d, want 0 — an inconclusive round must not be booked as a failure", failures)
	}
	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d, want 0", restarts)
	}
}

// TestHealthCheck_SessionChangedMidRoundIsNotBooked proves a failure is not
// recorded against a session that changed while the round was in flight — the
// resolver list a slow round just tried may belong to a profile that is no
// longer the live one.
func TestHealthCheck_SessionChangedMidRoundIsNotBooked(t *testing.T) {
	svc, _, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	startsAfterConnect := wgMgr.startCount
	wgMgr.mu.Unlock()

	swaps := 0
	svc.probeResolver = func(context.Context, string, string) error {
		swaps++
		other := deadDataPathProfile()
		other.ID = fmt.Sprintf("some-other-profile-%d", swaps)
		svc.profileMu.Lock()
		svc.currentProfile = &other
		svc.profileMu.Unlock()
		return errors.New("i/o timeout")
	}
	runProbedHealthChecks(svc, dnsProbeFailuresBeforeRebuild)

	svc.recoveryMu.Lock()
	failures := svc.dnsProbeFailures
	svc.recoveryMu.Unlock()
	if failures != 0 {
		t.Errorf("dnsProbeFailures = %d, want 0 — the round belonged to a profile that is no longer live", failures)
	}
	wgMgr.mu.Lock()
	restarts := wgMgr.startCount - startsAfterConnect
	wgMgr.mu.Unlock()
	if restarts != 0 {
		t.Errorf("wireguard restarts = %d, want 0", restarts)
	}
}

// TestRecordDNSProbeFailure_CountNeverExceedsTheThreshold proves the counter
// resets as soon as it reaches the threshold even inside the rebuild cooldown,
// so the log never reports an attempt past dnsProbeFailuresBeforeRebuild and
// the round after the cooldown expires gets a fresh three-round debounce.
func TestRecordDNSProbeFailure_CountNeverExceedsTheThreshold(t *testing.T) {
	svc, _, _, _, _ := dataPathTestService(t)

	for i := 0; i < dnsProbeFailuresBeforeRebuild*3; i++ {
		failures, _ := svc.recordDNSProbeFailure()
		if failures > dnsProbeFailuresBeforeRebuild {
			t.Fatalf("recordDNSProbeFailure returned %d, want at most %d", failures, dnsProbeFailuresBeforeRebuild)
		}
	}
}

// TestHealthCheck_DNSGuardChecksEveryTick proves the host's interface DNS is
// verified continuously rather than only at bring-up. On Windows the setting
// belongs to whoever wrote last, so nothing keeps it ours for the life of a
// session.
func TestHealthCheck_DNSGuardChecksEveryTick(t *testing.T) {
	svc, _, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	callsAfterConnect := wgMgr.dnsGuardCalls
	wgMgr.mu.Unlock()

	for range 4 {
		svc.runHealthCheck(context.Background())
	}

	wgMgr.mu.Lock()
	calls := wgMgr.dnsGuardCalls - callsAfterConnect
	wgMgr.mu.Unlock()
	if calls != 4 {
		t.Errorf("DNS guard calls = %d across 4 health checks, want 4", calls)
	}
}

// TestHealthCheck_DNSGuardCorrectionIsLoggedAndRateLimited proves a correction
// leaves a trail and then backs off. The log line is the only evidence that
// something else on the machine is taking the tunnel's DNS, and backing off
// keeps two writers from trading corrections every three seconds.
func TestHealthCheck_DNSGuardCorrectionIsLoggedAndRateLimited(t *testing.T) {
	svc, _, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	wgMgr.dnsGuardCorrected = true
	callsAfterConnect := wgMgr.dnsGuardCalls
	wgMgr.mu.Unlock()

	for range 5 {
		svc.runHealthCheck(context.Background())
	}

	wgMgr.mu.Lock()
	calls := wgMgr.dnsGuardCalls - callsAfterConnect
	wgMgr.mu.Unlock()
	if calls != 1 {
		t.Errorf("DNS guard calls = %d after a correction, want 1 — the rest fall inside the cooldown", calls)
	}

	var corrections int
	for _, entry := range svc.Logs(0) {
		if strings.Contains(entry.Msg, "stopped pointing at the tunnel's resolvers") {
			corrections++
		}
	}
	if corrections != 1 {
		t.Errorf("logged corrections = %d, want 1", corrections)
	}
}

// TestHealthCheck_DNSGuardErrorDoesNotDropTheSession proves a guard that cannot
// read the interface is reported and moved past: failing to verify DNS is not a
// reason to tear down a working tunnel.
func TestHealthCheck_DNSGuardErrorDoesNotDropTheSession(t *testing.T) {
	svc, _, _, wgMgr, _ := dataPathTestService(t)

	wgMgr.mu.Lock()
	wgMgr.dnsGuardErr = errors.New("read interface DNS: access denied")
	wgMgr.mu.Unlock()

	svc.runHealthCheck(context.Background())

	if st := svc.Status(context.Background()).State; st != state.StateConnected {
		t.Fatalf("state = %q, want CONNECTED — a guard error is not a dead tunnel", st)
	}
	var warned bool
	for _, entry := range svc.Logs(0) {
		if strings.Contains(entry.Msg, "could not verify the tunnel's DNS settings") {
			warned = true
		}
	}
	if !warned {
		t.Error("expected the guard error to be logged")
	}
}

// TestProbeResolverOverUDP_AcceptsAnyReply proves the probe measures the round
// trip, not the answer: a resolver that refuses the question still proves the
// tunnel carries traffic, and rebuilding on that would kill a working session.
func TestProbeResolverOverUDP_AcceptsAnyReply(t *testing.T) {
	const rcodeRefused = 5
	server, stop := stubDNSServer(t, func(query []byte) []byte {
		reply := make([]byte, len(query))
		copy(reply, query)
		reply[2] = 0x80 // QR
		reply[3] = rcodeRefused
		return reply
	})
	defer stop()

	if err := probeResolverWithDialer(context.Background(), &net.Dialer{}, server); err != nil {
		t.Errorf("probe failed against a responding resolver: %v", err)
	}
}

// TestProbeResolverOverUDP_IgnoresAForeignReply proves a datagram carrying
// someone else's transaction ID does not pass the probe.
func TestProbeResolverOverUDP_IgnoresAForeignReply(t *testing.T) {
	server, stop := stubDNSServer(t, func(query []byte) []byte {
		reply := make([]byte, len(query))
		copy(reply, query)
		binary.BigEndian.PutUint16(reply[0:2], binary.BigEndian.Uint16(query[0:2])+1)
		reply[2] = 0x80
		return reply
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := probeResolverWithDialer(ctx, &net.Dialer{}, server); err == nil {
		t.Error("expected the probe to reject a reply with a foreign transaction ID")
	}
}

// TestProbeResolverOverUDP_FailsWhenNothingAnswers is the black-holed tunnel:
// the query goes out and nothing comes back.
func TestProbeResolverOverUDP_FailsWhenNothingAnswers(t *testing.T) {
	server, stop := stubDNSServer(t, func([]byte) []byte { return nil })
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := probeResolverWithDialer(ctx, &net.Dialer{}, server); err == nil {
		t.Error("expected the probe to fail when the resolver never answers")
	}
}

// TestProbeResolverOverUDP_IgnoresAWrongQuestion proves the reply must echo
// back the question we asked, not just carry our transaction ID — a same-IP
// LAN responder that guesses or replays an ID cannot forge the question too.
func TestProbeResolverOverUDP_IgnoresAWrongQuestion(t *testing.T) {
	server, stop := stubDNSServer(t, func(query []byte) []byte {
		reply := make([]byte, len(query))
		copy(reply, query)
		reply[2] = 0x80  // QR
		reply[16] = 0x0f // QCLASS changed from IN(1) to CH(15)
		return reply
	})
	defer stop()

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	if err := probeResolverWithDialer(ctx, &net.Dialer{}, server); err == nil {
		t.Error("expected the probe to reject a reply whose question section does not match")
	}
}

// TestRootProbeQuery proves the query is a well-formed root-zone request.
func TestRootProbeQuery(t *testing.T) {
	msg, questionEnd, id, err := rootProbeQuery()
	if err != nil {
		t.Fatalf("rootProbeQuery: %v", err)
	}

	if questionEnd != 17 {
		t.Fatalf("question section ends at %d, want 17", questionEnd)
	}
	if len(msg) <= questionEnd {
		t.Fatalf("query length = %d, want the padded OPT record after the question", len(msg))
	}
	if got := binary.BigEndian.Uint16(msg[10:12]); got != 1 {
		t.Errorf("ARCOUNT = %d, want 1 (the OPT record)", got)
	}
	if got := binary.BigEndian.Uint16(msg[0:2]); got != id {
		t.Errorf("transaction ID in message = %d, want the reported %d", got, id)
	}
	if got := binary.BigEndian.Uint16(msg[2:4]); got != 0x0100 {
		t.Errorf("flags = %#04x, want 0x0100 (standard query, recursion desired)", got)
	}
	if got := binary.BigEndian.Uint16(msg[4:6]); got != 1 {
		t.Errorf("QDCOUNT = %d, want 1", got)
	}
	if msg[12] != 0 {
		t.Errorf("QNAME = %#02x, want 0x00 (the root label)", msg[12])
	}
	if got := binary.BigEndian.Uint16(msg[13:15]); !slices.Contains(rootProbeQTypes, got) {
		t.Errorf("QTYPE = %d, want one of the root-zone probe types %v", got, rootProbeQTypes)
	}
	if got := binary.BigEndian.Uint16(msg[15:17]); got != 1 {
		t.Errorf("QCLASS = %d, want 1 (IN)", got)
	}

	// A repeat is possible by chance (1/65536), but a constant ID would make
	// the probe trivially spoofable by any stale datagram on the port.
	_, _, second, err := rootProbeQuery()
	if err != nil {
		t.Fatalf("rootProbeQuery: %v", err)
	}
	if second == id {
		t.Error("two queries drew the same transaction ID")
	}
}

func TestIsDNSReplyTo(t *testing.T) {
	query, questionEnd, id, err := rootProbeQuery()
	if err != nil {
		t.Fatalf("rootProbeQuery: %v", err)
	}

	reply := func(flags byte, mutate func([]byte)) []byte {
		msg := make([]byte, len(query))
		copy(msg, query)
		msg[2] = flags
		if mutate != nil {
			mutate(msg)
		}
		return msg
	}

	if !isDNSReplyTo(reply(0x80, nil), query, questionEnd, id) {
		t.Error("a response carrying our ID and question should match")
	}
	if isDNSReplyTo(reply(0x00, nil), query, questionEnd, id) {
		t.Error("a query (QR unset) is not a reply")
	}
	if isDNSReplyTo(reply(0x80, func(m []byte) { binary.BigEndian.PutUint16(m[0:2], id+1) }), query, questionEnd, id) {
		t.Error("a response carrying a foreign ID should not match")
	}
	if isDNSReplyTo(reply(0x80, func(m []byte) { m[questionEnd-1] = 0x0f }), query, questionEnd, id) {
		t.Error("a response whose question section does not match ours should not match")
	}
	if isDNSReplyTo(make([]byte, 11), query, questionEnd, id) {
		t.Error("a datagram shorter than the query should not match")
	}
}

// stubDNSServer answers UDP queries on loopback with respond(query); a nil
// return means "answer nothing". Tests cannot bind port 53, so it points
// dnsProbePort at the ephemeral port it got and restores it afterwards, and
// returns the host to hand the probe.
func stubDNSServer(t *testing.T, respond func(query []byte) []byte) (string, func()) {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	host, port, err := net.SplitHostPort(conn.LocalAddr().String())
	if err != nil {
		conn.Close()
		t.Fatalf("split stub resolver address: %v", err)
	}
	previous := currentDNSProbePort()
	dnsProbePort.Store(port)
	t.Cleanup(func() { dnsProbePort.Store(previous) })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := conn.ReadFrom(buf)
			if err != nil {
				return
			}
			if reply := respond(buf[:n]); reply != nil {
				_, _ = conn.WriteTo(reply, addr)
			}
		}
	}()

	return host, func() { _ = conn.Close() }
}
