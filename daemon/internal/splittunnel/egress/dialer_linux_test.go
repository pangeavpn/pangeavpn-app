package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

func requireRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root (CAP_NET_ADMIN)")
	}
}

func getsockoptInt(t *testing.T, c syscall.Conn, level, opt int) int {
	t.Helper()
	var v int
	if err := connControl(c, func(fd uintptr) error {
		var gerr error
		v, gerr = unix.GetsockoptInt(int(fd), level, opt)
		return gerr
	}); err != nil {
		t.Fatalf("getsockopt(%d, %d): %v", level, opt, err)
	}
	return v
}

func TestLinuxSocketsCarrySplitMark(t *testing.T) {
	requireRoot(t)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_, _ = io.Copy(c, c)
	}()
	d := newLinuxDialer(func() (Identity, error) { return Identity{}, ErrNoInterface })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c, err := d.DialTCP(ctx, netip.MustParseAddrPort(ln.Addr().String()))
	if err != nil {
		t.Fatalf("DialTCP must not depend on the identity: %v", err)
	}
	defer c.Close()
	tc := c.(syscall.Conn)
	if got := getsockoptInt(t, tc, unix.SOL_SOCKET, unix.SO_MARK); got != SplitMark {
		t.Fatalf("TCP SO_MARK = %#x, want %#x", got, SplitMark)
	}
	for _, o := range []struct {
		name        string
		level, opt  int
		want        int
		wantNonZero bool
	}{
		{"SO_KEEPALIVE", unix.SOL_SOCKET, unix.SO_KEEPALIVE, 0, true},
		{"TCP_KEEPIDLE", unix.IPPROTO_TCP, unix.TCP_KEEPIDLE, 30, false},
		{"TCP_KEEPINTVL", unix.IPPROTO_TCP, unix.TCP_KEEPINTVL, 10, false},
		{"TCP_KEEPCNT", unix.IPPROTO_TCP, unix.TCP_KEEPCNT, 3, false},
	} {
		got := getsockoptInt(t, tc, o.level, o.opt)
		if (o.wantNonZero && got == 0) || (!o.wantNonZero && got != o.want) {
			t.Fatalf("%s = %d, want %d", o.name, got, o.want)
		}
	}
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("echo = %q, %v", buf, err)
	}

	pc, err := d.ListenUDP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	local := pc.LocalAddr().(*net.UDPAddr)
	if !local.IP.Equal(net.IPv4zero) || local.Port == 0 {
		t.Fatalf("UDP local address %v, want 0.0.0.0:<port>", local)
	}
	uc := pc.(syscall.Conn)
	if got := getsockoptInt(t, uc, unix.SOL_SOCKET, unix.SO_MARK); got != SplitMark {
		t.Fatalf("UDP SO_MARK = %#x, want %#x", got, SplitMark)
	}
	if got := getsockoptInt(t, uc, unix.IPPROTO_IP, unix.IP_MTU_DISCOVER); got != unix.IP_PMTUDISC_DONT {
		t.Fatalf("IP_MTU_DISCOVER = %d, want IP_PMTUDISC_DONT", got)
	}
	if err := d.Repin(uc); err != nil {
		t.Fatalf("Repin = %v, want no-op", err)
	}
	_ = d.Close()
	if _, err := d.ListenUDP(ctx); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("ListenUDP after Close = %v", err)
	}
}

// inNetns runs fn on a thread moved into a fresh network namespace; the thread is discarded afterwards.
func inNetns(t *testing.T, fn func() error) {
	t.Helper()
	requireRoot(t)
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			errc <- fmt.Errorf("unshare: %w", err)
			return
		}
		errc <- fn()
	}()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
}

func addVeth(name, peer, cidr string) (netlink.Link, error) {
	if err := netlink.LinkAdd(&netlink.Veth{LinkAttrs: netlink.LinkAttrs{Name: name}, PeerName: peer}); err != nil {
		return nil, fmt.Errorf("add %s: %w", name, err)
	}
	link, err := netlink.LinkByName(name)
	if err != nil {
		return nil, err
	}
	peerLink, err := netlink.LinkByName(peer)
	if err != nil {
		return nil, err
	}
	addr, err := netlink.ParseAddr(cidr)
	if err != nil {
		return nil, err
	}
	if err := netlink.AddrAdd(link, addr); err != nil {
		return nil, err
	}
	for _, l := range []netlink.Link{link, peerLink} {
		if err := netlink.LinkSetUp(l); err != nil {
			return nil, err
		}
	}
	return link, nil
}

func defaultVia(link netlink.Link, gw string, metric int) *netlink.Route {
	return &netlink.Route{LinkIndex: link.Attrs().Index, Gw: net.ParseIP(gw), Priority: metric, Table: unix.RT_TABLE_MAIN}
}

