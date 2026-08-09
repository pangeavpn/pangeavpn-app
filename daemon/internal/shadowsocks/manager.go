package shadowsocks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
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
	_ transport.BoundPortReporter = (*Manager)(nil)
)

// Manager owns a loopback UDP listener WireGuard's peer Endpoint points at,
// bridged to a single in-process Shadowsocks outbound.
type Manager struct {
	mu      sync.RWMutex
	logs    *state.LogStore
	running bool

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

// Start builds a single-outbound engine and wires a local UDP listener to it.
// No SessionWaiter on purpose: ListenPacket succeeds on a wrong password.
func (m *Manager) Start(ctx context.Context, profile state.ShadowsocksProfile) error {
	_ = ctx

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}
	if err := validateProfile(profile); err != nil {
		m.mu.Unlock()
		return err
	}

	targetHost := targetHostOrDefault(profile.TargetHost)
	targetPort := targetPortOrDefault(profile.TargetPort)

	engineCtx, cancel := context.WithCancel(context.Background())

	engine, err := box.New(box.Options{
		Context: registryContext(engineCtx),
		Options: option.Options{
			Log: &option.LogOptions{Level: "warn"},
			Outbounds: []option.Outbound{{
				Type:    C.TypeShadowsocks,
				Tag:     outboundTag,
				Options: buildOutboundOptions(profile),
			}},
		},
	})
	if err != nil {
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("shadowsocks: build engine: %w", err)
	}
	if err := engine.Start(); err != nil {
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("shadowsocks: start engine: %w", err)
	}

	outbound, loaded := engine.Outbound().Outbound(outboundTag)
	if !loaded {
		engine.Close()
		cancel()
		m.mu.Unlock()
		return errors.New("shadowsocks: outbound not registered")
	}

	destination := M.ParseSocksaddrHostPort(targetHost, uint16(targetPort))
	dialCtx, dialCancel := context.WithTimeout(engineCtx, 10*time.Second)
	remote, err := outbound.ListenPacket(dialCtx, destination)
	dialCancel()
	if err != nil {
		engine.Close()
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("shadowsocks: listen packet: %w", err)
	}

	localAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(profile.LocalPort)))
	if err != nil {
		remote.Close()
		engine.Close()
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("shadowsocks: resolve local addr: %w", err)
	}
	localConn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		remote.Close()
		engine.Close()
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("shadowsocks: listen local udp: %w", err)
	}

	boundPort := localAddr.Port
	if laddr, ok := localConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}

	done := make(chan struct{})

	m.generation++
	generation := m.generation
	m.engine = engine
	m.localConn = localConn
	m.remote = remote
	m.cancel = cancel
	m.boundLocalPort = boundPort
	m.running = true
	m.done = done
	m.mu.Unlock()

	m.logs.Add(state.LogInfo, state.SourceShadowsocks, fmt.Sprintf(
		"shadowsocks started, listening on 127.0.0.1:%d, relaying to %s:%d (target %s:%d)",
		boundPort, profile.RemoteHost, profile.RemotePort, targetHost, targetPort))

	remoteAddr := destinationUDPAddr(targetHost, targetPort)
	go func() {
		bridgeErr := bridgeUDP(engineCtx, localConn, remote, remoteAddr)

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
			m.logs.Add(state.LogWarn, state.SourceShadowsocks, fmt.Sprintf("shadowsocks bridge exited: %v", bridgeErr))
		} else {
			m.logs.Add(state.LogInfo, state.SourceShadowsocks, "shadowsocks stopped")
		}
		engine.Close()
		close(done)
	}()

	return nil
}

// destinationUDPAddr resolves the relay target for bridgeUDP's WriteTo. A
// non-literal host is passed through unresolved as a hostname Socksaddr.
func destinationUDPAddr(host string, port int) net.Addr {
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}
	}
	return M.ParseSocksaddrHostPort(host, uint16(port))
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
	// kick the bridge goroutine's blocked reads. Order mirrors reality.Stop.
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
		m.logs.Add(state.LogWarn, state.SourceShadowsocks, "shadowsocks stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
		m.forceResetState()
		return nil
	}
}

// forceResetState drops shared state to a stopped configuration even if the
// bridge goroutine has not finished; its generation check keeps it harmless.
func (m *Manager) forceResetState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.engine = nil
	m.localConn = nil
	m.remote = nil
	m.cancel = nil
	m.boundLocalPort = 0
	m.done = nil
	m.generation++
}
