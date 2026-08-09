// Package snowflake bridges WireGuard UDP traffic over a Tor Project
// Snowflake WebRTC rendezvous, as a last-resort DPI-evasion transport.
package snowflake

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sf "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/client/lib"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// Compile-time checks: Manager satisfies transport.Manager plus the two
// optional capabilities service.go type-asserts for (session readiness,
// dynamically-bound local port), same as cloak and naive.
var (
	_ transport.Manager           = (*Manager)(nil)
	_ transport.SessionWaiter     = (*Manager)(nil)
	_ transport.BoundPortReporter = (*Manager)(nil)
)

// snowflakeTransport is the subset of *sf.Transport this package depends
// on, so tests can substitute a fake dialer without a real broker.
type snowflakeTransport interface {
	Dial() (net.Conn, error)
}

// Manager runs an in-process loopback UDP listener that WireGuard's peer
// Endpoint points at, framing each datagram (see framing.go) onto a
// Snowflake client stream obtained via WebRTC rendezvous. Structurally
// mirrors cloak.inProcessManager: RWMutex-guarded state, a generation
// counter so a stale run() goroutine can't clobber a fresher Start, and a
// done channel Stop waits on.
type Manager struct {
	mu       sync.RWMutex
	logs     *state.LogStore
	running  bool
	stopping bool

	udpConn *net.UDPConn
	stream  net.Conn

	done       chan struct{}
	session    chan struct{}
	hasSession bool

	boundLocalPort int
	generation     uint64
	cancel         context.CancelFunc
}

func NewManager(logs *state.LogStore) *Manager {
	return &Manager{logs: logs}
}

// newSnowflakeClient is overridable in tests so Start can be exercised
// without a real broker/WebRTC stack.
var newSnowflakeClient = func(cfg sf.ClientConfig) (snowflakeTransport, error) {
	return sf.NewSnowflakeClient(cfg)
}

func (m *Manager) Start(ctx context.Context, profile state.SnowflakeProfile) error {
	_ = ctx

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}

	brokerURL := strings.TrimSpace(profile.BrokerURL)
	if brokerURL == "" {
		m.mu.Unlock()
		return errors.New("snowflake broker URL is required")
	}
	if profile.LocalPort < 0 {
		m.mu.Unlock()
		return fmt.Errorf("LocalPort must be >= 0, got %d", profile.LocalPort)
	}

	localAddr := net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", profile.LocalPort))
	udpAddr, err := net.ResolveUDPAddr("udp", localAddr)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("resolve local UDP addr %s: %w", localAddr, err)
	}
	udpConn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		m.mu.Unlock()
		return fmt.Errorf("listen UDP %s: %w", localAddr, err)
	}
	boundPort := udpAddr.Port
	if laddr, ok := udpConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}

	clientCfg := sf.ClientConfig{
		BrokerURL:          brokerURL,
		FrontDomains:       profile.FrontDomains,
		AmpCacheURL:        profile.AmpCacheURL,
		ICEAddresses:       profile.ICEServers,
		BridgeFingerprint:  profile.BridgeFingerprint,
		KeepLocalAddresses: profile.KeepLocalAddresses,
		Max:                1,
	}

	sfTransport, err := newSnowflakeClient(clientCfg)
	if err != nil {
		udpConn.Close()
		m.mu.Unlock()
		return fmt.Errorf("build snowflake client: %w", err)
	}

	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	session := make(chan struct{})
	m.generation++
	generation := m.generation
	m.udpConn = udpConn
	m.running = true
	m.stopping = false
	m.done = done
	m.session = session
	m.hasSession = false
	m.boundLocalPort = boundPort
	m.cancel = cancel
	m.mu.Unlock()

	m.logs.Add(state.LogInfo, state.SourceSnowflake, fmt.Sprintf("snowflake started, listening on 127.0.0.1:%d", boundPort))

	go m.run(runCtx, generation, udpConn, sfTransport)

	return nil
}

// run performs the (potentially long) WebRTC rendezvous, then bridges UDP
// datagrams over the resulting stream until either side fails or Stop
// cancels ctx. Always runs to completion on its own goroutine — Start
// returns as soon as the loopback listener is up, matching cloak/naive's
// "session comes up async, WaitForSession blocks for it" shape.
func (m *Manager) run(ctx context.Context, generation uint64, udpConn *net.UDPConn, sfTransport snowflakeTransport) {
	defer m.cleanup(generation)

	stream, err := dialWithCancel(ctx, sfTransport)
	if err != nil {
		m.mu.RLock()
		stopping := m.stopping
		m.mu.RUnlock()
		if !stopping {
			m.logs.Add(state.LogError, state.SourceSnowflake, fmt.Sprintf("snowflake dial failed: %v", err))
		}
		udpConn.Close()
		return
	}

	m.mu.Lock()
	if m.generation != generation {
		m.mu.Unlock()
		stream.Close()
		udpConn.Close()
		return
	}
	m.stream = stream
	m.mu.Unlock()

	m.markSessionEstablished()
	m.logs.Add(state.LogInfo, state.SourceSnowflake, "snowflake stream established")

	var peer atomic.Pointer[net.UDPAddr]
	var wg sync.WaitGroup
	wg.Add(2)
	errCh := make(chan error, 2)
	go func() {
		defer wg.Done()
		errCh <- pumpUDPToStream(udpConn, stream, &peer)
	}()
	go func() {
		defer wg.Done()
		errCh <- pumpStreamToUDP(stream, udpConn, &peer)
	}()

	bridgeErr := <-errCh
	udpConn.Close()
	stream.Close()
	wg.Wait()

	m.mu.RLock()
	stopping := m.stopping
	m.mu.RUnlock()
	if bridgeErr != nil && !stopping {
		m.logs.Add(state.LogWarn, state.SourceSnowflake, fmt.Sprintf("snowflake bridge stopped: %v", bridgeErr))
	} else {
		m.logs.Add(state.LogInfo, state.SourceSnowflake, "snowflake stopped")
	}
}

