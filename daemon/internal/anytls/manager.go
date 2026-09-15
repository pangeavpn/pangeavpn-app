package anytls

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

var (
	_ transport.Manager           = (*Manager)(nil)
	_ transport.SessionWaiter     = (*Manager)(nil)
	_ transport.BoundPortReporter = (*Manager)(nil)
)

// dialTimeout caps the TLS handshake plus session open that Start performs,
// independently of a caller whose ctx is context.Background().
const dialTimeout = 10 * time.Second

// Manager owns a loopback UDP listener WireGuard's peer Endpoint points at,
// bridged to a single in-process AnyTLS outbound over UDP-over-TCP.
type Manager struct {
	// startMu serializes Start/Stop end to end so two callers can't both pass
	// the running check and race to bind the same local port.
	startMu sync.Mutex

	mu      sync.RWMutex
	logs    *state.LogStore
	running bool
	profile state.AnyTLSProfile

	engine    *box.Box
	localConn *net.UDPConn
	remote    net.PacketConn
	cancel    context.CancelFunc

	// boundLocalPort is the port actually bound, which differs from
	// profile.LocalPort when the caller asked for dynamic allocation.
	boundLocalPort int

	done chan struct{}

	// generation bumps every Start so a bridge goroutine from a previous run
	// cannot clobber the state of a fresh one.
	generation uint64
}

func NewManager(logs *state.LogStore) *Manager {
	return &Manager{logs: logs}
}

// Start builds a single-outbound engine, dials the AnyTLS session (TLS
// handshake plus the UDP-over-TCP stream) synchronously, and wires a local
// UDP listener to it. A node that is unreachable or presents the wrong
// certificate fails here; a wrong password does not, since the server closes
// the session silently and the first datagram is what discovers it.
func (m *Manager) Start(ctx context.Context, profile state.AnyTLSProfile) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.RLock()
	running, current := m.running, m.profile
	m.mu.RUnlock()

	if running {
		if current == profile {
			return nil
		}
		// A different profile while running is a server/credential switch,
		// not a no-op: tear down the old session before building a new one.
		if err := m.Stop(ctx); err != nil {
			return fmt.Errorf("anytls: stop previous session: %w", err)
		}
	}

	if err := validateProfile(profile); err != nil {
		return err
	}

	targetHost := targetHostOrDefault(profile.TargetHost)
	targetPort := targetPortOrDefault(profile.TargetPort)

	outboundOptions, err := buildOutboundOptions(profile)
	if err != nil {
		return err
	}

	// engineCtx is rooted independently of ctx: the engine and bridge outlive
	// this call, which is typically request-scoped.
	engineCtx, cancel := context.WithCancel(context.Background())

	engine, err := box.New(box.Options{
		Context: registryContext(engineCtx),
		Options: option.Options{
			Log: &option.LogOptions{Level: "warn"},
			Outbounds: []option.Outbound{{
				Type:    C.TypeAnyTLS,
				Tag:     outboundTag,
				Options: outboundOptions,
			}},
		},
	})
	if err != nil {
		cancel()
		return fmt.Errorf("anytls: build engine: %w", err)
	}
	if err := engine.Start(); err != nil {
		engine.Close()
		cancel()
		return fmt.Errorf("anytls: start engine: %w", err)
	}

	outbound, loaded := engine.Outbound().Outbound(outboundTag)
	if !loaded {
		engine.Close()
		cancel()
		return errors.New("anytls: outbound not registered")
	}

	// The engine outlives Start, so only the dial follows the caller's ctx
	// (an aborted connect gives up promptly), with a hard cap in case ctx is
	// context.Background().
	destination := M.ParseSocksaddrHostPort(targetHost, uint16(targetPort))
	dialCtx, dialCancel := context.WithTimeout(engineCtx, dialTimeout)
	stopPropagation := context.AfterFunc(ctx, dialCancel)
	remote, err := outbound.ListenPacket(dialCtx, destination)
	stopPropagation()
	dialCancel()
	if err != nil {
		engine.Close()
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("anytls: handshake: %w", ctxErr)
		}
		return fmt.Errorf("anytls: handshake: %w", annotateHandshakeError(err))
	}

	localAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(profile.LocalPort)))
	if err != nil {
		remote.Close()
		engine.Close()
		cancel()
		return fmt.Errorf("anytls: resolve local addr: %w", err)
	}
	localConn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		remote.Close()
		engine.Close()
		cancel()
		return fmt.Errorf("anytls: listen local udp: %w", err)
	}

	boundPort := localAddr.Port
	if laddr, ok := localConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}

	done := make(chan struct{})

	m.mu.Lock()
	m.generation++
	generation := m.generation
	m.engine = engine
	m.localConn = localConn
	m.remote = remote
	m.cancel = cancel
	m.boundLocalPort = boundPort
	m.running = true
	m.profile = profile
	m.done = done
	m.mu.Unlock()

	m.logs.Add(state.LogInfo, state.SourceAnyTLS, fmt.Sprintf(
		"anytls started, listening on 127.0.0.1:%d, relaying to %s:%d (target %s:%d)",
		boundPort, profile.RemoteHost, profile.RemotePort, targetHost, targetPort))

	remoteAddr := destinationUDPAddr(targetHost, targetPort)
	go func() {
		bridgeErr := transport.BridgeUDP(engineCtx, localConn, remote, remoteAddr)
		cancel()
		localConn.Close()
		remote.Close()

		m.mu.Lock()
		if m.generation == generation {
			m.running = false
			m.engine = nil
			m.localConn = nil
			m.remote = nil
			m.cancel = nil
			m.boundLocalPort = 0
			m.done = nil
		}
		m.mu.Unlock()

		if bridgeErr != nil {
			m.logs.Add(state.LogWarn, state.SourceAnyTLS, fmt.Sprintf("anytls bridge exited: %v", bridgeErr))
		} else {
			m.logs.Add(state.LogInfo, state.SourceAnyTLS, "anytls stopped")
		}
		engine.Close()
		close(done)
	}()

	return nil
}

