package api

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strings"

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
	hosts := append([]string{profile.Cloak.RemoteHost}, transportPermitHosts(profile)...)
	if host, _, err := net.SplitHostPort(profile.WireGuard.DirectEndpoint); err == nil {
		hosts = append(hosts, host)
	}
	return ipLiterals(hosts)
}

// reachRound is one probe of the routes: what each finished route said, and the
// network the round ran on.
type reachRound struct {
	network  string
	routes   []reachRoute
	outcomes []reach.Outcome
	done     []bool
}

func (r reachRound) summary() string {
	parts := make([]string, 0, len(r.routes))
	for i, route := range r.routes {
		if r.done[i] {
			parts = append(parts, fmt.Sprintf("%s=%s", route.id(), r.outcomes[i]))
		}
	}
	return strings.Join(parts, ", ")
}

// probeHub asks every route at once from the physical NIC; stopOnAnswer ends the
// round at the first echo. ok is false when nothing ran or the host roamed mid-round.
func (s *Service) probeHub(ctx context.Context, profile state.Profile, stopOnAnswer bool) (round reachRound, summary string, ok bool) {
	if s.reachProbe == nil || s.physicalRoute == nil {
		return reachRound{}, "no probe", false
	}
	iface, _, err := s.physicalRoute()
	if err != nil || iface == "" {
		return reachRound{}, "no physical route", false
	}
	routes := s.reachRoutes(profile, iface)
	if len(routes) == 0 {
		return reachRound{}, "no route to the hub", false
	}
	network := s.currentNetworkKey()
	outcomes, done := s.probeReachRoutes(ctx, routes, stopOnAnswer)
	round = reachRound{network: network, routes: routes, outcomes: outcomes, done: done}
	if s.currentNetworkKey() != network {
		return round, "the network changed mid-round", false
	}
	return round, round.summary(), true
}

// reachVerdictFor asks the hub over every route. Any echo means the network works;
// it is offline only if each proven route off the session's own node went silent.
func (s *Service) reachVerdictFor(ctx context.Context, profile state.Profile) (reachVerdict, string) {
	round, summary, ok := s.probeHub(ctx, profile, true)
	if !ok {
		return reachUnknown, summary
	}
	nodes := sessionNodeIPs(profile)
	refused, decisive := false, 0
	for i, route := range round.routes {
		if !round.done[i] {
			continue
		}
		switch round.outcomes[i] {
		case reach.Answered:
			s.reachBaseline.record(round.network, route.id())
			return reachOnline, summary
		case reach.Refused:
			refused = true
		default:
			if !slices.Contains(nodes, route.remote) && s.reachBaseline.proven(round.network, route.id()) {
				decisive++
			}
		}
	}
	if refused || decisive == 0 {
		return reachUnknown, summary
	}
	return reachOffline, summary
}

// reproveBaseline runs right after a working bring-up, when the network is known
// good: a route that does not answer then is blocked, so it stops counting.
func (s *Service) reproveBaseline(ctx context.Context, profile state.Profile) {
	round, _, ok := s.probeHub(ctx, profile, false)
	if !ok {
		return
	}
	for i, route := range round.routes {
		if round.outcomes[i] == reach.Answered {
			s.reachBaseline.record(round.network, route.id())
		} else {
			s.reachBaseline.unprove(round.network, route.id())
		}
	}
}

type reachResult struct {
	index   int
	outcome reach.Outcome
}

func (s *Service) probeReachRoutes(ctx context.Context, routes []reachRoute, stopOnAnswer bool) ([]reach.Outcome, []bool) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make(chan reachResult, len(routes))
	for i, route := range routes {
		go func() { results <- reachResult{index: i, outcome: s.reachProbe(ctx, route)} }()
	}
	outcomes := make([]reach.Outcome, len(routes))
	done := make([]bool, len(routes))
	for range routes {
		result := <-results
		outcomes[result.index], done[result.index] = result.outcome, true
		if stopOnAnswer && result.outcome == reach.Answered {
			break
		}
	}
	return outcomes, done
}