func TestLinuxIdentityFromMainTable(t *testing.T) {
	inNetns(t, func() error {
		lo, err := netlink.LinkByName("lo")
		if err == nil {
			_ = netlink.LinkSetUp(lo)
		}
		if _, err := resolvePhysical(); !errors.Is(err, ErrNoInterface) {
			return fmt.Errorf("empty namespace: err = %v, want ErrNoInterface", err)
		}
		eth, err := addVeth("egr0", "egr0p", "192.0.2.10/24")
		if err != nil {
			return err
		}
		wg, err := addVeth("wg-egr", "wg-egrp", "10.66.0.2/24")
		if err != nil {
			return err
		}
		alt, err := addVeth("egr1", "egr1p", "198.51.100.7/24")
		if err != nil {
			return err
		}
		if err := netlink.AddrAdd(eth, &netlink.Addr{IPNet: &net.IPNet{IP: net.ParseIP("192.0.2.5").To4(), Mask: net.CIDRMask(24, 32)}}); err != nil {
			return err
		}
		if err := netlink.RouteAdd(defaultVia(eth, "192.0.2.1", 100)); err != nil {
			return fmt.Errorf("default via egr0: %w", err)
		}
		if err := netlink.RouteAdd(defaultVia(wg, "10.66.0.1", 5)); err != nil {
			return fmt.Errorf("default via wg-egr: %w", err)
		}
		tunnelTable := &netlink.Route{LinkIndex: alt.Attrs().Index, Gw: net.ParseIP("198.51.100.1"), Table: 51820}
		if err := netlink.RouteAdd(tunnelTable); err != nil {
			return fmt.Errorf("table 51820 default: %w", err)
		}

		d := newLinuxDialer(resolvePhysical)
		id, err := d.Current()
		want := Identity{Index: eth.Attrs().Index, Name: "egr0", Addrs: addrs("192.0.2.5", "192.0.2.10")}
		if err != nil || !id.Equal(want) {
			return fmt.Errorf("identity = %+v, %v; want %+v", id, err, want)
		}
		if _, _, changed, err := d.Refresh(); err != nil || changed {
			return fmt.Errorf("unchanged refresh: changed=%v err=%v", changed, err)
		}

		if err := netlink.RouteAdd(defaultVia(alt, "198.51.100.1", 50)); err != nil {
			return fmt.Errorf("default via egr1: %w", err)
		}
		old, cur, changed, err := d.Refresh()
		wantAlt := Identity{Index: alt.Attrs().Index, Name: "egr1", Addrs: addrs("198.51.100.7")}
		if err != nil || !changed || !old.Equal(want) || !cur.Equal(wantAlt) {
			return fmt.Errorf("roam: old=%+v cur=%+v changed=%v err=%v", old, cur, changed, err)
		}

		for _, r := range []*netlink.Route{defaultVia(alt, "198.51.100.1", 50), defaultVia(eth, "192.0.2.1", 100)} {
			if err := netlink.RouteDel(r); err != nil {
				return fmt.Errorf("route del: %w", err)
			}
		}
		multipath := &netlink.Route{Dst: &net.IPNet{IP: net.IPv4zero.To4(), Mask: net.CIDRMask(0, 32)}, Table: unix.RT_TABLE_MAIN, MultiPath: []*netlink.NexthopInfo{
			{LinkIndex: alt.Attrs().Index, Gw: net.ParseIP("198.51.100.1")},
			{LinkIndex: eth.Attrs().Index, Gw: net.ParseIP("192.0.2.1")},
		}, Priority: 20}
		if err := netlink.RouteAdd(multipath); err != nil {
			return fmt.Errorf("multipath default: %w", err)
		}
		if _, cur, changed, err := d.Refresh(); err != nil || changed || !cur.Equal(wantAlt) {
			return fmt.Errorf("multipath: cur=%+v changed=%v err=%v", cur, changed, err)
		}
		if err := netlink.RouteDel(multipath); err != nil {
			return fmt.Errorf("multipath del: %w", err)
		}

		old, cur, changed, err = d.Refresh()
		if !errors.Is(err, ErrNoInterface) || !changed || !old.Equal(wantAlt) || cur.Index != 0 {
			return fmt.Errorf("loss: old=%+v cur=%+v changed=%v err=%v", old, cur, changed, err)
		}
		if _, err := d.Current(); !errors.Is(err, ErrNoInterface) {
			return fmt.Errorf("current after loss = %v", err)
		}
		return nil
	})
}

func TestIsUnreachableUnix(t *testing.T) {
	for _, errno := range []syscall.Errno{unix.ENETUNREACH, unix.EHOSTUNREACH, unix.ENETDOWN, unix.EADDRNOTAVAIL, unix.ENXIO} {
		if !IsUnreachable(&net.OpError{Op: "write", Err: os.NewSyscallError("sendto", errno)}) {
			t.Fatalf("%v not unreachable", errno)
		}
	}
	for _, errno := range []syscall.Errno{unix.ECONNREFUSED, unix.EACCES, unix.EPERM, unix.ENOBUFS, unix.ETIMEDOUT} {
		if IsUnreachable(&net.OpError{Op: "write", Err: os.NewSyscallError("sendto", errno)}) {
			t.Fatalf("%v classified unreachable", errno)
		}
	}
}

func TestNewProbesMark(t *testing.T) {
	requireRoot(t)
	d, err := New(Options{})
	if err != nil {
		t.Fatalf("New as root: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}
