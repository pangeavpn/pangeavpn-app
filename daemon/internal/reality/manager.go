// Package reality embeds sing-box as a Go library to provide a VLESS+REALITY
// DPI-evasion transport: a VLESS outbound wrapped in REALITY, dialed
// in-process (no local SOCKS/mixed inbound — bridge.go relays UDP directly
// against the outbound's packet connection, mirroring cloak's in-process
// UDP-loopback-listener shape rather than naive's external-process one).
//
// REALITY requires sing-box's with_utls build tag (uTLS is not compiled in
// by default); without it, Start returns a clear "rebuild with -tags
// with_utls" error at the TLS layer rather than failing to build. See
// manager_test.go / e2e_test.go for the exact tags this package needs.
package reality

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	dnslocal "github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/vless"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// registryContext wires a minimal sing-box protocol registry into ctx: just
// the VLESS outbound this package needs, rather than sing-box/include's
// register-everything convenience (which pulls in shadowsocks, tor, anytls,
// etc. — unnecessary weight for a single-outbound in-process engine). The
// "local" DNS transport is required unconditionally: box.New always wires it
// as the DNS transport manager's fallback, even when nothing ever queries it.
func registryContext(ctx context.Context) context.Context {
	outboundRegistry := outbound.NewRegistry()
	vless.RegisterOutbound(outboundRegistry)
	dnsRegistry := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(dnsRegistry)
	return box.Context(ctx, inbound.NewRegistry(), outboundRegistry, endpoint.NewRegistry(), dnsRegistry, boxservice.NewRegistry())
}

// defaultTargetPort is where the remote node's sing-box VLESS+REALITY server
// forwards decoded UDP when RealityProfile.TargetPort is unset: the node's
// own local WireGuard listener, the same "server always forwards to its own
// loopback WireGuard" convention cloak/naive's server-side counterparts use.
const defaultTargetPort = 51820

const outboundTag = "reality-out"

// utlsFingerprint is the uTLS ClientHello fingerprint REALITY presents.
// "chrome" is the common default across VLESS+REALITY clients.
const utlsFingerprint = "chrome"

var _ transport.Manager = (*Manager)(nil)

// Manager satisfies transport.Manager (+ SessionWaiter, BoundPortReporter),
// mirroring cloak.inProcessManager's field shape: a loopback UDP listener
// WireGuard's peer Endpoint points at, owned entirely on the Go side.
type Manager struct {
	mu      sync.RWMutex
	logs    *state.LogStore
	running bool

	engine    *box.Box
	localConn *net.UDPConn
	remote    net.PacketConn
	cancel    context.CancelFunc

	// boundLocalPort is the actual loopback UDP port bound. Differs from
	// profile.LocalPort when the caller requested dynamic allocation
	// (LocalPort=0). Zero when not running.
	boundLocalPort int

	done       chan struct{}
	session    chan struct{}
	hasSession bool

	// generation bumps every Start; the bridge goroutine's cleanup only
	// clobbers shared state if its generation still matches the current
	// one, same guard cloak uses against a zombie goroutine from a
	// previous Start.
	generation uint64
}

func NewManager(logs *state.LogStore) *Manager {
	return &Manager{logs: logs}
}

