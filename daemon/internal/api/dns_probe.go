package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

// dnsProbeIntervalMin/Max bound the gap between rounds, redrawn per round: a
// fixed period is a beacon in the encrypted flow even when the payload is not.
const (
	dnsProbeIntervalMin = 8 * time.Second
	dnsProbeIntervalMax = 22 * time.Second
)

// dnsProbeTimeout bounds one resolver query.
const dnsProbeTimeout = 3 * time.Second

// dnsProbeFailuresBeforeRebuild: UDP loses datagrams, so one miss isn't a dead
// tunnel, but two in a row is.
const dnsProbeFailuresBeforeRebuild = 2

// dnsProbeRebuildCooldown keeps a node that answers handshakes but never
// carries traffic from being rebuilt every 90s forever.
const dnsProbeRebuildCooldown = 5 * time.Minute

// dnsProbeServers is how many of the session's resolvers one round tries before
// calling it a failure, bounding a round at dnsProbeServers*dnsProbeTimeout.
const dnsProbeServers = 2

// dataPathGateAttempts is how many queries a candidate gets during bring-up
// before it is rejected; the delay gives a just-up tunnel time to settle.
const (
	dataPathGateAttempts   = 2
	dataPathGateRetryDelay = 300 * time.Millisecond
)

// dataPathGateBudget bounds the wait on a host that has not made the tunnel's
// adapter usable yet; a ready host — blocked or not — spends none of it.
const dataPathGateBudget = 12 * time.Second

// dataPathRescueBytes: what the peer must send during a probe window to prove the
// tunnel carries traffic the host kept from the probe socket; keepalives (32 B) and a rekey (148 B) can't reach it.
const dataPathRescueBytes = 256

// dataPathRescueLogInterval spaces the health loop's rescue lines on a host
// that swallows every probe reply.
const dataPathRescueLogInterval = 10 * time.Minute

// dnsProbeRetransmitAfter is when an attempt resends its query inside its own
// timeout, so one dropped datagram costs a resend rather than a whole attempt.
const dnsProbeRetransmitAfter = time.Second

// nextDNSProbeDelay draws a uniform gap in [min, max]. A failed read falls back
// to the midpoint — a regular cadence is worse than a random one, never wrong.
func nextDNSProbeDelay() time.Duration {
	spread := int64(dnsProbeIntervalMax - dnsProbeIntervalMin)
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return dnsProbeIntervalMin + time.Duration(spread/2)
	}
	offset := int64(binary.BigEndian.Uint64(buf[:]) % uint64(spread+1))
	return dnsProbeIntervalMin + time.Duration(offset)
}

// dnsProbePort is atomic so tests may point the probe at a stub port from a
// goroutine without racing the health loop's reads.
var dnsProbePort atomic.Value

func init() {
	dnsProbePort.Store("53")
}

func currentDNSProbePort() string {
	return dnsProbePort.Load().(string)
}

// dnsGuardCorrectionCooldown keeps this guard and another DNS writer from
// trading writes seconds apart, turning a fight into a readable log trail.
const dnsGuardCorrectionCooldown = 30 * time.Second

// errDNSProbeInconclusive marks a round cancelled mid-flight by a Switch or
// Disconnect: neither a success nor a failure.
var errDNSProbeInconclusive = errors.New("dns probe: round did not complete")

// errDNSProbeNotReady marks a round that never left the host: retryable, and
// never a verdict.
var errDNSProbeNotReady = errors.New("dns probe: the tunnel adapter is not ready")

// errDNSProbeConnRefused marks an ICMP port-unreachable: proof the round trip
// happened, so it counts as evidence the tunnel carries traffic.
var errDNSProbeConnRefused = errors.New("dns probe: resolver refused the connection")

// errDNSProbeOversizedReply marks a reply too large for the read buffer: also
// proof of a round trip, since only the userspace copy failed.
var errDNSProbeOversizedReply = errors.New("dns probe: reply larger than the read buffer")

// errDataPathGate marks a candidate the bring-up gate rejected. Its text quotes
// a socket pinned to the tunnel, whose "no route" says nothing about the host.
var errDataPathGate = errors.New("rejected by the data-path gate")

type dataPathGateError struct{ err error }

func (e *dataPathGateError) Error() string        { return e.err.Error() }
func (e *dataPathGateError) Unwrap() error        { return e.err }
func (e *dataPathGateError) Is(target error) bool { return target == errDataPathGate }

