package hysteria2

import (
	"context"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxService "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	dnslocal "github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/protocol/direct"
	sbhysteria2 "github.com/sagernet/sing-box/protocol/hysteria2"
	"github.com/sagernet/sing-box/protocol/mixed"
)

// inboundRegistry registers only what the client-side box needs: a local
// mixed (SOCKS+HTTP) inbound, deliberately narrower than sing-box's "include" package.
func inboundRegistry() *inbound.Registry {
	r := inbound.NewRegistry()
	mixed.RegisterInbound(r)
	sbhysteria2.RegisterInbound(r)
	return r
}

// outboundRegistry registers direct (used by the test server harness to
// reach its final destination) and hysteria2 (the actual DPI-evasion leg).
func outboundRegistry() *outbound.Registry {
	r := outbound.NewRegistry()
	direct.RegisterOutbound(r)
	sbhysteria2.RegisterOutbound(r)
	return r
}

// dnsTransportRegistry needs at least the "local" transport registered — sing-box's
// DNS router falls back to it and panics-via-error if it's missing.
func dnsTransportRegistry() *dns.TransportRegistry {
	r := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(r)
	return r
}

// newBoxContext builds a context carrying the minimal registries above, the
// prerequisite box.New expects for constructing inbounds/outbounds/DNS.
func newBoxContext(ctx context.Context) context.Context {
	return box.Context(ctx, inboundRegistry(), outboundRegistry(), endpoint.NewRegistry(), dnsTransportRegistry(), boxService.NewRegistry(), certificate.NewRegistry())
}
