package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"math/bits"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

const (
	tcpKeepCnt       = 16
	tcpKeepIdle      = 3
	tcpKeepIntvl     = 17
	ipDontFragment   = 14
	soKeepAliveLevel = windows.SOL_SOCKET
)

func loopbackIndex(t *testing.T) int {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 {
			return ifc.Index
		}
	}
	t.Skip("no loopback interface")
	return 0
}

func otherIndex(t *testing.T, not int) int {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, ifc := range ifaces {
		if ifc.Index != not {
			return ifc.Index
		}
	}
	t.Skip("only one interface")
	return 0
}

type switchableResolver struct {
	mu  sync.Mutex
	id  Identity
	err error
}

func (r *switchableResolver) set(id Identity, err error) {
	r.mu.Lock()
	r.id, r.err = id, err
	r.mu.Unlock()
}

func (r *switchableResolver) resolve() (Identity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.id, r.err
}

func sockoptInt(t *testing.T, c syscall.Conn, level, opt int) (int, error) {
	t.Helper()
	var v int
	err := connControl(c, func(fd uintptr) error {
		var gerr error
		v, gerr = windows.GetsockoptInt(windows.Handle(fd), level, opt)
		return gerr
	})
	return v, err
}

func unicastIF(t *testing.T, c syscall.Conn) int {
	t.Helper()
	var idx int
	if err := connControl(c, func(fd uintptr) error {
		var gerr error
		idx, gerr = getUnicastIF(fd)
		return gerr
	}); err != nil {
		t.Fatalf("getsockopt IP_UNICAST_IF: %v", err)
	}
	return idx
}

func TestUnicastIFIsNetworkByteOrder(t *testing.T) {
	lo := loopbackIndex(t)
	if bits.ReverseBytes32(uint32(lo)) == uint32(lo) || binary.NativeEndian.Uint16([]byte{1, 0}) != 1 {
		t.Skip("byte order not observable")
	}
	pc, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	hostOrder := connControl(pc.(syscall.Conn), func(fd uintptr) error {
		return windows.SetsockoptInt(windows.Handle(fd), windows.IPPROTO_IP, ipUnicastIF, lo)
	})
	if !errors.Is(hostOrder, windows.WSAEADDRNOTAVAIL) && !errors.Is(hostOrder, windows.WSAEINVAL) {
		t.Fatalf("host-order index %d accepted (%v); the kernel must want network order", lo, hostOrder)
	}
	if err := connControl(pc.(syscall.Conn), func(fd uintptr) error { return setUnicastIF(fd, lo) }); err != nil {
		t.Fatalf("network-order index %d refused: %v", lo, err)
	}
	if got := unicastIF(t, pc.(syscall.Conn)); got != lo {
		t.Fatalf("IP_UNICAST_IF = %d, want %d", got, lo)
	}
}