// dialResult carries a Dial() outcome across the goroutine boundary in
// dialWithCancel.
type dialResult struct {
	conn net.Conn
	err  error
}

// dialWithCancel races sfTransport.Dial() (which has no cancellation hook
// of its own, and can block for tens of seconds during WebRTC rendezvous)
// against ctx. On cancellation it lets Dial finish in the background and
// closes whatever it eventually returns, so a Stop during rendezvous
// doesn't leak the WebRTC session.
func dialWithCancel(ctx context.Context, sfTransport snowflakeTransport) (net.Conn, error) {
	resultCh := make(chan dialResult, 1)
	go func() {
		conn, err := sfTransport.Dial()
		resultCh <- dialResult{conn: conn, err: err}
	}()

	select {
	case res := <-resultCh:
		return res.conn, res.err
	case <-ctx.Done():
		go func() {
			res := <-resultCh
			if res.conn != nil {
				res.conn.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

// pumpUDPToStream reads datagrams from udpConn (WireGuard's outbound
// packets), remembers the sender's address for the reverse direction, and
// frames each one onto the snowflake stream.
func pumpUDPToStream(udpConn *net.UDPConn, stream net.Conn, peer *atomic.Pointer[net.UDPAddr]) error {
	buf := make([]byte, 65535)
	for {
		n, addr, err := udpConn.ReadFromUDP(buf)
		if err != nil {
			return fmt.Errorf("read udp: %w", err)
		}
		peer.Store(addr)
		if err := WriteFrame(stream, buf[:n]); err != nil {
			return fmt.Errorf("write frame: %w", err)
		}
	}
}

// pumpStreamToUDP reads frames from the snowflake stream and writes each
// as a UDP datagram back to the last known WireGuard peer address. Frames
// that arrive before any WireGuard packet has been seen (so peer is still
// unset) are dropped.
func pumpStreamToUDP(stream net.Conn, udpConn *net.UDPConn, peer *atomic.Pointer[net.UDPAddr]) error {
	for {
		payload, err := ReadFrame(stream)
		if err != nil {
			return fmt.Errorf("read frame: %w", err)
		}
		addr := peer.Load()
		if addr == nil {
			continue
		}
		if _, err := udpConn.WriteToUDP(payload, addr); err != nil {
			return fmt.Errorf("write udp: %w", err)
		}
	}
}

// cleanup drops shared state to a stopped configuration, but only if this
// call's generation still matches the current one — a stale run()
// goroutine from a previous Start must not clobber a fresher Start's state.
func (m *Manager) cleanup(generation uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generation != generation {
		return
	}
	done := m.done
	m.running = false
	m.stopping = false
	m.udpConn = nil
	m.stream = nil
	m.done = nil
	m.session = nil
	m.boundLocalPort = 0
	m.cancel = nil
	if done != nil {
		close(done)
	}
}

func (m *Manager) markSessionEstablished() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.hasSession {
		return
	}
	m.hasSession = true
	if m.session != nil {
		close(m.session)
		m.session = nil
	}
}

// WaitForSession blocks until the WebRTC rendezvous + stream setup
// completes or timeout elapses. Snowflake rendezvous (broker polling, ICE
// gathering, DTLS/SCTP handshake) can take noticeably longer than Cloak's
// TLS handshake — callers should size timeout accordingly.
func (m *Manager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	m.mu.RLock()
	if !m.running {
		m.mu.RUnlock()
		return errors.New("snowflake is not running")
	}
	if m.hasSession {
		m.mu.RUnlock()
		return nil
	}
	session := m.session
	m.mu.RUnlock()

	if session == nil {
		return errors.New("snowflake session waiter unavailable")
	}

	if timeout <= 0 {
		timeout = 5 * time.Second
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-session:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("snowflake session not established within %s", timeout)
	}
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}

	m.stopping = true
	udpConn := m.udpConn
	stream := m.stream
	done := m.done
	cancel := m.cancel
	m.mu.Unlock()

	// Cancel first so a rendezvous still in flight (dialWithCancel) unblocks
	// immediately, then close the stream and UDP socket to kick the bridge
	// pumps out of their blocking reads.
	if cancel != nil {
		cancel()
	}
	if stream != nil {
		stream.Close()
	}
	if udpConn != nil {
		udpConn.Close()
	}

	if done == nil {
		return nil
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		m.forceReset()
		m.logs.Add(state.LogWarn, state.SourceSnowflake, "snowflake stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
		m.forceReset()
		return nil
	}
}

// forceReset mirrors cloak's forceResetStateLocked: drops shared state to
// stopped even if run() hasn't finished, bumping generation so a
// still-running goroutine's later cleanup call is a no-op.
func (m *Manager) forceReset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.stopping = false
	m.udpConn = nil
	m.stream = nil
	m.done = nil
	m.session = nil
	m.boundLocalPort = 0
	m.cancel = nil
	m.generation++
}

func (m *Manager) Status() state.TransportStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return state.TransportStatus{Running: m.running}
}

// BoundLocalPort reports the loopback UDP port the manager is currently
// bound to, or 0 when not running. Callers that requested dynamic
// allocation (LocalPort=0) use this to discover the kernel-assigned port.
func (m *Manager) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return 0
	}
	return m.boundLocalPort
}
