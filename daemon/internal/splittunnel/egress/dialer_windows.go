package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sync/atomic"
	"syscall"

	"golang.org/x/sys/windows"
	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"
)

const ipUnicastIF = 31

type windowsDialer struct {
	ids    *identityCache
	closed atomic.Bool
}

func New(opts Options) (Dialer, error) {
	return newWindowsDialer(resolvePhysical), nil
}

func newWindowsDialer(resolve func() (Identity, error)) *windowsDialer {
	d := &windowsDialer{ids: newIdentityCache(resolve)}
	_, _, _, _ = d.ids.refresh()
	return d
}

func RunBroker() int { return 2 }

func (d *windowsDialer) pinned() (func(fd uintptr) error, error) {
	if d.closed.Load() {
		return nil, net.ErrClosed
	}
	id, err := d.ids.current()
	if err != nil {
		return nil, err
	}
	return func(fd uintptr) error {
		err := setUnicastIF(fd, id.Index)
		// Windows refuses a removed adapter's index with WSAEINVAL; report it unreachable so dials refresh.
		if errors.Is(err, windows.WSAEINVAL) {
			return fmt.Errorf("interface %d gone: %w", id.Index, windows.WSAENETDOWN)
		}
		return err
	}, nil
}

func (d *windowsDialer) DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	target, err := tcpTarget(dst)
	if err != nil {
		return nil, err
	}
	pin, err := d.pinned()
	if err != nil {
		return nil, err
	}
	nd := net.Dialer{KeepAliveConfig: keepAliveConfig, Control: dialControl(pin)}
	return nd.DialContext(ctx, "tcp4", target)
}

func (d *windowsDialer) ListenUDP(ctx context.Context) (net.PacketConn, error) {
	pin, err := d.pinned()
	if err != nil {
		return nil, err
	}
	lc := net.ListenConfig{Control: dialControl(pin)}
	return lc.ListenPacket(ctx, "udp4", "0.0.0.0:0")
}

func (d *windowsDialer) Repin(c syscall.Conn) error {
	pin, err := d.pinned()
	if err != nil {
		return err
	}
	return connControl(c, pin)
}

func (d *windowsDialer) Current() (Identity, error) { return d.ids.current() }

func (d *windowsDialer) Refresh() (old, cur Identity, changed bool, err error) {
	return d.ids.refresh()
}

func (d *windowsDialer) Close() error {
	d.closed.Store(true)
	return nil
}

// setUnicastIF takes the IPv4 interface index in network byte order.
func setUnicastIF(fd uintptr, index int) error {
	var be [4]byte
	binary.BigEndian.PutUint32(be[:], uint32(index))
	return windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIF, int(binary.NativeEndian.Uint32(be[:])))
}

// getUnicastIF reads the index back; Windows returns it in host byte order.
func getUnicastIF(fd uintptr) (int, error) {
	return windows.GetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIF)
}

func resolvePhysical() (Identity, error) {
	table, err := winipcfg.GetIPForwardTable2(windows.AF_INET)
	if err != nil {
		return Identity{}, err
	}
	rows := make([]winRoute, 0, 4)
	for i := range table {
		row := &table[i]
		prefix := row.DestinationPrefix.Prefix()
		if !prefix.IsValid() || prefix.Bits() != 0 || !prefix.Addr().Is4() {
			continue
		}
		ifc, err := row.InterfaceLUID.Interface()
		if err != nil {
			continue
		}
		r := winRoute{
			LUID:     uint64(row.InterfaceLUID),
			Index:    row.InterfaceIndex,
			Name:     ifc.Alias(),
			IfType:   uint32(ifc.Type),
			Up:       ifc.OperStatus == winipcfg.IfOperStatusUp,
			Loopback: row.Loopback,
			NextHop:  row.NextHop.Addr(),
			Metric:   uint64(row.Metric),
		}
		if ipif, err := row.InterfaceLUID.IPInterface(windows.AF_INET); err == nil {
			r.Metric += uint64(ipif.Metric)
		}
		rows = append(rows, r)
	}
	best, ok := selectWindowsDefault(rows)
	if !ok {
		return Identity{}, ErrNoInterface
	}
	addrs, err := winipcfg.GetUnicastIPAddressTable(windows.AF_INET)
	if err != nil {
		return Identity{}, err
	}
	var v4 []netip.Addr
	for i := range addrs {
		a := &addrs[i]
		if uint64(a.InterfaceLUID) != best.LUID {
			continue
		}
		if a.DadState != winipcfg.DadStatePreferred && a.DadState != winipcfg.DadStateDeprecated {
			continue
		}
		v4 = append(v4, a.Address.Addr())
	}
	return Identity{Index: int(best.Index), Name: best.Name, Addrs: sortedV4(v4)}, nil
}
