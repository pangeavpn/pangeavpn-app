package shadowsocks

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// failingPacketConn fails the first failWrites WriteTo calls with err, then
// succeeds, recording how many datagrams got through.
type failingPacketConn struct {
	net.PacketConn
	failWrites atomic.Int32
	err        error
	delivered  atomic.Int32
	readBlock  chan struct{}
	closed     atomic.Bool
}

func newFailingPacketConn(failCount int, err error) *failingPacketConn {
	c := &failingPacketConn{err: err, readBlock: make(chan struct{})}
	c.failWrites.Store(int32(failCount))
	return c
}

func (c *failingPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if c.failWrites.Add(-1) >= 0 {
		return 0, c.err
	}
	c.delivered.Add(1)
	return len(p), nil
}

func (c *failingPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	<-c.readBlock
	return 0, nil, net.ErrClosed
}

func (c *failingPacketConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		close(c.readBlock)
	}
	return nil
}

func (c *failingPacketConn) LocalAddr() net.Addr                { return &net.UDPAddr{} }
func (c *failingPacketConn) SetDeadline(t time.Time) error      { return nil }
func (c *failingPacketConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *failingPacketConn) SetWriteDeadline(t time.Time) error { return nil }

// An oversized datagram (EMSGSIZE on Windows) must be dropped, not fatal —
// otherwise one packet takes the tunnel down and the health check loops.
func TestBridgeUDP_UndeliverableDatagramDoesNotKillTheBridge(t *testing.T) {
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen local: %v", err)
	}
	defer local.Close()

	remote := newFailingPacketConn(3, errors.New("wsasend: message larger than the internal message buffer"))
	defer remote.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- bridgeUDP(ctx, local, remote, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 51820}) }()

	sender, err := net.DialUDP("udp", nil, local.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sender.Close()

	// Three rejected, then three that must get through.
	for range 6 {
		if _, err := sender.Write([]byte("wireguard-shaped-packet")); err != nil {
			t.Fatalf("write: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}

	select {
	case err := <-done:
		t.Fatalf("bridge exited on a per-datagram write failure: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	if got := remote.delivered.Load(); got == 0 {
		t.Fatal("no datagram was delivered after the failures cleared")
	}
}

// A closed socket still ends the bridge cleanly, so Stop is not left hanging.
func TestBridgeUDP_ClosedSocketEndsCleanly(t *testing.T) {
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen local: %v", err)
	}
	remote := newFailingPacketConn(0, nil)

	done := make(chan error, 1)
	go func() {
		done <- bridgeUDP(context.Background(), local, remote, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 51820})
	}()

	time.Sleep(30 * time.Millisecond)
	local.Close()
	remote.Close()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("bridgeUDP on a clean shutdown = %v, want nil", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bridgeUDP did not return after its sockets were closed")
	}
}

// A socket that fails every single write must eventually give up rather than
// spin forever, so the health check can restart it.
func TestBridgeUDP_PermanentFailureGivesUp(t *testing.T) {
	local, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen local: %v", err)
	}
	defer local.Close()

	remote := newFailingPacketConn(1<<30, errors.New("permanent write failure"))
	defer remote.Close()

	done := make(chan error, 1)
	go func() {
		done <- bridgeUDP(context.Background(), local, remote, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 51820})
	}()

	sender, err := net.DialUDP("udp", nil, local.LocalAddr().(*net.UDPAddr))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer sender.Close()
	for range maxConsecutiveBridgeErrors + 5 {
		sender.Write([]byte("x"))
	}

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a permanently failing socket should surface an error, not nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("bridgeUDP never gave up on a socket failing every write")
	}
}

func TestIsBridgeShutdown(t *testing.T) {
	if !isBridgeShutdown(net.ErrClosed) {
		t.Error("net.ErrClosed should end the bridge")
	}
	if !isBridgeShutdown(context.Canceled) {
		t.Error("context.Canceled should end the bridge")
	}
	if isBridgeShutdown(errors.New("wsasend: message too large")) {
		t.Error("a per-datagram failure must not end the bridge")
	}
}
