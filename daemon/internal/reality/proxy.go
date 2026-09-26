package reality

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	dnslocal "github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/mixed"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/json/badoption"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	proxyInboundTag  = "reality-proxy-in"
	proxyOutboundTag = "reality-hub-out"

	// hubFlow is what the node's hub user demands; Vision is TCP-only, which
	// suits a CONNECT proxy and is why the data-plane builder can't use it.
	hubFlow = "xtls-rprx-vision"

	proxyProbeTimeout = 10 * time.Second
)

// proxyHealthCheckInterval/proxyHealthCheckMaxFailures bound how long a dead
// listener (the engine died without a Stop) stays reported as running.
const (
	proxyHealthCheckInterval    = 5 * time.Second
	proxyHealthCheckMaxFailures = 3
)

// ProxyManager carries hub traffic over VLESS+REALITY: a loopback mixed inbound
// answering HTTP CONNECT. The node pins this user's TCP to the hub, whatever the target.
type ProxyManager struct {
	// startMu serializes Start/Stop end to end so the mutex below need not be
	// held across the blocking engine build and handshake probe.
	startMu sync.Mutex

	mu      sync.Mutex
	logs    *state.LogStore
	running bool
	profile state.RealityProfile

	engine   *box.Box
	cancel   context.CancelFunc
	port     int
	username string
	password string

	// generation bumps every Start so a stale health-check goroutine cannot
	// clobber the state of a fresh session.
	generation uint64
}

func NewProxyManager(logs *state.LogStore) *ProxyManager {
	return &ProxyManager{logs: logs}
}

// Start brings up the proxy and returns its loopback port once one REALITY
// handshake with the node has succeeded. A running identical profile keeps its port.
func (p *ProxyManager) Start(ctx context.Context, profile state.RealityProfile) (int, error) {
	p.startMu.Lock()
	defer p.startMu.Unlock()

	profile = hubProfile(profile)
	p.mu.Lock()
	running, current, port := p.running, p.profile, p.port
	p.mu.Unlock()

	if running {
		if current == profile {
			return port, nil
		}
		// A different profile means a node or credential rotation: rebind
		// rather than keep serving a possibly-revoked user.
		if err := p.Stop(ctx); err != nil {
			return 0, fmt.Errorf("reality hub proxy: stop previous session: %w", err)
		}
	}

	if err := validateHubProfile(profile); err != nil {
		return 0, err
	}
	serverName, defaultedSNI := resolveServerName(profile.ServerName)
	if defaultedSNI {
		p.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf(
			"reality hub proxy profile has no serverName; presenting cover SNI %q", serverName))
	}

	port, err := pickFreeLoopbackTCPPort()
	if err != nil {
		return 0, fmt.Errorf("reality hub proxy: pick port: %w", err)
	}
	username, password, err := randomProxyCredential()
	if err != nil {
		return 0, fmt.Errorf("reality hub proxy: generate credential: %w", err)
	}

	engineCtx, cancel := context.WithCancel(context.Background())
	engine, err := box.New(box.Options{
		Context: proxyRegistryContext(engineCtx),
		Options: option.Options{
			Log: &option.LogOptions{Level: "warn"},
			Inbounds: []option.Inbound{{
				Type: C.TypeMixed,
				Tag:  proxyInboundTag,
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     proxyLoopbackAddr(),
						ListenPort: uint16(port),
					},
					Users: []auth.User{{Username: username, Password: password}},
				},
			}},
			Outbounds: []option.Outbound{{
				Type:    C.TypeVLESS,
				Tag:     proxyOutboundTag,
				Options: buildHubOutboundOptions(profile, profile.RemoteHost, profile.RemotePort, serverName),
			}},
		},
	})
	if err != nil {
		cancel()
		return 0, fmt.Errorf("reality hub proxy: build engine: %w", err)
	}
	if err := engine.Start(); err != nil {
		engine.Close()
		cancel()
		return 0, fmt.Errorf("reality hub proxy: start engine: %w", err)
	}
	if err := probeHandshake(ctx, engineCtx, engine); err != nil {
		engine.Close()
		cancel()
		return 0, fmt.Errorf("reality hub proxy: handshake: %w", annotateHandshakeError(err))
	}

	p.mu.Lock()
	p.generation++
	generation := p.generation
	p.engine = engine
	p.cancel = cancel
	p.port = port
	p.username = username
	p.password = password
	p.profile = profile
	p.running = true
	p.mu.Unlock()

	go p.watchHealth(engineCtx, generation, port)

	p.logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf(
		"reality hub proxy listening on 127.0.0.1:%d via %s:%d", port, profile.RemoteHost, profile.RemotePort))
	return port, nil
}

