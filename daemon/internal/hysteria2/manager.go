// Package hysteria2 wraps sing-box's Hysteria2 (QUIC + Salamander
// obfuscation) client as an in-process DPI-evasion transport, mirroring
// cloak.Manager and naive.Manager's shape: a loopback UDP listener
// WireGuard's peer Endpoint points at, bridging its traffic through the
// tunnel to the real WireGuard server.
package hysteria2

import (
	"context"
	"fmt"
	"net"
	"os"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// Compile-time checks: Manager satisfies transport.Manager plus both
// optional capabilities, same as cloak's inProcessManager.
var (
	_ transport.Manager           = (*Manager)(nil)
	_ transport.SessionWaiter     = (*Manager)(nil)
	_ transport.BoundPortReporter = (*Manager)(nil)
)

type Manager struct {
	mu      sync.RWMutex
	logs    *state.LogStore
	running bool
	box     *box.Box
	bridge  *udpBridge
}

func NewManager(logs *state.LogStore) *Manager {
	return &Manager{logs: logs}
}

// Start builds a client-side sing-box instance (mixed inbound + hysteria2
// outbound) and a UDP bridge in front of it, then leaves both running.
func (m *Manager) Start(ctx context.Context, profile state.Hysteria2Profile) error {
	m.mu.RLock()
	running := m.running
	m.mu.RUnlock()
	if running {
		return nil
	}

	if err := validateProfile(profile); err != nil {
		return fmt.Errorf("hysteria2: %w", err)
	}

	mixedPort, err := pickFreeLoopbackPort()
	if err != nil {
		return fmt.Errorf("hysteria2: pick internal port: %w", err)
	}

	opts, err := buildClientOptions(profile, mixedPort)
	if err != nil {
		return fmt.Errorf("hysteria2: %w", err)
	}

	b, err := box.New(box.Options{Context: newBoxContext(context.Background()), Options: opts})
	if err != nil {
		return fmt.Errorf("hysteria2: build box: %w", err)
	}
	if err := b.Start(); err != nil {
		b.Close()
		return fmt.Errorf("hysteria2: start box: %w", err)
	}

	mixedAddr := fmt.Sprintf("127.0.0.1:%d", mixedPort)
	bridge, err := newUDPBridge(ctx, m.logs, profile.LocalPort, mixedAddr)
	if err != nil {
		b.Close()
		return fmt.Errorf("hysteria2: %w", err)
	}

	m.mu.Lock()
	m.running = true
	m.box = b
	m.bridge = bridge
	m.mu.Unlock()

	m.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("hysteria2 started (pid=%d) listening on 127.0.0.1:%d", os.Getpid(), bridge.boundPort()))
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	b := m.box
	bridge := m.bridge
	m.running = false
	m.box = nil
	m.bridge = nil
	m.mu.Unlock()

	if bridge != nil {
		bridge.Close()
	}
	if b != nil {
		if err := b.Close(); err != nil {
			return fmt.Errorf("hysteria2: stop box: %w", err)
		}
	}
	m.logs.Add(state.LogInfo, state.SourceDaemon, "hysteria2 stopped")
	return nil
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

// BoundLocalPort reports the loopback UDP port WireGuard's peer Endpoint
// should target, or 0 when not running.
func (m *Manager) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running || m.bridge == nil {
		return 0
	}
	return m.bridge.boundPort()
}

// WaitForSession forces and proves the Hysteria2 QUIC handshake and
// Salamander/password auth against the remote server by opening (then
// immediately closing) a packet session on the same outbound instance the
// bridge's traffic flows through. sing-quic's client caches the resulting
// QUIC connection and reuses it for later traffic, so this is a real
// warm-up rather than a disposable probe.
func (m *Manager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	m.mu.RLock()
	b := m.box
	running := m.running
	m.mu.RUnlock()
	if !running || b == nil {
		return fmt.Errorf("hysteria2: not running")
	}

	ob, ok := b.Outbound().Outbound(hysteria2OutboundTag)
	if !ok {
		return fmt.Errorf("hysteria2: outbound %q not found", hysteria2OutboundTag)
	}

	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	target, err := net.ResolveUDPAddr("udp", relayDestination)
	if err != nil {
		return fmt.Errorf("hysteria2: resolve relay destination: %w", err)
	}

	pc, err := ob.ListenPacket(waitCtx, M.SocksaddrFromNet(target))
	if err != nil {
		return fmt.Errorf("hysteria2: session handshake failed: %w", err)
	}
	pc.Close()
	return nil
}
