package reality

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"

	box "github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const hubDialTag = "reality-hub-dial"

var errHubProxyStopped = errors.New("reality hub proxy is not running")

// HubRemote is the node the running proxy reaches the hub through, or "" when
// stopped; the daemon's reachability probe needs it to pick and permit routes.
func (p *ProxyManager) HubRemote() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.running {
		return ""
	}
	return p.profile.RemoteHost
}

// DialHub opens one stream to host:port over a throwaway outbound carrying the
// live proxy's credentials, bound to iface when set so it bypasses the tunnel.
func (p *ProxyManager) DialHub(ctx context.Context, iface, host string, port int) (net.Conn, error) {
	p.mu.Lock()
	running, profile := p.running, p.profile
	p.mu.Unlock()
	if !running {
		return nil, errHubProxyStopped
	}
	serverName, _ := resolveServerName(profile.ServerName)
	outboundOpts := buildHubOutboundOptions(profile, profile.RemoteHost, profile.RemotePort, serverName)
	if iface != "" {
		outboundOpts.BindInterface = iface
	}
	engineCtx, cancel := context.WithCancel(context.Background())
	engine, err := box.New(box.Options{
		Context: registryContext(engineCtx),
		Options: option.Options{
			Log:       &option.LogOptions{Level: "warn"},
			Outbounds: []option.Outbound{{Type: C.TypeVLESS, Tag: hubDialTag, Options: outboundOpts}},
		},
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("reality hub dial: build engine: %w", err)
	}
	closeEngine := func() {
		engine.Close()
		cancel()
	}
	if err := engine.Start(); err != nil {
		closeEngine()
		return nil, fmt.Errorf("reality hub dial: start engine: %w", err)
	}
	out, loaded := engine.Outbound().Outbound(hubDialTag)
	if !loaded {
		closeEngine()
		return nil, errors.New("reality hub dial: outbound not registered")
	}
	conn, err := out.DialContext(ctx, N.NetworkTCP, M.ParseSocksaddrHostPort(host, uint16(port)))
	if err != nil {
		closeEngine()
		return nil, err
	}
	return &engineConn{Conn: conn, closeEngine: closeEngine}, nil
}

// engineConn tears its throwaway engine down along with the stream.
type engineConn struct {
	net.Conn
	once        sync.Once
	closeEngine func()
}

func (c *engineConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.closeEngine)
	return err
}
