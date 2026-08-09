// Package shadowsocks embeds sing-box to carry WireGuard UDP over a
// Shadowsocks AEAD/SS-2022 outbound, same in-process shape as internal/reality.
package shadowsocks

import (
	"context"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	dnslocal "github.com/sagernet/sing-box/dns/transport/local"
	sbshadowsocks "github.com/sagernet/sing-box/protocol/shadowsocks"
)

// registryContext wires only the Shadowsocks outbound. The "local" DNS
// transport is mandatory: box.New always wires it as the DNS fallback.
func registryContext(ctx context.Context) context.Context {
	return box.Context(ctx, inbound.NewRegistry(), newOutboundRegistry(), endpoint.NewRegistry(), newDNSRegistry(), boxservice.NewRegistry())
}

func newOutboundRegistry() *outbound.Registry {
	r := outbound.NewRegistry()
	sbshadowsocks.RegisterOutbound(r)
	return r
}

func newDNSRegistry() *dns.TransportRegistry {
	r := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(r)
	return r
}
