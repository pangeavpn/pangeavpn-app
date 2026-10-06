package api

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"
	"sync"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reach"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// reachVerdict is what the hub probes say about the host's own network.
type reachVerdict int

const (
	reachUnknown reachVerdict = iota
	reachOnline
	reachOffline
)

const (
	reachRouteDirect      = "direct"
	reachRouteReality     = "reality"
	reachRouteShadowsocks = "shadowsocks"
)

// hubProxyDialer lets the probe borrow a running hub proxy's credentials;
// reality.ProxyManager and shadowsocks.ProxyManager fit.
type hubProxyDialer interface {
	HubRemote() string
	DialHub(ctx context.Context, iface, host string, port int) (net.Conn, error)
}

// reachRoute is one way out to the hub: its kind, the IP it leaves toward, and
// how to dial it from the physical interface.
type reachRoute struct {
	kind   string
	remote string
	dial   reach.DialFunc
}

func (r reachRoute) id() string { return r.kind + ":" + r.remote }

func probeReachRoute(ctx context.Context, route reachRoute) reach.Outcome {
	return reach.Probe(ctx, route.dial)
}

// reachRoutes is every way to ask the hub from outside the tunnel: the hub IPs the
// profile bypasses, plus each running hub proxy whose node the lock lets out.
func (s *Service) reachRoutes(profile state.Profile, iface string) []reachRoute {
	routes := make([]reachRoute, 0, 3)
	if s.directHubAllowed(profile) {
		for _, ip := range ipLiterals(profile.WireGuard.BypassHosts) {
			routes = append(routes, reachRoute{kind: reachRouteDirect, remote: ip, dial: reach.DirectDialer(ip, iface)})
		}
	}
	if s.realityProxy != nil {
		routes = s.appendProxyRoute(routes, reachRouteReality, s.realityProxy, iface)
	}
	if s.shadowsocksProxy != nil {
		routes = s.appendProxyRoute(routes, reachRouteShadowsocks, s.shadowsocksProxy, iface)
	}
	return routes
}

// directHubAllowed keeps the probe off the hub IP when the user routes the hub
// through the tunnel, or the app reaches it only via a hub proxy.
func (s *Service) directHubAllowed(profile state.Profile) bool {
	if profile.WireGuard.HubInTunnel {
		return false
	}
	proxyRunning := func(proxy hubProxyDialer) bool { return proxy.HubRemote() != "" }
	if s.realityProxy != nil && proxyRunning(s.realityProxy) {
		return false
	}
	return s.shadowsocksProxy == nil || !proxyRunning(s.shadowsocksProxy)
}

// appendProxyRoute skips a node given by name (resolving it would need the dead
// tunnel's DNS) and, under the lock, any node no stored profile vouches for.
func (s *Service) appendProxyRoute(routes []reachRoute, kind string, proxy hubProxyDialer, iface string) []reachRoute {
	remotes := ipLiterals([]string{proxy.HubRemote()})
	if len(remotes) == 0 {
		return routes
	}
	remote := remotes[0]
	if s.killSwitch.Active() && !s.vouchedHosts()[remote] {
		return routes
	}
	dial := func(ctx context.Context) (net.Conn, error) {
		return proxy.DialHub(ctx, iface, reach.HubHost, reach.HubPort)
	}
	return append(routes, reachRoute{kind: kind, remote: remote, dial: dial})
}

// sessionNodeIPs is every address the session's own transports use; a hub route
// through one of them shares the dead tunnel's fate, so its silence proves nothing.
func sessionNodeIPs(profile state.Profile) []string {
	return ipLiterals(append([]string{profile.Cloak.RemoteHost}, transportPermitHosts(profile)...))
}

// reachVerdictFor probes every route at once. Any echo means the network works;
// it is offline only if each proven route off the session's own node went silent.
func (s *Service) reachVerdictFor(ctx context.Context, profile state.Profile) (reachVerdict, string) {
	if s.reachProbe == nil || s.physicalRoute == nil {
		return reachUnknown, "no probe"
	}
	iface, _, err := s.physicalRoute()
	if err != nil || iface == "" {
		return reachUnknown, "no physical route"
	}
	routes := s.reachRoutes(profile, iface)
	if len(routes) == 0 {
		return reachUnknown, "no route to the hub"
	}
	outcomes := s.probeReachRoutes(ctx, routes)
	network := s.currentNetworkKey()
	nodes := sessionNodeIPs(profile)
	answered, refused, decisive := false, false, 0
	parts := make([]string, 0, len(routes))
	for i, route := range routes {
		parts = append(parts, fmt.Sprintf("%s=%s", route.id(), outcomes[i]))
		switch outcomes[i] {
		case reach.Answered:
			answered = true
			s.reachBaseline.record(network, route.id())
		case reach.Refused:
			refused = true
		default:
			if !slices.Contains(nodes, route.remote) && s.reachBaseline.proven(network, route.id()) {
				decisive++
			}
		}
	}
	summary := strings.Join(parts, ", ")
	switch {
	case answered:
		return reachOnline, summary
	case refused || decisive == 0:
		return reachUnknown, summary
	default:
		return reachOffline, summary
	}
}

func (s *Service) probeReachRoutes(ctx context.Context, routes []reachRoute) []reach.Outcome {
	outcomes := make([]reach.Outcome, len(routes))
	var wg sync.WaitGroup
	for i, route := range routes {
		wg.Go(func() { outcomes[i] = s.reachProbe(ctx, route) })
	}
	wg.Wait()
	return outcomes
}
