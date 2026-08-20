package shadowsocks

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/mixed"
	"github.com/sagernet/sing/common/auth"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const proxyInboundTag = "shadowsocks-proxy-in"

// proxyHealthCheckInterval/proxyHealthCheckMaxFailures bound how long a dead
// listener (the engine died without a Stop) stays reported as running.
const (
	proxyHealthCheckInterval    = 5 * time.Second
	proxyHealthCheckMaxFailures = 3
)

// ProxyManager carries hub traffic over Shadowsocks: a loopback mixed inbound
// answering HTTP CONNECT, no WireGuard bridge. Runs before any profile exists.
type ProxyManager struct {
	// startMu serializes Start/Stop end to end, mirroring Manager, so the
	// mutex below need not be held across the blocking engine build.
	startMu sync.Mutex

	mu      sync.Mutex
	logs    *state.LogStore
	running bool
	profile state.ShadowsocksProfile

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

// Start brings up the proxy and returns its loopback port. Starting an already
// running proxy returns the live port rather than rebinding.
func (p *ProxyManager) Start(ctx context.Context, profile state.ShadowsocksProfile) (int, error) {
	p.startMu.Lock()
	defer p.startMu.Unlock()

	p.mu.Lock()
	running, current, port := p.running, p.profile, p.port
	p.mu.Unlock()

	if running {
		if current == profile {
			return port, nil
		}
		// A different profile while running means a server or credential
		// rotation: keep serving the old (possibly revoked) node instead
		// would be worse than the churn of a rebind.
		if err := p.Stop(ctx); err != nil {
			return 0, fmt.Errorf("shadowsocks proxy: stop previous session: %w", err)
		}
	}

	if err := validateProfile(profile); err != nil {
		return 0, err
	}

	port, err := pickFreeLoopbackTCPPort()
	if err != nil {
		return 0, fmt.Errorf("shadowsocks proxy: pick port: %w", err)
	}
	username, password, err := randomProxyCredential()
	if err != nil {
		return 0, fmt.Errorf("shadowsocks proxy: generate credential: %w", err)
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
				Type:    C.TypeShadowsocks,
				Tag:     outboundTag,
				Options: buildOutboundOptions(profile),
			}},
		},
	})
	if err != nil {
		cancel()
		return 0, fmt.Errorf("shadowsocks proxy: build engine: %w", err)
	}
	if err := engine.Start(); err != nil {
		engine.Close()
		cancel()
		return 0, fmt.Errorf("shadowsocks proxy: start engine: %w", err)
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

	p.logs.Add(state.LogInfo, state.SourceShadowsocks, fmt.Sprintf(
		"shadowsocks hub proxy listening on 127.0.0.1:%d via %s:%d", port, profile.RemoteHost, profile.RemotePort))
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
	p.profile = state.ShadowsocksProfile{}
	p.running = false
	p.generation++
	p.logs.Add(state.LogInfo, state.SourceShadowsocks, "shadowsocks hub proxy stopped")
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

// proxyRegistryContext widens registryContext with the mixed inbound this
// proxy fronts itself with.
func proxyRegistryContext(ctx context.Context) context.Context {
	inbounds := inbound.NewRegistry()
	mixed.RegisterInbound(inbounds)
	return box.Context(ctx, inbounds, newOutboundRegistry(), endpoint.NewRegistry(), newDNSRegistry(), boxservice.NewRegistry())
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