// destinationUDPAddr resolves the relay target for transport.BridgeUDP's
// WriteTo. A non-literal host is passed through unresolved as a hostname
// Socksaddr: the node resolves it, and a lookup here would leak under Lockdown.
func destinationUDPAddr(host string, port int) net.Addr {
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}
	}
	return M.ParseSocksaddrHostPort(host, uint16(port))
}

// annotateHandshakeError names the likely cause of the opaque EOF the dial
// returns when the node accepts TCP but drops the session before answering.
func annotateHandshakeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "EOF") {
		return fmt.Errorf("%w (node closed the AnyTLS session before answering; verify the node's password, certificate pin and server name match the provisioned profile)", err)
	}
	return err
}

// WaitForSession reports whether the TLS session Start dialed is still up.
// The handshake itself already completed inside Start, so this never blocks.
func (m *Manager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	_ = ctx
	_ = timeout
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return errors.New("anytls is not running")
	}
	return nil
}

func (m *Manager) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return 0
	}
	return m.boundLocalPort
}

func (m *Manager) Status() state.TransportStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return state.TransportStatus{}
	}
	pid := os.Getpid()
	return state.TransportStatus{Running: true, PID: &pid}
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	localConn := m.localConn
	remote := m.remote
	cancel := m.cancel
	done := m.done
	m.mu.Unlock()

	// Cancel first so an in-flight dial unblocks, then close both sockets to
	// kick the bridge goroutine's blocked reads. Order mirrors shadowsocks.Stop.
	if cancel != nil {
		cancel()
	}
	if localConn != nil {
		localConn.Close()
	}
	if remote != nil {
		remote.Close()
	}

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()

	select {
	case <-done:
		return nil
	case <-timer.C:
		m.forceResetState()
		m.logs.Add(state.LogWarn, state.SourceAnyTLS, "anytls stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
		m.forceResetState()
		m.logs.Add(state.LogWarn, state.SourceAnyTLS, "anytls stop cancelled; forced shutdown")
		return ctx.Err()
	}
}

// forceResetState drops shared state to stopped and closes the engine itself,
// so a subsequent Start does not race a still-live AnyTLS session.
func (m *Manager) forceResetState() {
	m.mu.Lock()
	engine := m.engine
	m.running = false
	m.engine = nil
	m.localConn = nil
	m.remote = nil
	m.cancel = nil
	m.boundLocalPort = 0
	m.done = nil
	m.profile = state.AnyTLSProfile{}
	m.generation++
	m.mu.Unlock()

	if engine != nil {
		engine.Close()
	}
}
