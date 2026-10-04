package egress

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// linuxDialer marks its sockets so policy routing sends them via the main table.
type linuxDialer struct {
	ids    *identityCache
	closed atomic.Bool
}

// New fails up front when SO_MARK is refused (no CAP_NET_ADMIN), rather than on every flow.
func New(opts Options) (Dialer, error) {
	if err := probeMark(); err != nil {
		return nil, fmt.Errorf("split egress: %w", err)
	}
	return newLinuxDialer(resolvePhysical), nil
}

func probeMark() error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return os.NewSyscallError("socket", err)
	}
	defer unix.Close(fd)
	return os.NewSyscallError("setsockopt SO_MARK", setMarkNoDF(uintptr(fd)))
}

func newLinuxDialer(resolve func() (Identity, error)) *linuxDialer {
	d := &linuxDialer{ids: newIdentityCache(resolve)}
	_, _, _, _ = d.ids.refresh()
	return d
}

func RunBroker() int { return 2 }

func setMark(fd uintptr) error {
	return unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, SplitMark)
}

func setMarkNoDF(fd uintptr) error {
	if err := setMark(fd); err != nil {
		return err
	}
	return unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_MTU_DISCOVER, unix.IP_PMTUDISC_DONT)
}

func (d *linuxDialer) DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	target, err := tcpTarget(dst)
	if err != nil {
		return nil, err
	}
	if d.closed.Load() {
		return nil, net.ErrClosed
	}
	nd := net.Dialer{KeepAliveConfig: keepAliveConfig, Control: dialControl(setMark)}
	return nd.DialContext(ctx, "tcp4", target)
}

func (d *linuxDialer) ListenUDP(ctx context.Context) (net.PacketConn, error) {
	if d.closed.Load() {
		return nil, net.ErrClosed
	}
	lc := net.ListenConfig{Control: dialControl(setMarkNoDF)}
	return lc.ListenPacket(ctx, "udp4", "0.0.0.0:0")
}

func (d *linuxDialer) Repin(c syscall.Conn) error { return nil }

func (d *linuxDialer) Current() (Identity, error) { return d.ids.current() }

func (d *linuxDialer) Refresh() (old, cur Identity, changed bool, err error) {
	return d.ids.refresh()
}

func (d *linuxDialer) Close() error {
	d.closed.Store(true)
	return nil
}

func resolvePhysical() (Identity, error) {
	rib, err := syscall.NetlinkRIB(unix.RTM_GETROUTE, unix.AF_INET)
	if err != nil {
		return Identity{}, err
	}
	msgs, err := syscall.ParseNetlinkMessage(rib)
	if err != nil {
		return Identity{}, err
	}
	routes := make([]linuxRoute, 0, len(msgs))
	for i := range msgs {
		if r, ok := decodeLinuxRoute(&msgs[i]); ok {
			routes = append(routes, r)
		}
	}
	index, name, ok := selectLinuxDefault(routes, interfaceState)
	if !ok {
		return Identity{}, ErrNoInterface
	}
	return identityFor(index, name)
}

func decodeLinuxRoute(m *syscall.NetlinkMessage) (linuxRoute, bool) {
	if m.Header.Type != unix.RTM_NEWROUTE || len(m.Data) < unix.SizeofRtMsg {
		return linuxRoute{}, false
	}
	rtm := (*unix.RtMsg)(unsafe.Pointer(&m.Data[0]))
	if rtm.Family != unix.AF_INET {
		return linuxRoute{}, false
	}
	r := linuxRoute{DstLen: rtm.Dst_len, Table: uint32(rtm.Table), Type: rtm.Type}
	attrs, err := syscall.ParseNetlinkRouteAttr(m)
	if err != nil {
		return linuxRoute{}, false
	}
	for _, a := range attrs {
		switch a.Attr.Type {
		case unix.RTA_OIF:
			if len(a.Value) >= 4 {
				r.Oif = int(binary.NativeEndian.Uint32(a.Value))
			}
		case unix.RTA_TABLE:
			if len(a.Value) >= 4 {
				r.Table = binary.NativeEndian.Uint32(a.Value)
			}
		case unix.RTA_PRIORITY:
			if len(a.Value) >= 4 {
				r.Priority = binary.NativeEndian.Uint32(a.Value)
			}
		case unix.RTA_MULTIPATH:
			// struct rtnexthop: len u16, flags u8, hops u8, ifindex i32.
			if r.Oif == 0 && len(a.Value) >= 8 {
				r.Oif = int(int32(binary.NativeEndian.Uint32(a.Value[4:8])))
			}
		}
	}
	return r, true
}
