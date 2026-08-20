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
	"syscall"
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

// dnsProbePort is the resolver port, held in an atomic.Value so tests may point
// the probe at a stub on an ephemeral port from a goroutine without racing the
// health loop's reads; nothing reconfigures it at runtime otherwise.
var dnsProbePort atomic.Value

func init() {
	dnsProbePort.Store("53")
}

func currentDNSProbePort() string {
	return dnsProbePort.Load().(string)
}

// dnsGuardCorrectionCooldown is how long the DNS guard waits after correcting
// the interface's resolvers. Normally the check runs every health tick and finds
// nothing to do; when something else is writing DNS too, this keeps the two from
// trading writes three seconds apart and turns the fight into a readable trail
// of log lines instead.
const dnsGuardCorrectionCooldown = 30 * time.Second

// errDNSProbeInconclusive marks a round that could not run to completion — a
// Switch or Disconnect cancelled the health check's context mid-flight — so it
// must not be booked as either a success or a failure.
var errDNSProbeInconclusive = errors.New("dns probe: round did not complete")

// errDNSProbeConnRefused marks an ICMP port-unreachable on the connected
// socket: the resolver rejected the port, but that reply is proof the round
// trip happened, so it counts as evidence the tunnel is carrying traffic.
var errDNSProbeConnRefused = errors.New("dns probe: resolver refused the connection")

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

// activeTunnelInterface finds the host's VPN adapter by name. Allow-LAN
// subtracts RFC1918 ranges (including the tunnel's own resolvers) from
// AllowedIPs and routes them off-tunnel, so the probe must be pinned to this
// interface rather than trusting whatever currently owns the default route.
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

// probeResolverOverUDP asks a resolver for the root NS set and reports whether a
// matching reply comes back. The socket is pinned to the tunnel interface (see
// bindDialerToInterface's platform variants), so the query provably leaves —
// and the reply is provably read — over the tunnel rather than the LAN.
//
// Any well-formed reply counts, including an error RCODE. The probe asks whether
// the tunnel still carries a round trip, not whether the resolver liked the
// question, and treating REFUSED as a dead tunnel would rebuild a working
// session. The root NS set belongs to no third party, every recursive resolver
// answers it from cache, and the ~500 byte response is far larger than the
// ~150 byte handshake packets that keep passing when a path has stopped
// carrying anything bigger.
func probeResolverOverUDP(ctx context.Context, server string) error {
	iface, err := activeTunnelInterface()
	if err != nil {
		return fmt.Errorf("find tunnel interface: %w", err)
	}
	dialer, err := bindDialerToInterface(iface)
	if err != nil {
		return fmt.Errorf("bind to tunnel interface %s: %w", iface, err)
	}
	return probeResolverWithDialer(ctx, dialer, server)
}

// probeResolverWithDialer is probeResolverOverUDP's dial-and-match core, split
// out so protocol behavior (reply matching, timeouts) can be tested against an
// unbound dialer without depending on a real tunnel interface being up.
func probeResolverWithDialer(ctx context.Context, dialer *net.Dialer, server string) error {
	conn, err := dialer.DialContext(ctx, "udp", net.JoinHostPort(server, currentDNSProbePort()))
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

	query, id, err := rootNSQuery()
	if err != nil {
		return fmt.Errorf("build probe query: %w", err)
	}
	if _, err := conn.Write(query); err != nil {
		return err
	}

	buf := make([]byte, 1500)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return classifyProbeReadError(ctx, err)
		}
		if isDNSReplyTo(buf[:n], query, id) {
			return nil
		}
		// Someone else's datagram on our port; keep waiting out the deadline.
	}
}

// classifyProbeReadError sorts a failed read into the round's outcome: the
// health check's own context ending mid-round proves nothing, while an ICMP
// port-unreachable arriving as ECONNRESET/ECONNREFUSED on the connected socket
// proves the opposite of what it looks like — the tunnel carried the round trip.
func classifyProbeReadError(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return fmt.Errorf("%w: %v", errDNSProbeInconclusive, ctx.Err())
	}
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) {
		return errDNSProbeConnRefused
	}
	return err
}

// rootNSQuery builds a `. IN NS` query and returns it with the transaction ID to
// match the reply against. A predictable ID would make the reply-matching check
// trivially spoofable, so a failed crypto/rand.Read fails the probe outright
// rather than falling back to a guessable one.
func rootNSQuery() ([]byte, uint16, error) {
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, 0, fmt.Errorf("generate probe transaction ID: %w", err)
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
	return msg, id, nil
}

// isDNSReplyTo reports whether msg is a response to query: same transaction ID,
// QR set, and the same question section echoed back. Matching on ID alone lets
// a same-IP LAN responder — reached because the query leaked off-tunnel — pass
// as the tunnel resolver; the question section is spoofed far less cheaply.
func isDNSReplyTo(msg, query []byte, id uint16) bool {
	const headerLen = 12
	if len(msg) < len(query) {
		return false
	}
	if binary.BigEndian.Uint16(msg[0:2]) != id {
		return false
	}
	if msg[2]&0x80 == 0 { // QR
		return false
	}
	return bytes.Equal(msg[headerLen:len(query)], query[headerLen:])
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
		switch {
		case err == nil, errors.Is(err, errDNSProbeConnRefused):
			s.recordDNSProbeSuccess()
			return false
		case errors.Is(err, errDNSProbeInconclusive):
			return false
		}
		lastErr = fmt.Errorf("%s: %w", server, err)
	}

	// A round takes up to dnsProbeServers*dnsProbeTimeout with opMu unheld; if a
	// Switch landed during it, the failure belongs to a resolver list — and a
	// rebuild would hit a session — that is no longer the live one.
	if current, ok := s.getCurrentProfile(); !ok || current.ID != profile.ID {
		return false
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
// The count resets as soon as it reaches the threshold, in or out of cooldown,
// so it never climbs past dnsProbeFailuresBeforeRebuild and the first failure
// after the cooldown expires starts a fresh three-round debounce.
func (s *Service) recordDNSProbeFailure() (int, bool) {
	s.recoveryMu.Lock()
	defer s.recoveryMu.Unlock()

	s.dnsProbeFailures++
	failures := s.dnsProbeFailures
	if failures < dnsProbeFailuresBeforeRebuild {
		return failures, false
	}
	s.dnsProbeFailures = 0
	if time.Now().Before(s.dnsProbeQuietUntil) {
		return failures, false
	}
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
