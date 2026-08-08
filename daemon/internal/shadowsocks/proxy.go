package shadowsocks

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/mixed"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const proxyInboundTag = "shadowsocks-proxy-in"

// ProxyManager carries hub traffic over Shadowsocks: a loopback mixed inbound
// answering HTTP CONNECT, no WireGuard bridge. Runs before any profile exists.
type ProxyManager struct {
	mu      sync.Mutex
	logs    *state.LogStore
	running bool

	engine *box.Box
	cancel context.CancelFunc
	port   int
}

func NewProxyManager(logs *state.LogStore) *ProxyManager {
	return &ProxyManager{logs: logs}
}

// Start brings up the proxy and returns its loopback port. Starting an already
// running proxy returns the live port rather than rebinding.
func (p *ProxyManager) Start(ctx context.Context, profile state.ShadowsocksProfile) (int, error) {
	_ = ctx

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.running {
		return p.port, nil
	}
	if err := validateProfile(profile); err != nil {
		return 0, err
	}

	port, err := pickFreeLoopbackTCPPort()
	if err != nil {
		return 0, fmt.Errorf("shadowsocks proxy: pick port: %w", err)
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
		cancel()
		engine.Close()
		return 0, fmt.Errorf("shadowsocks proxy: start engine: %w", err)
	}

	p.engine = engine
	p.cancel = cancel
	p.port = port
	p.running = true

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
	p.running = false
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
