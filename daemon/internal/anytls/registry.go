// Package anytls embeds sing-box to carry WireGuard UDP over an AnyTLS
// session, same in-process shape as internal/shadowsocks. AnyTLS pads TLS
// record sizes against TLS-in-TLS fingerprinting and multiplexes streams over
// one connection; WireGuard's datagrams ride inside as UDP-over-TCP.
package anytls

import (
	"context"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	"github.com/sagernet/sing-box/dns"
	dnslocal "github.com/sagernet/sing-box/dns/transport/local"
	sbanytls "github.com/sagernet/sing-box/protocol/anytls"
)

// registryContext wires only the AnyTLS outbound. The "local" DNS transport
// is mandatory: box.New always wires it as the DNS fallback.
func registryContext(ctx context.Context) context.Context {
	return box.Context(ctx, inbound.NewRegistry(), newOutboundRegistry(), endpoint.NewRegistry(), newDNSRegistry(), boxservice.NewRegistry(), certificate.NewRegistry())
}

func newOutboundRegistry() *outbound.Registry {
	r := outbound.NewRegistry()
	sbanytls.RegisterOutbound(r)
	return r
}

func newDNSRegistry() *dns.TransportRegistry {
	r := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(r)
	return r
}
