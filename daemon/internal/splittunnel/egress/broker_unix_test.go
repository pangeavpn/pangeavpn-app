//go:build darwin || linux

package egress

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

const brokerHelperEnv = "EGRESS_TEST_BROKER_HELPER"

func TestMain(m *testing.M) {
	if os.Getenv(brokerHelperEnv) == "1" {
		os.Exit(runBrokerFD(3, testPin))
	}
	os.Exit(m.Run())
}

// inProcessBroker returns a spawn func that serves dial on a socket pair inside the test process.
func inProcessBroker(t *testing.T, dial brokerDialFunc, spawns *atomic.Int32) (func() (*brokerProc, error), func()) {
	t.Helper()
	var mu sync.Mutex
	var current net.Conn
	spawn := func() (*brokerProc, error) {
		parent, child, err := socketPair()
		if err != nil {
			return nil, err
		}
		pc, err := net.FileConn(parent)
		parent.Close()
		if err != nil {
			child.Close()
			return nil, err
		}
		cc, err := net.FileConn(child)
		child.Close()
		if err != nil {
			pc.Close()
			return nil, err
		}
		mu.Lock()
		current = cc
		mu.Unlock()
		exited := make(chan struct{})
		go func() {
			_ = serveBroker(cc.(*net.UnixConn), dial)
			cc.Close()
			close(exited)
		}()
		if spawns != nil {
			spawns.Add(1)
		}
		return &brokerProc{conn: pc.(*net.UnixConn), exited: exited, kill: func() { cc.Close() }}, nil
	}
	crash := func() {
		mu.Lock()
		defer mu.Unlock()
		if current != nil {
			current.Close()
			current = nil
		}
	}
	return spawn, crash
}

func startClient(t *testing.T, spawn func() (*brokerProc, error)) *brokerClient {
	t.Helper()
	b := newBrokerClient(spawn, newRateLog(t.Logf))
	b.minBackoff = 200 * time.Millisecond
	if err := b.start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.close() })
	return b
}

func echoListener(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return ln
}

func recordingPin(seen *sync.Map) func(fd uintptr, ifindex int) error {
	return func(fd uintptr, ifindex int) error {
		seen.Store(ifindex, true)
		return nil
	}
}