func (p *ProxyManager) Stop(ctx context.Context) error {
	_ = ctx

	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return nil
	}
	if p.cancel != nil {
		p.cancel()
	}
	err := p.engine.Close()
	p.engine = nil
	p.cancel = nil
	p.port = 0
	p.username = ""
	p.password = ""
	p.profile = state.RealityProfile{}
	p.running = false
	p.generation++
	p.logs.Add(state.LogInfo, state.SourceDaemon, "reality hub proxy stopped")
	if err != nil && !errors.Is(err, net.ErrClosed) {
		return err
	}
	return nil
}

// Port is the live loopback port, or 0 when stopped.
func (p *ProxyManager) Port() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return 0
	}
	return p.port
}

// Credentials returns the Basic Auth username and password required to use
// the live proxy port; both are empty when stopped.
func (p *ProxyManager) Credentials() (string, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.username, p.password
}

// watchHealth polls the loopback listener so an inbound that dies without a
// Stop still flips running to false instead of Port() going stale forever.
func (p *ProxyManager) watchHealth(ctx context.Context, generation uint64, port int) {
	ticker := time.NewTicker(proxyHealthCheckInterval)
	defer ticker.Stop()
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	fails := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			fails = 0
			continue
		}
		if fails++; fails < proxyHealthCheckMaxFailures {
			continue
		}
		p.mu.Lock()
		if p.generation == generation {
			p.running = false
			p.engine = nil
			p.cancel = nil
			p.port = 0
			p.username = ""
			p.password = ""
		}
		p.mu.Unlock()
		return
	}
}

// probeHandshake completes one REALITY handshake so a wrong key, short ID or
// SNI fails Start. The VLESS request is never written, so the node relays nothing.
func probeHandshake(ctx, engineCtx context.Context, engine *box.Box) error {
	hubOut, loaded := engine.Outbound().Outbound(proxyOutboundTag)
	if !loaded {
		return errors.New("outbound not registered")
	}
	// The engine outlives Start, so only the probe follows the caller's context.
	dialCtx, dialCancel := context.WithTimeout(engineCtx, proxyProbeTimeout)
	defer dialCancel()
	stopPropagation := context.AfterFunc(ctx, dialCancel)
	defer stopPropagation()

	conn, err := hubOut.DialContext(dialCtx, N.NetworkTCP, M.ParseSocksaddrHostPort("127.0.0.1", 443))
	if err != nil {
		return err
	}
	conn.Close()
	return nil
}

// hubProfile keeps only the fields the hub proxy dials with, so data-plane
// fields never make an identical profile look like a rotation.
func hubProfile(profile state.RealityProfile) state.RealityProfile {
	return state.RealityProfile{
		RemoteHost: strings.TrimSpace(profile.RemoteHost),
		RemotePort: profile.RemotePort,
		UUID:       strings.TrimSpace(profile.UUID),
		PublicKey:  strings.TrimSpace(profile.PublicKey),
		ShortID:    strings.TrimSpace(profile.ShortID),
		ServerName: strings.TrimSpace(profile.ServerName),
	}
}

func validateHubProfile(profile state.RealityProfile) error {
	if profile.RemoteHost == "" {
		return errors.New("reality hub proxy remoteHost is required")
	}
	if profile.RemotePort <= 0 || profile.RemotePort > 65535 {
		return errors.New("reality hub proxy remotePort must be > 0 and <= 65535")
	}
	if profile.UUID == "" {
		return errors.New("reality hub proxy uuid is required")
	}
	if profile.PublicKey == "" {
		return errors.New("reality hub proxy publicKey is required")
	}
	return nil
}

// buildHubOutboundOptions derives the hub outbound from the data-plane one so
// the REALITY/uTLS settings stay shared; only the flow and network differ.
func buildHubOutboundOptions(profile state.RealityProfile, remoteHost string, remotePort int, serverName string) *option.VLESSOutboundOptions {
	opts := buildOutboundOptions(profile, remoteHost, remotePort, serverName)
	opts.Flow = hubFlow
	opts.Network = option.NetworkList(N.NetworkTCP)
	return opts
}

// proxyRegistryContext is registryContext plus the mixed inbound this proxy
// fronts itself with.
func proxyRegistryContext(ctx context.Context) context.Context {
	inbounds := inbound.NewRegistry()
	mixed.RegisterInbound(inbounds)
	outbounds := outbound.NewRegistry()
	vless.RegisterOutbound(outbounds)
	dnsRegistry := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(dnsRegistry)
	return box.Context(ctx, inbounds, outbounds, endpoint.NewRegistry(), dnsRegistry, boxservice.NewRegistry(), certificate.NewRegistry())
}

// randomProxyCredential returns a fresh random Basic Auth username and
// password so only this daemon's own callers can use the proxy port.
func randomProxyCredential() (string, string, error) {
	buf := make([]byte, 36)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(buf)
	return encoded[:24], encoded[24:], nil
}

func proxyLoopbackAddr() *badoption.Addr {
	addr := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	return &addr
}

// pickFreeLoopbackTCPPort grabs an OS-assigned port. The close-then-bind race
// is fine here: loopback only, and a collision surfaces from engine.Start.
func pickFreeLoopbackTCPPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}
