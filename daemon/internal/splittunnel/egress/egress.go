// Package egress opens the off-tunnel sockets that carry traffic of excluded apps.
package egress

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"slices"
	"syscall"
)

// Identity names the physical interface excluded traffic leaves by.
type Identity struct {
	Index int
	Name  string
	Addrs []netip.Addr
}

func (a Identity) Equal(b Identity) bool {
	return a.Index == b.Index && a.Name == b.Name && slices.Equal(a.Addrs, b.Addrs)
}

type Dialer interface {
	DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error)
	ListenUDP(ctx context.Context) (net.PacketConn, error)
	Repin(c syscall.Conn) error
	Current() (Identity, error)
	Refresh() (old, cur Identity, changed bool, err error)
	Close() error
}

type Options struct {
	BrokerGID int
	Logf      func(format string, args ...any)
}

var (
	ErrUnsupported = errors.New("split tunnelling egress is not supported on this OS")
	ErrNoInterface = errors.New("no physical interface")
)

// SplitMark is the Linux SO_MARK on bypass sockets: it carries every bit of the
// tunnel fwmark 0xca6c so policy routing skips the tunnel table.
const SplitMark = 0x1ca6c