// ensureTunnelDNS re-asserts the session's resolvers on the host: the tunnel
// can carry traffic while the host has silently stopped querying it.
func (s *Service) ensureTunnelDNS(ctx context.Context, profile state.Profile) {
	guard, ok := s.wg.(wgDNSGuard)
	if !ok || !s.dnsGuardDue() {
		return
	}

	corrected, err := guard.EnsureDNS(ctx, profile.WireGuard)
	switch {
	case err != nil:
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not verify the tunnel's DNS settings: %v", err))
	case corrected:
		s.holdDNSGuard(dnsGuardCorrectionCooldown)
		s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
			"host had stopped pointing at the tunnel's resolvers (%s); re-applied them",
			strings.Join(wg.Resolvers(profile.WireGuard), ", ")))
	}
}

func (s *Service) dnsGuardDue() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	return !time.Now().Before(s.dnsGuardNextAt)
}

func (s *Service) holdDNSGuard(d time.Duration) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsGuardNextAt = time.Now().Add(d)
}

// activeTunnelInterface finds the host's VPN adapter by name, since Allow-LAN
// routes RFC1918 ranges off-tunnel and the default route can't be trusted.
func activeTunnelInterface() (string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return "", err
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}
		if isTunnelInterfaceName(iface.Name, nil) {
			return iface.Name, nil
		}
	}
	return "", errors.New("no tunnel interface is up")
}

// probeResolverOverUDP asks a resolver a root-zone question over a socket
// pinned to tunnelInterface; any well-formed reply, even REFUSED, counts.
func probeResolverOverUDP(ctx context.Context, tunnelInterface, server string) error {
	// A local setup failure is the host still bringing the adapter up: retryable,
	// and never a pass on a tunnel nothing has crossed.
	iface := strings.TrimSpace(tunnelInterface)
	if iface == "" {
		var err error
		iface, err = activeTunnelInterface()
		if err != nil {
			return fmt.Errorf("%w: find tunnel interface: %v", errDNSProbeNotReady, err)
		}
	}
	dialer, err := bindDialerToInterface(iface)
	if err != nil {
		return fmt.Errorf("%w: bind to tunnel interface %s: %v", errDNSProbeNotReady, iface, err)
	}
	return probeResolverWithDialer(ctx, dialer, server)
}