// Start builds a single-outbound sing-box engine (VLESS+REALITY), performs
// the REALITY handshake synchronously (via the first ListenPacket call), and
// wires a local UDP loopback listener to it. Returning nil means the
// handshake already succeeded — see WaitForSession.
func (m *Manager) Start(ctx context.Context, profile state.RealityProfile) error {
	_ = ctx

	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return nil
	}

	remoteHost := strings.TrimSpace(profile.RemoteHost)
	if remoteHost == "" {
		m.mu.Unlock()
		return errors.New("reality remote host is required")
	}
	if strings.TrimSpace(profile.UUID) == "" {
		m.mu.Unlock()
		return errors.New("reality uuid is required")
	}
	if strings.TrimSpace(profile.PublicKey) == "" {
		m.mu.Unlock()
		return errors.New("reality public key is required")
	}
	if profile.LocalPort < 0 {
		m.mu.Unlock()
		return fmt.Errorf("LocalPort must be >= 0, got %d", profile.LocalPort)
	}

	remotePort := profile.RemotePort
	if remotePort <= 0 {
		remotePort = 443
	}
	targetPort := profile.TargetPort
	if targetPort <= 0 {
		targetPort = defaultTargetPort
	}
	serverName := strings.TrimSpace(profile.ServerName)
	if serverName == "" {
		serverName = remoteHost
	}

	engineCtx, cancel := context.WithCancel(context.Background())

	engine, err := box.New(box.Options{
		Context: registryContext(engineCtx),
		Options: option.Options{
			Log: &option.LogOptions{Level: "warn"},
			Outbounds: []option.Outbound{{
				Type:    C.TypeVLESS,
				Tag:     outboundTag,
				Options: buildOutboundOptions(profile, remoteHost, remotePort, serverName),
			}},
		},
	})
	if err != nil {
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("reality: build engine: %w", err)
	}
	if err := engine.Start(); err != nil {
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("reality: start engine: %w", err)
	}

	outbound, loaded := engine.Outbound().Outbound(outboundTag)
	if !loaded {
		engine.Close()
		cancel()
		m.mu.Unlock()
		return errors.New("reality: outbound not registered")
	}

	destination := M.ParseSocksaddrHostPort("127.0.0.1", uint16(targetPort))
	dialCtx, dialCancel := context.WithTimeout(engineCtx, 10*time.Second)
	remote, err := outbound.ListenPacket(dialCtx, destination)
	dialCancel()
	if err != nil {
		engine.Close()
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("reality: handshake: %w", err)
	}

	localAddr, err := net.ResolveUDPAddr("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(profile.LocalPort)))
	if err != nil {
		remote.Close()
		engine.Close()
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("reality: resolve local addr: %w", err)
	}
	localConn, err := net.ListenUDP("udp", localAddr)
	if err != nil {
		remote.Close()
		engine.Close()
		cancel()
		m.mu.Unlock()
		return fmt.Errorf("reality: listen local udp: %w", err)
	}

	boundPort := localAddr.Port
	if laddr, ok := localConn.LocalAddr().(*net.UDPAddr); ok {
		boundPort = laddr.Port
	}

	done := make(chan struct{})
	session := make(chan struct{})
	close(session) // ListenPacket above already completed the REALITY handshake

	m.generation++
	generation := m.generation
	m.engine = engine
	m.localConn = localConn
	m.remote = remote
	m.cancel = cancel
	m.boundLocalPort = boundPort
	m.running = true
	m.done = done
	m.session = session
	m.hasSession = true
	m.mu.Unlock()

	m.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf(
		"reality started, listening on 127.0.0.1:%d, relaying to %s:%d (target 127.0.0.1:%d)",
		boundPort, remoteHost, remotePort, targetPort))

	remoteAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: targetPort}
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
			m.session = nil
			m.hasSession = false
		}
		m.mu.Unlock()

		if bridgeErr != nil {
			m.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("reality bridge exited: %v", bridgeErr))
		} else {
			m.logs.Add(state.LogInfo, state.SourceDaemon, "reality stopped")
		}
		engine.Close()
		close(done)
	}()

	return nil
}

// buildOutboundOptions constructs the VLESS+REALITY outbound sing-box's
// registry expects: option.Outbound.Options must be a pointer matching the
// type registered for the outbound's Type (protocol/vless.RegisterOutbound
// registers option.VLESSOutboundOptions).
func buildOutboundOptions(profile state.RealityProfile, remoteHost string, remotePort int, serverName string) *option.VLESSOutboundOptions {
	return &option.VLESSOutboundOptions{
		ServerOptions: option.ServerOptions{Server: remoteHost, ServerPort: uint16(remotePort)},
		UUID:          profile.UUID,
		Flow:          profile.Flow,
		OutboundTLSOptionsContainer: option.OutboundTLSOptionsContainer{
			TLS: &option.OutboundTLSOptions{
				Enabled:    true,
				ServerName: serverName,
				UTLS:       &option.OutboundUTLSOptions{Enabled: true, Fingerprint: utlsFingerprint},
				Reality: &option.OutboundRealityOptions{
					Enabled:   true,
					PublicKey: profile.PublicKey,
					ShortID:   profile.ShortID,
				},
			},
		},
	}
}

func (m *Manager) WaitForSession(ctx context.Context, timeout time.Duration) error {
	_ = ctx
	_ = timeout
	m.mu.RLock()
	defer m.mu.RUnlock()
	if !m.running {
		return errors.New("reality is not running")
	}
	if !m.hasSession {
		return errors.New("reality session not established")
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

	// Cancel the engine context first so any in-flight dial/handshake retry
	// unblocks immediately, then close both sockets to kick the bridge
	// goroutine's blocked reads. Order mirrors cloak.Stop.
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
		m.forceResetStateLocked()
		m.logs.Add(state.LogWarn, state.SourceDaemon, "reality stop timed out; forced shutdown")
		return nil
	case <-ctx.Done():
		m.forceResetStateLocked()
		return nil
	}
}

// forceResetStateLocked drops shared state to a stopped configuration even
// if the bridge goroutine has not finished. Safe to call when Stop's wait
// has timed out; the goroutine's generation check prevents it from later
// clobbering a fresh Start.
func (m *Manager) forceResetStateLocked() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.running = false
	m.engine = nil
	m.localConn = nil
	m.remote = nil
	m.cancel = nil
	m.boundLocalPort = 0
	m.done = nil
	m.session = nil
	m.hasSession = false
	m.generation++
}