func TestDialTCPPinsUnicastIF(t *testing.T) {
	lo := loopbackIndex(t)
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

	d := newWindowsDialer(func() (Identity, error) { return Identity{Index: lo, Name: "lo"}, nil })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := d.DialTCP(ctx, netip.MustParseAddrPort(ln.Addr().String()))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if got := unicastIF(t, c.(syscall.Conn)); got != lo {
		t.Fatalf("IP_UNICAST_IF = %d, want %d", got, lo)
	}
	if c.LocalAddr().(*net.TCPAddr).IP.To4() == nil {
		t.Fatalf("local address %v is not IPv4", c.LocalAddr())
	}
	if v, err := sockoptInt(t, c.(syscall.Conn), soKeepAliveLevel, windows.SO_KEEPALIVE); err != nil || v == 0 {
		t.Fatalf("SO_KEEPALIVE = %d, %v", v, err)
	}
	for _, o := range []struct {
		name string
		opt  int
		want int
	}{{"TCP_KEEPIDLE", tcpKeepIdle, 30}, {"TCP_KEEPINTVL", tcpKeepIntvl, 10}, {"TCP_KEEPCNT", tcpKeepCnt, 3}} {
		v, err := sockoptInt(t, c.(syscall.Conn), windows.IPPROTO_TCP, o.opt)
		if err != nil {
			t.Logf("%s not readable here: %v", o.name, err)
			continue
		}
		if v != o.want {
			t.Fatalf("%s = %d, want %d", o.name, v, o.want)
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
}

func TestListenUDPPinsUnicastIF(t *testing.T) {
	lo := loopbackIndex(t)
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()

	d := newWindowsDialer(func() (Identity, error) { return Identity{Index: lo, Name: "lo"}, nil })
	pc, err := d.ListenUDP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	local := pc.LocalAddr().(*net.UDPAddr)
	if !local.IP.Equal(net.IPv4zero) || local.Port == 0 {
		t.Fatalf("local address %v, want 0.0.0.0:<port>", local)
	}
	if got := unicastIF(t, pc.(syscall.Conn)); got != lo {
		t.Fatalf("IP_UNICAST_IF = %d, want %d", got, lo)
	}
	if v, err := sockoptInt(t, pc.(syscall.Conn), windows.IPPROTO_IP, ipDontFragment); err != nil || v != 0 {
		t.Fatalf("IP_DONTFRAGMENT = %d, %v; want 0", v, err)
	}
	if _, err := pc.WriteTo([]byte("hello"), peer.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	_ = peer.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, from, err := peer.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "hello" || from.(*net.UDPAddr).Port != local.Port {
		t.Fatalf("peer read %q from %v, %v", buf[:n], from, err)
	}
}

func TestRepinFollowsRefresh(t *testing.T) {
	lo := loopbackIndex(t)
	other := otherIndex(t, lo)
	r := &switchableResolver{id: Identity{Index: lo, Name: "lo"}}
	d := newWindowsDialer(r.resolve)
	pc, err := d.ListenUDP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	port := pc.LocalAddr().(*net.UDPAddr).Port

	r.set(Identity{Index: other, Name: "other"}, nil)
	if _, _, changed, err := d.Refresh(); err != nil || !changed {
		t.Fatalf("Refresh changed=%v err=%v", changed, err)
	}
	if got := unicastIF(t, pc.(syscall.Conn)); got != lo {
		t.Fatalf("refresh alone re-pinned the socket to %d", got)
	}
	if err := d.Repin(pc.(syscall.Conn)); err != nil {
		t.Fatal(err)
	}
	if got := unicastIF(t, pc.(syscall.Conn)); got != other {
		t.Fatalf("IP_UNICAST_IF after Repin = %d, want %d", got, other)
	}
	if pc.LocalAddr().(*net.UDPAddr).Port != port {
		t.Fatal("Repin changed the local port")
	}

	r.set(Identity{}, ErrNoInterface)
	if _, _, changed, err := d.Refresh(); !errors.Is(err, ErrNoInterface) || !changed {
		t.Fatalf("Refresh to none changed=%v err=%v", changed, err)
	}
	if err := d.Repin(pc.(syscall.Conn)); !errors.Is(err, ErrNoInterface) {
		t.Fatalf("Repin without interface = %v", err)
	}
	if got := unicastIF(t, pc.(syscall.Conn)); got != other {
		t.Fatalf("failed Repin touched the socket: %d", got)
	}
}

// A removed adapter (an unplugged dock) leaves its index cached; the dial must read as unreachable so it refreshes and retries.
func TestDialPinnedToRemovedInterfaceIsUnreachable(t *testing.T) {
	const gone = 9999
	if _, err := net.InterfaceByIndex(gone); err == nil {
		t.Skipf("interface index %d exists here", gone)
	}
	lo := loopbackIndex(t)
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	dst := netip.MustParseAddrPort(ln.Addr().String())
	r := &switchableResolver{id: Identity{Index: gone, Name: "dock"}}
	d := newWindowsDialer(r.resolve)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if c, err := d.DialTCP(ctx, dst); !IsUnreachable(err) {
		if c != nil {
			c.Close()
		}
		t.Fatalf("DialTCP via a removed interface = %v, want an unreachable error", err)
	}
	if pc, err := d.ListenUDP(ctx); !IsUnreachable(err) {
		if pc != nil {
			pc.Close()
		}
		t.Fatalf("ListenUDP via a removed interface = %v, want an unreachable error", err)
	}
	r.set(Identity{Index: lo, Name: "lo"}, nil)
	if _, _, changed, err := d.Refresh(); err != nil || !changed {
		t.Fatalf("Refresh changed=%v err=%v", changed, err)
	}
	c, err := d.DialTCP(ctx, dst)
	if err != nil {
		t.Fatalf("DialTCP after Refresh: %v", err)
	}
	c.Close()
}

func TestDialFailsFastWithoutInterface(t *testing.T) {
	d := newWindowsDialer(func() (Identity, error) { return Identity{}, ErrNoInterface })
	ctx := context.Background()
	if _, err := d.DialTCP(ctx, netip.MustParseAddrPort("192.0.2.1:443")); !errors.Is(err, ErrNoInterface) {
		t.Fatalf("DialTCP = %v", err)
	}
	if _, err := d.ListenUDP(ctx); !errors.Is(err, ErrNoInterface) {
		t.Fatalf("ListenUDP = %v", err)
	}
	if _, err := d.DialTCP(ctx, netip.MustParseAddrPort("[2001:db8::1]:443")); !errors.Is(err, errNotIPv4) {
		t.Fatalf("DialTCP v6 = %v", err)
	}
	d2 := newWindowsDialer(func() (Identity, error) { return Identity{Index: 1}, nil })
	_ = d2.Close()
	if _, err := d2.ListenUDP(ctx); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("ListenUDP after Close = %v", err)
	}
}

func TestNewResolvesPhysicalInterface(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	id, err := d.Current()
	if errors.Is(err, ErrNoInterface) {
		t.Skip("no physical default route on this machine")
	}
	if err != nil {
		t.Fatal(err)
	}
	ifc, err := net.InterfaceByIndex(id.Index)
	if err != nil {
		t.Fatalf("selected index %d: %v", id.Index, err)
	}
	if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 || id.Name == "" {
		t.Fatalf("selected %+v (%v)", id, ifc.Flags)
	}
	for _, a := range id.Addrs {
		if !a.Is4() {
			t.Fatalf("non-IPv4 address %v", a)
		}
	}
	t.Logf("physical interface index %d with %d IPv4 addresses", id.Index, len(id.Addrs))
	if _, _, changed, err := d.Refresh(); err != nil || changed {
		t.Fatalf("second Refresh changed=%v err=%v", changed, err)
	}
}

func TestNewPinsSocketsToPhysicalInterface(t *testing.T) {
	d, err := New(Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	id, err := d.Current()
	if errors.Is(err, ErrNoInterface) || (err == nil && len(id.Addrs) == 0) {
		t.Skip("no physical default route with an IPv4 address on this machine")
	}
	if err != nil {
		t.Fatal(err)
	}
	pc, err := d.ListenUDP(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if got := unicastIF(t, pc.(syscall.Conn)); got != id.Index {
		t.Fatalf("UDP IP_UNICAST_IF = %d, want %d", got, id.Index)
	}

	ln, err := net.Listen("tcp4", netip.AddrPortFrom(id.Addrs[0], 0).String())
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := d.DialTCP(ctx, netip.MustParseAddrPort(ln.Addr().String()))
	if err != nil {
		t.Fatalf("DialTCP to the physical address: %v", err)
	}
	defer c.Close()
	if got := unicastIF(t, c.(syscall.Conn)); got != id.Index {
		t.Fatalf("TCP IP_UNICAST_IF = %d, want %d", got, id.Index)
	}
	if err := d.Repin(c.(syscall.Conn)); err != nil {
		t.Fatalf("Repin on a connected socket: %v", err)
	}
}

func TestIsUnreachableWindows(t *testing.T) {
	for _, code := range []syscall.Errno{10051, 10065, 10049, 10050, 1231, 1232} {
		if !IsUnreachable(&net.OpError{Op: "write", Err: code}) {
			t.Fatalf("errno %d not unreachable", code)
		}
	}
	for _, code := range []syscall.Errno{windows.WSAECONNREFUSED, windows.WSAEACCES, windows.WSAENOBUFS, windows.WSAETIMEDOUT, windows.ERROR_CONNECTION_REFUSED} {
		if IsUnreachable(&net.OpError{Op: "write", Err: code}) {
			t.Fatalf("errno %d classified unreachable", code)
		}
	}
}
