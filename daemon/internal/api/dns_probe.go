package api

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

// dnsProbeInterval is how often a Connected session is tested end to end.
const dnsProbeInterval = 30 * time.Second

// dnsProbeTimeout bounds one resolver query.
const dnsProbeTimeout = 3 * time.Second

// dnsProbeFailuresBeforeRebuild is how many consecutive rounds must fail before
// the session is rebuilt. UDP loses datagrams and a resolver may drop one, so a
// single miss is not a dead tunnel; three across 90s is.
const dnsProbeFailuresBeforeRebuild = 3

// dnsProbeRebuildCooldown is the minimum gap between probe-driven rebuilds. A
// node that keeps answering handshakes but never carries traffic would otherwise
// be rebuilt every 90s forever, and each rebuild drops the tunnel for a second.
const dnsProbeRebuildCooldown = 5 * time.Minute

// dnsProbeServers is how many of the session's resolvers one round tries before
// calling it a failure, bounding a round at dnsProbeServers*dnsProbeTimeout.
const dnsProbeServers = 2

// dnsProbePort is the resolver port. A variable only so tests can point the
// probe at a stub on an ephemeral port; nothing reconfigures it at runtime.
var dnsProbePort = "53"

// dnsGuardCorrectionCooldown is how long the DNS guard waits after correcting
// the interface's resolvers. Normally the check runs every health tick and finds
// nothing to do; when something else is writing DNS too, this keeps the two from
// trading writes three seconds apart and turns the fight into a readable trail
// of log lines instead.
const dnsGuardCorrectionCooldown = 30 * time.Second

// ensureTunnelDNS re-asserts the session's resolvers on the host.
//
// This is the failure the probe cannot see: the tunnel carries traffic, so a
// query aimed at a resolver over it is answered, but the host has stopped
// sending its queries there. Windows gives interface DNS to whoever wrote last,
// so another VPN client's DNS enforcement — or a Windows component re-profiling
// the adapter — takes it over silently, and every name lookup on the machine
// fails while the VPN reports a healthy connection.
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

// probeResolverOverUDP asks a resolver for the root NS set and reports whether a
// matching reply comes back. Nothing binds the socket to the tunnel: while a
// session is up the tunnel owns the default route at metric 0, so the query
// leaves through it, and if it does not the kill switch drops it — which is the
// answer the probe is looking for either way.
//
// Any well-formed reply counts, including an error RCODE. The probe asks whether
// the tunnel still carries a round trip, not whether the resolver liked the
// question, and treating REFUSED as a dead tunnel would rebuild a working
// session. The root NS set belongs to no third party, every recursive resolver
// answers it from cache, and the ~500 byte response is far larger than the
// ~150 byte handshake packets that keep passing when a path has stopped
// carrying anything bigger.
func probeResolverOverUDP(ctx context.Context, server string) error {
	conn, err := (&net.Dialer{}).DialContext(ctx, "udp", net.JoinHostPort(server, dnsProbePort))
	if err != nil {
		return err
	}
	defer conn.Close()

	deadline := time.Now().Add(dnsProbeTimeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}

	query, id := rootNSQuery()
	if _, err := conn.Write(query); err != nil {
		return err
	}

	buf := make([]byte, 1500)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return err
		}
		if isDNSReplyTo(buf[:n], id) {
			return nil
		}
		// Someone else's datagram on our port; keep waiting out the deadline.
	}
}

// rootNSQuery builds a `. IN NS` query and returns it with the transaction ID to
// match the reply against.
func rootNSQuery() ([]byte, uint16) {
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		binary.BigEndian.PutUint16(idBytes[:], uint16(time.Now().UnixNano()))
	}
	id := binary.BigEndian.Uint16(idBytes[:])

	msg := make([]byte, 0, 17)
	msg = binary.BigEndian.AppendUint16(msg, id)
	msg = binary.BigEndian.AppendUint16(msg, 0x0100) // standard query, recursion desired
	msg = binary.BigEndian.AppendUint16(msg, 1)      // QDCOUNT
	msg = binary.BigEndian.AppendUint16(msg, 0)      // ANCOUNT
	msg = binary.BigEndian.AppendUint16(msg, 0)      // NSCOUNT
	msg = binary.BigEndian.AppendUint16(msg, 0)      // ARCOUNT
	msg = append(msg, 0)                             // QNAME: the root label
	msg = binary.BigEndian.AppendUint16(msg, 2)      // QTYPE: NS
	msg = binary.BigEndian.AppendUint16(msg, 1)      // QCLASS: IN
	return msg, id
}

