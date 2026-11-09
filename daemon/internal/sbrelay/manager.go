// Package sbrelay runs one in-process sing-box outbound as WireGuard's loopback
// UDP peer. TUIC and CDN are both this shape, differing only in the outbound.
package sbrelay

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

const (
	outboundTag = "relay-out"
	dialTimeout = 10 * time.Second
)

// ProtectPath, when set, is the unix socket each outbound fd is handed to.
// Mobile points it at VpnService.protect(); desktop leaves it "".
var ProtectPath string

// spec is what a transport's profile builds into: the outbound plus where it relays.
type spec struct {
	outbound   option.Outbound
	remote     string
	targetHost string
	targetPort int
	localPort  int
	annotate   func(error) error
}

type Manager[P comparable] struct {
	name   string
	source state.LogSource
	build  func(P) (spec, error)

	startMu sync.Mutex

	mu             sync.RWMutex
	logs           *state.LogStore
	running        bool
	profile        P
	engine         *box.Box
	localConn      *net.UDPConn
	remote         net.PacketConn
	cancel         context.CancelFunc
	boundLocalPort int
	done           chan struct{}
	generation     uint64
}

var (
	_ transport.Manager           = (*Manager[state.TUICProfile])(nil)
	_ transport.SessionWaiter     = (*Manager[state.TUICProfile])(nil)
	_ transport.BoundPortReporter = (*Manager[state.TUICProfile])(nil)
)

// Start dials the outbound synchronously, so an unreachable server or a bad
// certificate fails here rather than at WireGuard's first handshake.
func (m *Manager[P]) Start(ctx context.Context, profile P) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()

	m.mu.RLock()
	running, current := m.running, m.profile
	m.mu.RUnlock()
	if running && current == profile {
		return nil
	}

	sp, err := m.build(profile)
	if err != nil {
		return err
	}
	if running {
		if err := m.Stop(ctx); err != nil {
			return fmt.Errorf("%s: stop previous session: %w", m.name, err)
		}
	}

	engineCtx, cancel := context.WithCancel(context.Background())
	engine, err := box.New(box.Options{
		Context: registryContext(engineCtx),
		Options: option.Options{
			Log:       &option.LogOptions{Level: "warn"},
			Outbounds: []option.Outbound{sp.outbound},
		},
	})
	if err != nil {
		cancel()
		return fmt.Errorf("%s: build engine: %w", m.name, err)
	}
	if err := engine.Start(); err != nil {
		engine.Close()
		cancel()
		return fmt.Errorf("%s: start engine: %w", m.name, err)
	}
	outbound, loaded := engine.Outbound().Outbound(outboundTag)
	if !loaded {
		engine.Close()
		cancel()
		return fmt.Errorf("%s: outbound not registered", m.name)
	}

	destination := M.ParseSocksaddrHostPort(sp.targetHost, uint16(sp.targetPort))
	dialCtx, dialCancel := context.WithTimeout(engineCtx, dialTimeout)
	stopPropagation := context.AfterFunc(ctx, dialCancel)
	remote, err := outbound.ListenPacket(dialCtx, destination)
	stopPropagation()
	dialCancel()
	if err != nil {
		engine.Close()
		cancel()
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("%s: handshake: %w", m.name, ctxErr)
		}
		if sp.annotate != nil {
			err = sp.annotate(err)
		}
		return fmt.Errorf("%s: handshake: %w", m.name, err)
	}

	localConn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: sp.localPort})
	if err != nil {
		remote.Close()
		engine.Close()
		cancel()
		return fmt.Errorf("%s: listen local udp: %w", m.name, err)
	}
	boundPort := localConn.LocalAddr().(*net.UDPAddr).Port

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

	target := net.JoinHostPort(sp.targetHost, strconv.Itoa(sp.targetPort))
	m.logs.Add(state.LogInfo, m.source, fmt.Sprintf("%s started, listening on 127.0.0.1:%d, relaying to %s (target %s)",
		m.name, boundPort, sp.remote, target))

	go func() {
		bridgeErr := transport.BridgeUDP(engineCtx, localConn, remote, destinationAddr(sp.targetHost, sp.targetPort))
		cancel()
		localConn.Close()
		remote.Close()

		m.mu.Lock()
		if m.generation == generation {
			m.clearLocked()
		}
		m.mu.Unlock()

		if bridgeErr != nil {
			m.logs.Add(state.LogWarn, m.source, fmt.Sprintf("%s bridge exited: %v", m.name, bridgeErr))
		} else {
			m.logs.Add(state.LogInfo, m.source, m.name+" stopped")
		}
		engine.Close()
		close(done)
	}()
	return nil
}

// destinationAddr leaves a hostname target for the server to resolve; resolving
// it here would leak under Lockdown.
func destinationAddr(host string, port int) net.Addr {
	if ip := net.ParseIP(host); ip != nil {
		return &net.UDPAddr{IP: ip, Port: port}
	}
	return M.ParseSocksaddrHostPort(host, uint16(port))
}

func (m *Manager[P]) clearLocked() {
	var zero P
	m.running = false
	m.profile = zero
	m.engine = nil
	m.localConn = nil
	m.remote = nil
	m.cancel = nil
	m.boundLocalPort = 0
	m.done = nil
}

// WaitForSession never blocks: Start already completed the handshake.
func (m *Manager[P]) WaitForSession(context.Context, time.Duration) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return fmt.Errorf("%s is not running", m.name)
	}
	return nil
}

func (m *Manager[P]) BoundLocalPort() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.boundLocalPort
}

func (m *Manager[P]) Status() state.TransportStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return state.TransportStatus{}
	}
	pid := os.Getpid()
	return state.TransportStatus{Running: true, PID: &pid}
}

func (m *Manager[P]) Stop(ctx context.Context) error {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return nil
	}
	localConn, remote, cancel, done := m.localConn, m.remote, m.cancel, m.done
	m.mu.Unlock()

	cancel()
	localConn.Close()
	remote.Close()

	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		m.forceReset()
		m.logs.Add(state.LogWarn, m.source, m.name+" stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
		m.forceReset()
		m.logs.Add(state.LogWarn, m.source, m.name+" stop cancelled; forced shutdown")
		return ctx.Err()
	}
}

// forceReset bumps the generation so the stuck bridge goroutine can't clobber a later Start.
func (m *Manager[P]) forceReset() {
	m.mu.Lock()
	engine := m.engine
	m.clearLocked()
	m.generation++
	m.mu.Unlock()
	if engine != nil {
		engine.Close()
	}
}
