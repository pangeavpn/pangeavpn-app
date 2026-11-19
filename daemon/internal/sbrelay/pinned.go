package sbrelay

import (
	"context"
	"errors"
	"net"

	"github.com/sagernet/sing-box/adapter"
	"github.com/sagernet/sing-box/adapter/outbound"
	"github.com/sagernet/sing-box/log"
	M "github.com/sagernet/sing/common/metadata"
	N "github.com/sagernet/sing/common/network"
)

const (
	pinnedType = "pangea-pinned"
	pinnedTag  = "relay-pinned"
)

// pinnedOptions carries a Go dialer, so it is only ever built in code, never parsed.
type pinnedOptions struct {
	Dial func(ctx context.Context, network, address string) (net.Conn, error) `json:"-"`
}

// pinnedOutbound is the edge connection's detour when the pin supplies its own sockets (the
// macOS egress broker), so sing-box never opens one the lock would see as the daemon's.
type pinnedOutbound struct {
	outbound.Adapter
	dial func(ctx context.Context, network, address string) (net.Conn, error)
}

func registerPinned(registry *outbound.Registry) {
	outbound.Register[pinnedOptions](registry, pinnedType, func(_ context.Context, _ adapter.Router, _ log.ContextLogger, tag string, options pinnedOptions) (adapter.Outbound, error) {
		if options.Dial == nil {
			return nil, errors.New("pinned outbound has no dialer")
		}
		return &pinnedOutbound{Adapter: outbound.NewAdapter(pinnedType, tag, []string{N.NetworkTCP}, nil), dial: options.Dial}, nil
	})
}

func (o *pinnedOutbound) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	return o.dial(ctx, network, destination.String())
}

func (o *pinnedOutbound) ListenPacket(context.Context, M.Socksaddr) (net.PacketConn, error) {
	return nil, errors.New("the pinned edge dialer carries TCP only")
}