// probeResolverWithDialer is the dial-and-match core, split out so it can be
// tested against an unbound dialer without a real tunnel interface.
func probeResolverWithDialer(ctx context.Context, dialer *net.Dialer, server string) error {
	conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(server, currentDNSProbePort()))
	if err != nil {
		return classifyProbeSendError(err)
	}
	defer conn.Close()
	enableUnreachableReports(conn)

	deadline := time.Now().Add(dnsProbeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	query, questionEnd, id, err := rootProbeQuery()
	if err != nil {
		return fmt.Errorf("build probe query: %w", err)
	}
	if _, err := conn.Write(query); err != nil {
		return classifyProbeSendError(err)
	}

	retransmitAt := time.Now().Add(dnsProbeRetransmitAfter)
	if !retransmitAt.Before(deadline) {
		retransmitAt = deadline
	}
	retransmitted := false

	buf := make([]byte, maxProbeReplySize)
	for {
		readDeadline := deadline
		if !retransmitted {
			readDeadline = retransmitAt
		}
		if err := conn.SetReadDeadline(readDeadline); err != nil {
			return err
		}
		n, err := conn.Read(buf)
		if err != nil {
			// A datagram dropped right after the handshake costs a resend, not the attempt.
			if !retransmitted && isTimeout(err) && ctx.Err() == nil && time.Now().Before(deadline) {
				retransmitted = true
				if _, err := conn.Write(query); err != nil {
					return classifyProbeSendError(err)
				}
				continue
			}
			return classifyProbeReadError(ctx, err)
		}
		if isDNSReplyTo(buf[:n], query, questionEnd, id) {
			return nil
		}
		// Someone else's datagram on our port; keep waiting out the deadline.
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// classifyProbeSendError: a dial or send the host could not route out of the
// tunnel is the adapter not being ready, never a verdict on the tunnel.
func classifyProbeSendError(err error) error {
	if isNetworkUnreachable(err) {
		return fmt.Errorf("%w: the tunnel's route is not published yet", errDNSProbeNotReady)
	}
	return err
}

// classifyProbeReadError sorts a failed read into the round's outcome: an
// ECONNRESET/ECONNREFUSED actually proves the tunnel carried the round trip.
func classifyProbeReadError(ctx context.Context, err error) error {
	// Only a deliberate cancel (Switch/Disconnect) is inconclusive; the probe's
	// own deadline expiring IS the no-answer the round exists to detect.
	if errors.Is(ctx.Err(), context.Canceled) {
		return fmt.Errorf("%w: %v", errDNSProbeInconclusive, ctx.Err())
	}
	if isConnRefused(err) {
		return errDNSProbeConnRefused
	}
	if isOversizedDatagram(err) {
		return errDNSProbeOversizedReply
	}
	return err
}

// dnsProbeMaxPadding caps the EDNS0 padding drawn per query. Enough spread that
// consecutive probes never share a length, small enough to stay one datagram.
const dnsProbeMaxPadding = 64

// Root-zone questions every resolver answers from its priming cache, sized
// apart. DNSKEY/DNSSEC are out: their ~1 KB answers a tight path drops.
var rootProbeQTypes = []uint16{
	2,  // NS: the root NS set, ~500 B
	6,  // SOA: one record, ~100 B
	1,  // A: NODATA, ~50 B
	28, // AAAA: NODATA, ~50 B
}

// rootProbePayloadSizes are the advertised EDNS0 buffer sizes. Which one is sent
// decides where the resolver truncates, which varies the reply's size too.
var rootProbePayloadSizes = []uint16{512, 1232, 1400, 4096}

// maxProbeReplySize must cover the largest advertised size: Windows fails an
// oversized recvfrom outright (WSAEMSGSIZE) where Unix just truncates.
const maxProbeReplySize = 4096

// rootProbeQuery draws the transaction ID, question, buffer size and padding
// per query, so neither request nor reply has a size a censor can lock onto.
func rootProbeQuery() ([]byte, int, uint16, error) {
	var seed [5]byte
	if _, err := rand.Read(seed[:]); err != nil {
		return nil, 0, 0, fmt.Errorf("generate probe transaction ID: %w", err)
	}
	id := binary.BigEndian.Uint16(seed[:2])
	padding := make([]byte, int(seed[2])%(dnsProbeMaxPadding+1))
	qtype := rootProbeQTypes[int(seed[3])%len(rootProbeQTypes)]
	payloadSize := rootProbePayloadSizes[int(seed[4])%len(rootProbePayloadSizes)]
	msg := make([]byte, 0, 17+11+len(padding))
	msg = binary.BigEndian.AppendUint16(msg, id)
	msg = binary.BigEndian.AppendUint16(msg, 0x0100) // standard query, recursion desired
	msg = binary.BigEndian.AppendUint16(msg, 1)      // QDCOUNT
	msg = binary.BigEndian.AppendUint16(msg, 0)      // ANCOUNT
	msg = binary.BigEndian.AppendUint16(msg, 0)      // NSCOUNT
	msg = binary.BigEndian.AppendUint16(msg, 1)      // ARCOUNT: the OPT record below
	msg = append(msg, 0)                             // QNAME: the root label
	msg = binary.BigEndian.AppendUint16(msg, qtype)
	msg = binary.BigEndian.AppendUint16(msg, 1) // QCLASS: IN
	questionEnd := len(msg)

	msg = append(msg, 0)                                             // OPT owner: root
	msg = binary.BigEndian.AppendUint16(msg, 41)                     // TYPE: OPT
	msg = binary.BigEndian.AppendUint16(msg, payloadSize)            // advertised UDP payload size
	msg = binary.BigEndian.AppendUint32(msg, 0)                      // extended RCODE and flags, DO clear
	msg = binary.BigEndian.AppendUint16(msg, uint16(len(padding)+4)) // RDLENGTH
	msg = binary.BigEndian.AppendUint16(msg, 12)                     // option code: PADDING
	msg = binary.BigEndian.AppendUint16(msg, uint16(len(padding)))   // option length
	msg = append(msg, padding...)
	return msg, questionEnd, id, nil
}

// isDNSReplyTo checks the transaction ID, QR bit, and echoed question; ID
// alone would let a same-IP LAN responder pass as the tunnel resolver.
func isDNSReplyTo(msg, query []byte, questionEnd int, id uint16) bool {
	const headerLen = 12
	if len(msg) < questionEnd || questionEnd > len(query) {
		return false
	}
	if binary.BigEndian.Uint16(msg[0:2]) != id {
		return false
	}
	if msg[2]&0x80 == 0 { // QR
		return false
	}
	return bytes.Equal(msg[headerLen:questionEnd], query[headerLen:questionEnd])
}

// dataPathIsDead reports whether the tunnel has stopped carrying traffic while
// WireGuard still handshakes: a relay can keep rekeying while forwarding nothing.
func (s *Service) dataPathIsDead(ctx context.Context, profile state.Profile) bool {
	if s.probeResolver == nil {
		return false
	}
	servers := wg.ProbeResolvers(profile.WireGuard)
	if len(servers) == 0 {
		return false
	}
	if !s.dnsProbeDue() {
		return false
	}

	servers = probeServerOrder(servers)
	iface := s.resolveWireGuardInterfaceName(ctx, profile.WireGuard)
	rxBefore, rxKnown := s.peerRxBytes(ctx, profile.WireGuard)
	var lastErr error
	for _, server := range servers {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		err := s.probeResolver(probeCtx, iface, server)
		cancel()
		switch {
		case err == nil, errors.Is(err, errDNSProbeConnRefused), errors.Is(err, errDNSProbeOversizedReply):
			s.recordDNSProbeSuccess()
			return false
		case errors.Is(err, errDNSProbeInconclusive), errors.Is(err, errDNSProbeNotReady):
			return false
		}
		lastErr = fmt.Errorf("%s: %w", server, err)
	}

	// A round runs with opMu unheld; if a Switch landed during it, the failure
	// belongs to a session that is no longer the live one.
	if current, ok := s.getCurrentProfile(); !ok || current.ID != profile.ID {
		return false
	}

	if rxKnown {
		if rxAfter, ok := s.peerRxBytes(ctx, profile.WireGuard); ok {
			delta := rxAfter - rxBefore
			if delta >= dataPathRescueBytes {
				s.recordDNSProbeSuccess()
				if s.dataPathRescueLogDue() {
					s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
						"tunnel carried %d bytes from the peer during a round whose resolver query went unanswered; the host is swallowing the daemon's probe replies: %v", delta, lastErr))
				}
				return false
			}
			lastErr = fmt.Errorf("%w (peer sent %d bytes during the round)", lastErr, delta)
		}
	}

	failures, rebuild := s.recordDNSProbeFailure()
	s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
		"tunnel is handshaking but did not resolve over it (attempt %d/%d): %v", failures, dnsProbeFailuresBeforeRebuild, lastErr))
	return rebuild
}