func TestBrokerTCPRoundTrip(t *testing.T) {
	ln := echoListener(t)
	var seen sync.Map
	spawn, _ := inProcessBroker(t, brokerDial(recordingPin(&seen)), nil)
	b := startClient(t, spawn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := b.dialTCP(ctx, 7, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, ok := seen.Load(7); !ok {
		t.Fatal("broker did not pin the socket to the requested interface")
	}
	if c.RemoteAddr().String() != ln.Addr().String() {
		t.Fatalf("remote %v, want %v", c.RemoteAddr(), ln.Addr())
	}
	if v := getsockoptUnix(t, c.(syscall.Conn), unix.SOL_SOCKET, unix.SO_KEEPALIVE); v == 0 {
		t.Fatal("keepalive off on the passed socket")
	}
	if v := getsockoptUnix(t, c.(syscall.Conn), unix.IPPROTO_TCP, unix.TCP_KEEPINTVL); v != 10 {
		t.Fatalf("TCP_KEEPINTVL = %d, want 10", v)
	}
	if v := getsockoptUnix(t, c.(syscall.Conn), unix.IPPROTO_TCP, unix.TCP_KEEPCNT); v != 3 {
		t.Fatalf("TCP_KEEPCNT = %d, want 3", v)
	}
	if _, err := c.Write([]byte("through the broker")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len("through the broker"))
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "through the broker" {
		t.Fatalf("echo = %q, %v", buf, err)
	}
}

func TestBrokerUDPRoundTrip(t *testing.T) {
	peer, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	var seen sync.Map
	spawn, _ := inProcessBroker(t, brokerDial(recordingPin(&seen)), nil)
	b := startClient(t, spawn)

	pc, err := b.listenUDP(context.Background(), 9)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	if _, ok := seen.Load(9); !ok {
		t.Fatal("broker did not pin the UDP socket")
	}
	local := pc.LocalAddr().(*net.UDPAddr)
	if !local.IP.Equal(net.IPv4zero) || local.Port == 0 {
		t.Fatalf("local address %v, want 0.0.0.0:<port>", local)
	}
	if _, err := pc.WriteTo([]byte("ping"), peer.LocalAddr()); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 16)
	_ = peer.SetReadDeadline(time.Now().Add(5 * time.Second))
	n, from, err := peer.ReadFrom(buf)
	if err != nil || string(buf[:n]) != "ping" || from.(*net.UDPAddr).Port != local.Port {
		t.Fatalf("peer read %q from %v, %v", buf[:n], from, err)
	}
	if _, err := peer.WriteTo([]byte("pong"), from); err != nil {
		t.Fatal(err)
	}
	_ = pc.SetReadDeadline(time.Now().Add(5 * time.Second))
	if n, _, err := pc.ReadFrom(buf); err != nil || string(buf[:n]) != "pong" {
		t.Fatalf("reply %q, %v", buf[:n], err)
	}
}

// TestBrokerPairsDescriptorsWithResponses interleaves failures and successes so a
// descriptor matched to the wrong response would read back someone else's tag.
func TestBrokerPairsDescriptorsWithResponses(t *testing.T) {
	ln := echoListener(t)
	dial := func(ctx context.Context, req brokerRequest) (*os.File, error) {
		if req.IfIndex%3 == 0 {
			return nil, &net.OpError{Op: "dial", Net: "tcp4", Err: os.NewSyscallError("connect", unix.ENETUNREACH)}
		}
		var d net.Dialer
		c, err := d.DialContext(ctx, "tcp4", req.Addr)
		if err != nil {
			return nil, err
		}
		defer c.Close()
		var tag [4]byte
		binary.BigEndian.PutUint32(tag[:], uint32(req.IfIndex))
		if _, err := c.Write(tag[:]); err != nil {
			return nil, err
		}
		return c.(*net.TCPConn).File()
	}
	spawn, _ := inProcessBroker(t, dial, nil)
	b := startClient(t, spawn)

	const n = 120
	errs := make(chan error, n)
	var wg sync.WaitGroup
	for i := 1; i <= n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			c, err := b.dialTCP(ctx, i, ln.Addr().String())
			if i%3 == 0 {
				if c != nil || !IsUnreachable(err) {
					errs <- fmt.Errorf("request %d: got %v, %v; want an unreachable error", i, c, err)
				}
				return
			}
			if err != nil {
				errs <- fmt.Errorf("request %d: %v", i, err)
				return
			}
			defer c.Close()
			var tag [4]byte
			_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
			if _, err := io.ReadFull(c, tag[:]); err != nil {
				errs <- fmt.Errorf("request %d: read tag: %v", i, err)
				return
			}
			if got := binary.BigEndian.Uint32(tag[:]); got != uint32(i) {
				errs <- fmt.Errorf("request %d got the socket of request %d", i, got)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if s := b.strayCount(); s != 0 {
		t.Fatalf("%d stray descriptors", s)
	}
}

// expectEOF waits for the far side of an accepted connection to see the dialed socket fully closed.
func expectEOF(t *testing.T, accepted <-chan net.Conn) {
	t.Helper()
	select {
	case c := <-accepted:
		defer c.Close()
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		if _, err := c.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
			t.Fatalf("server side read = %v, want EOF (descriptor leaked)", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("broker never connected")
	}
}

func acceptingListener(t *testing.T) (net.Listener, <-chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	accepted := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- c
		}
	}()
	return ln, accepted
}

func TestBrokerLateResponseClosesDescriptor(t *testing.T) {
	ln, accepted := acceptingListener(t)
	release := make(chan struct{})
	answered := make(chan struct{})
	inner := brokerDial(func(uintptr, int) error { return nil })
	dial := func(ctx context.Context, req brokerRequest) (*os.File, error) {
		<-release
		defer close(answered)
		return inner(context.Background(), req)
	}
	spawn, _ := inProcessBroker(t, dial, nil)
	b := startClient(t, spawn)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.dialTCP(ctx, 3, ln.Addr().String()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled dial = %v", err)
	}
	close(release)
	<-answered
	expectEOF(t, accepted)
	deadline := time.Now().Add(2 * time.Second)
	for b.strayCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if s := b.strayCount(); s != 1 {
		t.Fatalf("stray count = %d, want 1", s)
	}
}

func TestBrokerUnknownIDClosesDescriptor(t *testing.T) {
	ln, accepted := acceptingListener(t)
	parent, child, err := socketPair()
	if err != nil {
		t.Fatal(err)
	}
	pc, _ := net.FileConn(parent)
	parent.Close()
	cc, _ := net.FileConn(child)
	child.Close()
	fake := newFrameConn(cc.(*net.UnixConn))
	defer cc.Close()
	exited := make(chan struct{})
	b := startClient(t, func() (*brokerProc, error) {
		return &brokerProc{conn: pc.(*net.UnixConn), exited: exited, kill: func() {}}, nil
	})
	defer close(exited)

	c, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	file, err := c.(*net.TCPConn).File()
	c.Close()
	if err != nil {
		t.Fatal(err)
	}
	frame, _ := encodeFrame(brokerResponse{ID: 999})
	if err := fake.writeFrame(frame, file); err != nil {
		t.Fatal(err)
	}
	file.Close()
	expectEOF(t, accepted)
	if s := b.strayCount(); s != 1 {
		t.Fatalf("stray count = %d, want 1", s)
	}
}

func TestBrokerSupervisionRestarts(t *testing.T) {
	ln := echoListener(t)
	var spawns atomic.Int32
	block := make(chan struct{})
	inner := brokerDial(func(uintptr, int) error { return nil })
	dial := func(ctx context.Context, req brokerRequest) (*os.File, error) {
		if req.IfIndex == 99 {
			select {
			case <-block:
			case <-ctx.Done():
			}
			return nil, errors.New("aborted")
		}
		return inner(ctx, req)
	}
	spawn, crash := inProcessBroker(t, dial, &spawns)
	b := startClient(t, spawn)

	inflight := make(chan error, 1)
	go func() {
		_, err := b.dialTCP(context.Background(), 99, ln.Addr().String())
		inflight <- err
	}()
	time.Sleep(50 * time.Millisecond)
	crash()
	select {
	case err := <-inflight:
		if !errors.Is(err, ErrBrokerUnavailable) {
			t.Fatalf("in-flight request after crash = %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("in-flight request not failed when the broker died")
	}

	start := time.Now()
	_, err := b.dialTCP(context.Background(), 1, ln.Addr().String())
	if !errors.Is(err, ErrBrokerUnavailable) {
		t.Fatalf("dial while down = %v, want ErrBrokerUnavailable", err)
	}
	if time.Since(start) > 50*time.Millisecond {
		t.Fatalf("dial while down took %v; want fail-fast", time.Since(start))
	}

	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := b.dialTCP(context.Background(), 1, ln.Addr().String())
		if err == nil {
			c.Close()
			break
		}
		if !errors.Is(err, ErrBrokerUnavailable) || time.Now().After(deadline) {
			t.Fatalf("broker never came back: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := spawns.Load(); got != 2 {
		t.Fatalf("spawns = %d, want 2", got)
	}
	close(block)

	_ = b.close()
	if _, err := b.dialTCP(context.Background(), 1, ln.Addr().String()); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("dial after close = %v", err)
	}
	select {
	case <-b.done:
	default:
		t.Fatal("supervisor still running after close")
	}
}

func TestBrokerRealProcess(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to set the broker credential")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const gid = 65534
	env := append(os.Environ(), brokerHelperEnv+"=1", "GORACE=atexit_sleep_ms=0")
	var first atomic.Pointer[brokerProc]
	spawn := func() (*brokerProc, error) {
		p, err := spawnBroker(exe, []string{"-test.run=^$"}, env, &syscall.Credential{Uid: 0, Gid: gid})
		if err == nil {
			first.CompareAndSwap(nil, p)
		}
		return p, err
	}
	b := newBrokerClient(spawn, newRateLog(t.Logf))
	if err := b.start(); err != nil {
		t.Fatal(err)
	}
	defer b.close()
	proc := first.Load()
	if got := procGID(t, proc.pid); got != gid {
		t.Fatalf("broker gid = %d, want %d", got, gid)
	}
	ln := echoListener(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, err := b.dialTCP(ctx, 1234, ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	checkTestPin(t, c.(syscall.Conn), 1234)
	if _, err := c.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	pc, err := b.listenUDP(ctx, 77)
	if err != nil {
		t.Fatal(err)
	}
	defer pc.Close()
	checkTestPin(t, pc.(syscall.Conn), 77)

	proc.conn.Close()
	// stop() only kills a broker that lingers 2 s, so exiting sooner means it saw EOF.
	select {
	case <-proc.exited:
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("broker did not exit on EOF")
	}
}

// testPin stands in for IP_BOUND_IF in the helper broker: it stamps the TTL so the test can read it back.
func testPin(fd uintptr, ifindex int) error {
	return unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TTL, ifindex%250+1)
}

func checkTestPin(t *testing.T, c syscall.Conn, ifindex int) {
	t.Helper()
	if got := getsockoptUnix(t, c, unix.IPPROTO_IP, unix.IP_TTL); got != ifindex%250+1 {
		t.Fatalf("IP_TTL = %d, want %d: the broker's pin did not reach the passed socket", got, ifindex%250+1)
	}
}

func getsockoptUnix(t *testing.T, c syscall.Conn, level, opt int) int {
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

func procGID(t *testing.T, pid int) int {
	t.Helper()
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		t.Skipf("no procfs: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "Gid:"); ok {
			var real, eff int
			fmt.Sscan(rest, &real, &eff)
			return eff
		}
	}
	t.Fatal("no Gid line")
	return -1
}
