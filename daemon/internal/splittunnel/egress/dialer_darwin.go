package egress

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/net/route"
	"golang.org/x/sys/unix"
)

const brokerFlag = "--split-egress-broker"

const (
	routePath    = "/sbin/route"
	routeTimeout = 5 * time.Second
)

// darwinDialer gets its sockets from a broker child running with the pf-permitted gid.
type darwinDialer struct {
	ids    *identityCache
	broker *brokerClient
	mirror *routeMirror
	closed atomic.Bool
}

func New(opts Options) (Dialer, error) {
	if opts.BrokerGID <= 0 {
		return nil, fmt.Errorf("split egress: no broker group")
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("split egress: %w", err)
	}
	cred := &syscall.Credential{Uid: 0, Gid: uint32(opts.BrokerGID)}
	// An empty env, not nil: nil inherits the daemon's whole environment into a root child.
	spawn := func() (*brokerProc, error) { return spawnBroker(exe, []string{brokerFlag}, []string{}, cred) }
	log := newRateLog(opts.Logf)
	m := newRouteMirror(fetchRoutes, runRoute, interfaceState, log)
	d := &darwinDialer{ids: newIdentityCache(func() (Identity, error) { return resolvePhysical(m) }), broker: newBrokerClient(spawn, log), mirror: m}
	if err := d.broker.start(); err != nil {
		return nil, fmt.Errorf("split egress broker: %w", err)
	}
	_, _, _, _ = d.ids.refresh()
	return d, nil
}

// RemoveOrphanedRoutes drops scoped defaults a daemon that died without closing its dialer left behind.
func RemoveOrphanedRoutes(logf func(format string, args ...any)) {
	m := newRouteMirror(fetchRoutes, runRoute, interfaceState, newRateLog(logf))
	m.close()
}

func RunBroker() int { return runBrokerFD(3, setBoundIF) }

func setBoundIF(fd uintptr, index int) error {
	return unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_BOUND_IF, index)
}

func (d *darwinDialer) index() (int, error) {
	if d.closed.Load() {
		return 0, net.ErrClosed
	}
	id, err := d.ids.current()
	if err != nil {
		return 0, err
	}
	return id.Index, nil
}

func (d *darwinDialer) DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	target, err := tcpTarget(dst)
	if err != nil {
		return nil, err
	}
	index, err := d.index()
	if err != nil {
		return nil, err
	}
	return d.broker.dialTCP(ctx, index, target)
}

func (d *darwinDialer) ListenUDP(ctx context.Context) (net.PacketConn, error) {
	index, err := d.index()
	if err != nil {
		return nil, err
	}
	return d.broker.listenUDP(ctx, index)
}

// Repin is safe on broker sockets: pf matches the creating credential, which does not change.
func (d *darwinDialer) Repin(c syscall.Conn) error {
	index, err := d.index()
	if err != nil {
		return err
	}
	return connControl(c, func(fd uintptr) error { return setBoundIF(fd, index) })
}

func (d *darwinDialer) Current() (Identity, error) { return d.ids.current() }

// SetRouteWanted holds the scoped default only while flows may bypass; the next Refresh applies it.
func (d *darwinDialer) SetRouteWanted(wanted bool) { d.mirror.setWanted(wanted) }

func (d *darwinDialer) Refresh() (old, cur Identity, changed bool, err error) {
	return d.ids.refresh()
}

func (d *darwinDialer) Close() error {
	d.closed.Store(true)
	err := d.broker.close()
	d.mirror.close()
	return err
}

// resolvePhysical brings the mirror in line with the primary default, then names the egress interface.
func resolvePhysical(m *routeMirror) (Identity, error) {
	routes, err := m.fetch()
	if err != nil {
		return Identity{}, err
	}
	m.sync(routes)
	index, name, ok := selectDarwinDefault(routes, m.iface)
	if !ok {
		return Identity{}, ErrNoInterface
	}
	return identityFor(index, name)
}

func fetchRoutes() ([]ribRoute, error) {
	rib, err := route.FetchRIB(syscall.AF_INET, route.RIBTypeRoute, 0)
	if err != nil {
		return nil, err
	}
	msgs, err := route.ParseRIB(route.RIBTypeRoute, rib)
	if err != nil {
		return nil, err
	}
	return ribRoutes(msgs), nil
}

func runRoute(args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), routeTimeout)
	defer cancel()
	return routeResult(exec.CommandContext(ctx, routePath, args...).CombinedOutput())
}

const mirrorFlags = unix.RTF_IFSCOPE | unix.RTF_PROTO2

func ribRoutes(msgs []route.Message) []ribRoute {
	routes := make([]ribRoute, 0, len(msgs))
	for _, m := range msgs {
		rm, ok := m.(*route.RouteMessage)
		if !ok {
			continue
		}
		var next netip.Addr
		if len(rm.Addrs) > unix.RTAX_GATEWAY {
			if gw, ok := rm.Addrs[unix.RTAX_GATEWAY].(*route.Inet4Addr); ok {
				next = netip.AddrFrom4(gw.IP)
			}
		}
		routes = append(routes, ribRoute{
			Index:         rm.Index,
			DefaultV4:     rm.Flags&unix.RTF_HOST == 0 && isDefaultV4(rm.Addrs),
			Up:            rm.Flags&unix.RTF_UP != 0,
			Gateway:       rm.Flags&unix.RTF_GATEWAY != 0,
			IfScope:       rm.Flags&unix.RTF_IFSCOPE != 0,
			RejectOrBlack: rm.Flags&(unix.RTF_REJECT|unix.RTF_BLACKHOLE) != 0,
			NextHop:       next,
			Mirror:        rm.Flags&mirrorFlags == mirrorFlags,
		})
	}
	return routes
}

// isDefaultV4 matches 0.0.0.0 with a nil or all-zero mask (host routes are excluded by
// the caller); the tunnel's /1 halves miss.
func isDefaultV4(addrs []route.Addr) bool {
	if len(addrs) <= unix.RTAX_DST {
		return false
	}
	dst, ok := addrs[unix.RTAX_DST].(*route.Inet4Addr)
	if !ok || dst.IP != [4]byte{} {
		return false
	}
	if len(addrs) <= unix.RTAX_NETMASK || addrs[unix.RTAX_NETMASK] == nil {
		return true
	}
	switch mask := addrs[unix.RTAX_NETMASK].(type) {
	case *route.Inet4Addr:
		return mask.IP == [4]byte{}
	case *route.Inet6Addr:
		return mask.IP == [16]byte{}
	}
	return false
}