// probeServerOrder trims the resolver list to one round's worth, starting at a
// random member so the same resolver is not always asked first.
func probeServerOrder(servers []string) []string {
	var seed [1]byte
	if _, err := rand.Read(seed[:]); err == nil {
		start := int(seed[0]) % len(servers)
		servers = append(append([]string(nil), servers[start:]...), servers[:start]...)
	}
	if len(servers) > dnsProbeServers {
		servers = servers[:dnsProbeServers]
	}
	return servers
}

// canProveDataPath reports whether traffic can be judged directly. Where it
// cannot, the handshake is the only liveness signal there is.
func (s *Service) canProveDataPath(profile state.Profile) bool {
	return s.probeResolver != nil && len(wg.ProbeResolvers(profile.WireGuard)) > 0
}

// proveDataPath is the bring-up gate: a candidate must carry a round trip, not
// just handshake. No resolvers passes as non-evidence; a cancelled gate is a teardown.
func (s *Service) proveDataPath(ctx context.Context, kind string, wireGuardProfile state.WireGuardProfile) error {
	if s.probeResolver == nil {
		return nil
	}
	servers := wg.ProbeResolvers(wireGuardProfile)
	if len(servers) == 0 {
		return nil
	}
	servers = probeServerOrder(servers)

	budget := s.dataPathBudget
	if budget <= 0 {
		budget = dataPathGateBudget
	}
	deadline := time.Now().Add(budget)
	s.awaitTunnelReady(ctx, wireGuardProfile, deadline)
	iface := s.resolveWireGuardInterfaceName(ctx, wireGuardProfile)
	rxBefore, rxKnown := s.peerRxBytes(ctx, wireGuardProfile)

	var lastErr error
	delay := false
	for attempt := 0; attempt < dataPathGateAttempts; {
		if delay {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(dataPathGateRetryDelay):
			}
		}
		delay = true

		server := servers[attempt%len(servers)]
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		err := s.probeResolver(probeCtx, iface, server)
		cancel()
		switch {
		case err == nil, errors.Is(err, errDNSProbeConnRefused), errors.Is(err, errDNSProbeOversizedReply):
			return nil
		case errors.Is(err, errDNSProbeInconclusive):
			// Only a Disconnect cancels a bring-up: passing would log a verified
			// tunnel and reach CONNECTED on a session already being torn down.
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return nil
		case errors.Is(err, errDNSProbeNotReady):
			// Nothing left the host, so this is neither a verdict nor an attempt.
			if !time.Now().Before(deadline) {
				return &dataPathGateError{fmt.Errorf("tunnel came up but the host had not made its adapter usable in time: %w", err)}
			}
			continue
		}
		lastErr = fmt.Errorf("%s: %w", server, err)
		attempt++
	}

	// The socket heard nothing, but bytes the peer sent meanwhile prove the transport
	// carries traffic and the host kept the reply from the daemon: apps work there, as before this gate.
	if rxKnown {
		if rxAfter, ok := s.peerRxBytes(ctx, wireGuardProfile); ok {
			delta := rxAfter - rxBefore
			if delta >= dataPathRescueBytes {
				s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
					"%s tunnel carried %d bytes from the peer while the daemon's own resolver query went unanswered; accepting it, the host is swallowing the daemon's probe replies: %v", kind, delta, lastErr))
				return nil
			}
			lastErr = fmt.Errorf("%w (peer sent %d bytes during the probe)", lastErr, delta)
		}
	}
	return &dataPathGateError{fmt.Errorf("tunnel came up but did not carry traffic: %w", lastErr)}
}

