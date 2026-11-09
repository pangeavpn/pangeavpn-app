package sbrelay

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
	"github.com/sagernet/sing-box/protocol/tuic"
	"github.com/sagernet/sing-box/protocol/vless"
)

// registryContext wires only the TUIC and VLESS outbounds. The "local" DNS
// transport is mandatory: box.New always wires it as the DNS fallback.
func registryContext(ctx context.Context) context.Context {
	outbounds := outbound.NewRegistry()
	tuic.RegisterOutbound(outbounds)
	vless.RegisterOutbound(outbounds)
	transports := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(transports)
	return box.Context(ctx, inbound.NewRegistry(), outbounds, endpoint.NewRegistry(), transports, boxservice.NewRegistry(), certificate.NewRegistry())
}