// isDNSReplyTo reports whether msg is a response carrying our transaction ID.
func isDNSReplyTo(msg []byte, id uint16) bool {
	const headerLen = 12
	if len(msg) < headerLen {
		return false
	}
	if binary.BigEndian.Uint16(msg[0:2]) != id {
		return false
	}
	return msg[2]&0x80 != 0 // QR
}

// dataPathIsDead reports whether the tunnel has stopped carrying traffic while
// WireGuard still handshakes.
//
// The handshake alone is a weak liveness signal: its packets are ~150 bytes and
// the peer answers them from the node itself, so a relay that has stopped
// forwarding, or a path that has stopped passing anything full-sized, leaves the
// session rekeying happily every two minutes while nothing the user does works.
// That is the state a browser reports as a DNS probe error. Resolving over the
// tunnel is the cheapest end-to-end check and covers exactly what breaks first.
//
// A round runs at most every dnsProbeInterval and only escalates after
// dnsProbeFailuresBeforeRebuild consecutive failures.
func (s *Service) dataPathIsDead(ctx context.Context, profile state.Profile) bool {
	if s.probeResolver == nil {
		return false
	}
	servers := wg.Resolvers(profile.WireGuard)
	if len(servers) == 0 {
		return false
	}
	if !s.dnsProbeDue() {
		return false
	}

	if len(servers) > dnsProbeServers {
		servers = servers[:dnsProbeServers]
	}
	var lastErr error
	for _, server := range servers {
		probeCtx, cancel := context.WithTimeout(ctx, dnsProbeTimeout)
		err := s.probeResolver(probeCtx, server)
		cancel()
		if err == nil {
			s.recordDNSProbeSuccess()
			return false
		}
		lastErr = fmt.Errorf("%s: %w", server, err)
	}

	failures, rebuild := s.recordDNSProbeFailure()
	s.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
		"tunnel is handshaking but did not resolve over it (attempt %d/%d): %v", failures, dnsProbeFailuresBeforeRebuild, lastErr))
	return rebuild
}

// dnsProbeDue reports whether a round is due, claiming the slot when it is so
// the 3s health tick does not probe on every pass.
func (s *Service) dnsProbeDue() bool {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	if time.Now().Before(s.dnsProbeNextAt) {
		return false
	}
	s.dnsProbeNextAt = time.Now().Add(dnsProbeInterval)
	return true
}

func (s *Service) recordDNSProbeSuccess() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeFailures = 0
}

// recordDNSProbeFailure books a failed round and reports the running count plus
// whether it is time to rebuild: enough consecutive failures, and outside the
// cooldown that keeps a node which never recovers from being rebuilt on a loop.
func (s *Service) recordDNSProbeFailure() (int, bool) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()

	s.dnsProbeFailures++
	failures := s.dnsProbeFailures
	if failures < dnsProbeFailuresBeforeRebuild || time.Now().Before(s.dnsProbeQuietUntil) {
		return failures, false
	}
	s.dnsProbeFailures = 0
	s.dnsProbeQuietUntil = time.Now().Add(dnsProbeRebuildCooldown)
	return failures, true
}

// resetDNSProbe restarts the schedule for a tunnel that has just come up, so it
// is not judged on the previous one's failures and gets a round to settle.
//
// The cooldown deliberately survives: a rebuild ends here too, and clearing it
// would let a node that answers handshakes but never carries traffic be rebuilt
// every dnsProbeFailuresBeforeRebuild rounds forever.
func (s *Service) resetDNSProbe() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeFailures = 0
	s.dnsProbeNextAt = time.Now().Add(dnsProbeInterval)
}

// endDNSProbeSession clears the schedule outright once a session is over, so the
// next one the user starts is not held back by the last one's cooldown.
func (s *Service) endDNSProbeSession() {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()
	s.dnsProbeFailures = 0
	s.dnsProbeNextAt = time.Time{}
	s.dnsProbeQuietUntil = time.Time{}
}