// peerRxBytes reads the peer's received-byte counter, or reports it unknown.
func (s *Service) peerRxBytes(ctx context.Context, profile state.WireGuardProfile) (int64, bool) {
	status, err := s.wg.Status(ctx, profile)
	if err != nil || !status.Running {
		return 0, false
	}
	return status.BytesIn, true
}

// dataPathRescueLogDue claims the next rescue log slot, so a session whose host
// swallows every reply says so once per interval rather than every round.
func (s *Service) dataPathRescueLogDue() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if time.Now().Before(s.dataPathRescueLoggedAt.Add(dataPathRescueLogInterval)) {
		return false
	}
	s.dataPathRescueLoggedAt = time.Now()
	return true
}

// awaitTunnelReady returns the moment the host has the tunnel's adapter usable,
// or when the budget or ctx runs out and the attempts below give the verdict.
func (s *Service) awaitTunnelReady(ctx context.Context, wireGuardProfile state.WireGuardProfile, deadline time.Time) {
	reporter, ok := s.wg.(wgTunnelReadiness)
	if !ok {
		return
	}
	for {
		ready, err := reporter.TunnelReady(ctx, wireGuardProfile)
		if err != nil {
			s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("could not tell whether the tunnel adapter was ready: %v", err))
			return
		}
		if ready || !time.Now().Before(deadline) {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(dataPathGateRetryDelay):
		}
	}
}

// dnsProbeDue reports whether a round is due, claiming the slot when it is so
// the 3s health tick does not probe on every pass.
func (s *Service) dnsProbeDue() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if time.Now().Before(s.dnsProbeNextAt) {
		return false
	}
	s.dnsProbeNextAt = time.Now().Add(nextDNSProbeDelay())
	return true
}

func (s *Service) recordDNSProbeSuccess() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeFailures = 0
}

// recordDNSProbeFailure books a failed round and reports the count plus whether
// it is time to rebuild: enough consecutive failures, and outside the cooldown.
func (s *Service) recordDNSProbeFailure() (int, bool) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()

	s.dnsProbeFailures++
	failures := s.dnsProbeFailures
	// One miss is enough right after a resume: the host just woke, so the
	// debounce against UDP loss only delays a near-certain rebuild.
	threshold := dnsProbeFailuresBeforeRebuild
	if time.Now().Before(s.resumeFreshUntil) {
		threshold = 1
	}
	if failures < threshold {
		return failures, false
	}
	s.dnsProbeFailures = 0
	if time.Now().Before(s.dnsProbeQuietUntil) {
		return failures, false
	}
	s.dnsProbeQuietUntil = time.Now().Add(dnsProbeRebuildCooldown)
	return failures, true
}

// deferDataPathRebuild gives back the rebuild a round just claimed, so a tunnel
// judged while the host had no network is re-judged as soon as it is back.
func (s *Service) deferDataPathRebuild() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeFailures = 0
	s.dnsProbeQuietUntil = time.Time{}
}

// resetDNSProbe restarts the schedule for a tunnel that has just come up. The
// cooldown deliberately survives, since a rebuild ends here too.
func (s *Service) resetDNSProbe() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeFailures = 0
	s.dnsProbeNextAt = time.Now().Add(nextDNSProbeDelay())
}

// endDNSProbeSession clears the schedule outright once a session is over, so the
// next one the user starts is not held back by the last one's cooldown.
func (s *Service) endDNSProbeSession() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeFailures = 0
	s.dnsProbeNextAt = time.Time{}
	s.dnsProbeQuietUntil = time.Time{}
	s.dataPathRescueLoggedAt = time.Time{}
}
