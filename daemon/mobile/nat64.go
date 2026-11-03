package mobile

// On an IPv6-only network an IPv4 node is reachable only at the address the
// platform synthesizes for it, so every dial of a literal goes through one.

import (
	"context"
	"net"
	"net/netip"
	"slices"
	"strings"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// dialFunc is the shape of http.Transport.DialContext.
type dialFunc func(ctx context.Context, network, address string) (net.Conn, error)

// synthesizeHost maps a public IPv4 literal through synth, keeping host unless
// the answer is an IPv6 address a socket can actually reach.
func synthesizeHost(synth func(string) string, host string) string {
	literal := strings.TrimSpace(host)
	if !nat64Eligible(literal) {
		return host
	}
	answer, err := netip.ParseAddr(strings.TrimSpace(synth(literal)))
	if err != nil || !answer.Is6() || answer.Is4In6() || answer.Zone() != "" ||
		answer.IsLoopback() || answer.IsUnspecified() || answer.IsLinkLocalUnicast() {
		return host
	}
	return answer.String()
}

// nat64Eligible leaves loopback alone, where the local proxies listen, and the
// private ranges NAT64 never translates to.
func nat64Eligible(literal string) bool {
	if !isIPv4Literal(literal) {
		return false
	}
	addr, err := netip.ParseAddr(literal)
	return err == nil && addr.IsGlobalUnicast() && !addr.IsPrivate()
}

func synthesizeAddress(synth func(string) string, address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return address
	}
	mapped := synthesizeHost(synth, host)
	if mapped == host {
		return address
	}
	return net.JoinHostPort(mapped, port)
}

// nat64Dial asks synth on every dial rather than once, so the answer follows
// the network the device is on now.
func nat64Dial(dial dialFunc, synth func(string) string) dialFunc {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		return dial(ctx, network, synthesizeAddress(synth, address))
	}
}

// synthesizedProfile returns a copy of p dialing synthesized addresses, and
// whether any changed. p is never written: the next network may need others.
func synthesizedProfile(p *state.Profile, synth func(string) string) (*state.Profile, bool) {
	if p == nil {
		return nil, false
	}
	m := &hostMapper{synth: synth, answers: map[string]string{}}
	out := *p
	out.Cloak.RemoteHost = m.host(p.Cloak.RemoteHost)
	if p.Naive != nil {
		naive := *p.Naive
		naive.RemoteHost, naive.ServerName = m.namedHost(naive.RemoteHost, naive.ServerName)
		out.Naive = &naive
	}
	if p.Reality != nil {
		reality := *p.Reality
		reality.RemoteHost = m.host(reality.RemoteHost)
		out.Reality = &reality
	}
	if p.Hysteria2 != nil {
		hysteria2 := *p.Hysteria2
		hysteria2.RemoteHost, hysteria2.ServerName = m.namedHost(hysteria2.RemoteHost, hysteria2.ServerName)
		hysteria2.RemotePorts = slices.Clone(p.Hysteria2.RemotePorts)
		out.Hysteria2 = &hysteria2
	}
	if p.Shadowsocks != nil {
		shadowsocks := *p.Shadowsocks
		shadowsocks.RemoteHost = m.host(shadowsocks.RemoteHost)
		out.Shadowsocks = &shadowsocks
	}
	if p.AnyTLS != nil {
		anytls := *p.AnyTLS
		anytls.RemoteHost, anytls.ServerName = m.namedHost(anytls.RemoteHost, anytls.ServerName)
		out.AnyTLS = &anytls
	}
	return &out, m.changed
}

// hostMapper asks the synthesizer once per address, however many transports share it.
type hostMapper struct {
	synth   func(string) string
	answers map[string]string
	changed bool
}

func (m *hostMapper) host(host string) string {
	answer, asked := m.answers[host]
	if !asked {
		answer = synthesizeHost(m.synth, host)
		m.answers[host] = answer
	}
	if answer != host {
		m.changed = true
	}
	return answer
}

// namedHost is for the transports whose empty TLS name falls back to the dialed
// host: the name becomes the original address, so SNI and verification hold.
func (m *hostMapper) namedHost(host, serverName string) (string, string) {
	mapped := m.host(host)
	if mapped != host && strings.TrimSpace(serverName) == "" {
		return mapped, strings.TrimSpace(host)
	}
	return mapped, serverName
}
