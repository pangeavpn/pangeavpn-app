package splittunnel

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"testing"
	"time"

	"golang.org/x/crypto/curve25519"
	"golang.zx2c4.com/wireguard/conn/bindtest"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

// startPump makes the first Read hand the inner device over to the pump.
func startPump(t testing.TB, h *harness) {
	t.Helper()
	h.tun.fromOS(tunnelUDP(40500, []byte("kick")))
	bufs, sizes := readBufs(h.dev.BatchSize(), 65535+wgOffset)
	n, err := h.dev.Read(bufs, sizes, wgOffset)
	if err != nil || n < 1 || sizes[0] < 1 {
		t.Fatalf("first Read = %d, %v", n, err)
	}
	h.engine()
}

func readBufs(n, size int) ([][]byte, []int) {
	bufs := make([][]byte, n)
	for i := range bufs {
		bufs[i] = make([]byte, size)
	}
	return bufs, make([]int, n)
}

func TestCloseUnblocksReadWithEmptyOut(t *testing.T) {
	for _, kind := range allKinds {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind, noReader: true})
			h.setRules(appGame)
			startPump(t, h)

			res := make(chan error, 1)
			go func() {
				bufs, sizes := readBufs(h.dev.BatchSize(), 65535+wgOffset)
				_, err := h.dev.Read(bufs, sizes, wgOffset)
				res <- err
			}()
			time.Sleep(50 * time.Millisecond)
			start := time.Now()
			if err := h.dev.Close(); err != nil {
				t.Fatal(err)
			}
			if d := time.Since(start); d > 200*time.Millisecond {
				t.Fatalf("Close took %v", d)
			}
			select {
			case err := <-res:
				if !errors.Is(err, os.ErrClosed) {
					t.Fatalf("blocked Read returned %v, want os.ErrClosed", err)
				}
			case <-time.After(200 * time.Millisecond):
				t.Fatal("blocked Read did not return after Close")
			}
		})
	}
}

func TestCloseWithFullOutAndStuckClassifier(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, noReader: true})
	h.eg.setTarget(h.server(echoConn))
	h.setRules(appGame)
	startPump(t, h)
	release := h.cls.block()
	defer release()
	for p := uint16(40510); p < 40520; p++ {
		h.cls.set(p, true, appGame)
		h.tun.fromOS(syn(p, dst(remoteA, 7500), uint32(p)))
	}
	for i := 0; i < 1500; i++ {
		h.tun.fromOS(tunnelUDP(40600, []byte(fmt.Sprintf("tunnel %d", i))))
	}
	e := h.engine()
	waitFor(t, 3*time.Second, "out full", func() bool { return len(e.out) == cap(e.out) && h.c.Status().Counters.OutDrops > 0 })
	waitFor(t, 3*time.Second, "classifier stuck", func() bool { return h.cls.calls.Load() > 0 })

	start := time.Now()
	if err := h.dev.Close(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("Close took %v with a full out queue and a stuck Classify", d)
	}
	bufs, sizes := readBufs(h.dev.BatchSize(), 65535+wgOffset)
	if _, err := h.dev.Read(bufs, sizes, wgOffset); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Read after Close = %v", err)
	}
	release()
}

func TestWriteAndReadAfterClose(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindDarwin, noReader: true})
	h.setRules(appGame)
	startPump(t, h)
	_ = h.dev.Close()
	b := make([]byte, wgOffset+40)
	copy(b[wgOffset:], syn(1, dst(remoteA, 1), 1))
	if n, err := h.dev.Write([][]byte{b}, wgOffset); n != 0 || !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Write after Close = %d, %v", n, err)
	}
	if err := h.device().writeInjected([][]byte{b}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("engine injection after Close = %v", err)
	}
	bufs, sizes := readBufs(1, 2048)
	if _, err := h.dev.Read(bufs, sizes, wgOffset); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("Read after Close = %v", err)
	}
	if err := h.dev.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
}

func TestReadReturnsStickyInnerError(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, noReader: true})
	h.setRules(appGame)
	startPump(t, h)
	h.tun.fromOS(tunnelUDP(40700, []byte("last packet")))
	h.tun.in <- nil
	bufs, sizes := readBufs(h.dev.BatchSize(), 65535+wgOffset)
	type result struct {
		got int
		err error
	}
	res := make(chan result, 1)
	go func() {
		got := 0
		for {
			n, err := h.dev.Read(bufs, sizes, wgOffset)
			got += n
			if err != nil {
				res <- result{got, err}
				return
			}
		}
	}()
	var r result
	select {
	case r = <-res:
	case <-time.After(3 * time.Second):
		t.Fatal("inner error never surfaced")
	}
	if !errors.Is(r.err, errInnerGone) {
		t.Fatalf("Read error = %v, want the inner error", r.err)
	}
	if r.got != 1 {
		t.Fatalf("read %d packets before the error, want the queued one", r.got)
	}
	for i := 0; i < 3; i++ {
		if _, err := h.dev.Read(bufs, sizes, wgOffset); !errors.Is(err, errInnerGone) {
			t.Fatalf("Read %d after the error = %v, want it sticky", i, err)
		}
	}
}

func TestForceMTUAndLUIDForwarding(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	h.setRules(appGame)
	h.tun.fromOS(tunnelUDP(40800, []byte("kick")))
	e := h.engine()

	type mtuForcer interface {
		ForceMTU(int)
		LUID() uint64
	}
	f, ok := h.dev.(mtuForcer)
	if !ok {
		t.Fatal("wrapper does not expose ForceMTU/LUID")
	}
	if f.LUID() != 0xfeed {
		t.Fatalf("LUID = %#x", f.LUID())
	}
	f.ForceMTU(1280)
	if h.tun.forced.Load() != 1280 || e.link.MTU() != 1280 {
		t.Fatalf("ForceMTU not forwarded: inner=%d link=%d", h.tun.forced.Load(), e.link.MTU())
	}

	inner := h.device().inner.(*winFakeTUN)
	inner.panicOnForce.Store(true)
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("inner ForceMTU panic did not reach the caller")
			}
		}()
		f.ForceMTU(1300)
	}()
	inner.panicOnForce.Store(false)
	f.ForceMTU(1340)
	if e.link.MTU() != 1340 {
		t.Fatal("a lock was left held after the inner panic")
	}
	h.device().UpdateTunnelInfo(wg.TunnelInfo{Addresses: []netip.Addr{netip.MustParseAddr("10.9.9.9")}, DNS: []netip.Addr{remoteA}, MTU: 1400})
	if e.link.MTU() != 1400 || !h.device().isTunnelDNS(remoteA) || h.device().isTunnelDNS(tunDNS) {
		t.Fatal("UpdateTunnelInfo did not apply DNS/MTU")
	}
	if h.device().TunnelAddr() != tunAddr {
		t.Fatal("UpdateTunnelInfo moved the tunnel address")
	}

	h.close()

	lin := newHarness(t, harnessOpts{kind: kindLinux})
	if l := lin.dev.(mtuForcer).LUID(); l != 0 {
		t.Fatalf("LUID without an inner LUID = %#x", l)
	}
	lin.dev.(mtuForcer).ForceMTU(1200)
}

func TestHandoverDivertsInPlaceWithoutKeepingCallerBuffers(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, noReader: true})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40900, true, appGame)
	h.setRules(appGame)

	first := tunnelUDP(40901, []byte("before"))
	second := tunnelUDP(40902, []byte("after"))
	h.tun.fromOS(first)
	h.tun.fromOS(syn(40900, dst(remoteA, 7900), 4242))
	h.tun.fromOS(second)
	time.Sleep(20 * time.Millisecond)

	bufs, sizes := readBufs(h.dev.BatchSize(), 65535+wgOffset)
	n, err := h.dev.Read(bufs, sizes, wgOffset)
	if err != nil && !errors.Is(err, tun.ErrTooManySegments) {
		t.Fatal(err)
	}
	if n != 3 || sizes[1] != 0 {
		t.Fatalf("handover batch n=%d sizes=%v; the bypass SYN must be zero-sized in place", n, sizes[:3])
	}
	if !bytes.Equal(bufs[0][wgOffset:wgOffset+sizes[0]], first) || !bytes.Equal(bufs[2][wgOffset:wgOffset+sizes[2]], second) {
		t.Fatal("tunnel packets in the handover batch were altered")
	}
	for _, b := range bufs {
		for i := range b {
			b[i] = 0xAA
		}
	}
	waitFor(t, 3*time.Second, "diverted SYN dialled", func() bool { return h.eg.dials.Load() == 1 })
}

func TestEngineCopiesIntoSmallBuffers(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, noReader: true})
	h.setRules(appGame)
	startPump(t, h)
	e := h.engine()
	for _, b := range e.batch {
		if len(b) != 65535+wgOffset {
			t.Fatalf("pump read buffer len %d, want the caller's %d", len(b), 65535+wgOffset)
		}
	}
	release := h.cls.block()
	defer release()
	h.cls.set(41000, true, appGame)
	h.tun.fromOS(syn(41000, dst(remoteA, 8000), 1))
	for i := 0; i < 20; i++ {
		h.tun.fromOS(tunnelUDP(41001, make([]byte, 1200)))
	}
	waitFor(t, 2*time.Second, "queued packets", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return len(e.out) == 20 && e.pending == 1
	})
	e.mu.Lock()
	for _, f := range e.tcp {
		for _, p := range f.queue {
			if cap(p.data) > engineBufSize {
				t.Errorf("pending packet holds a %d-byte buffer", cap(p.data))
			}
		}
	}
	e.mu.Unlock()
	for i := 0; i < 20; i++ {
		p := <-e.out
		if cap(p.data) > engineBufSize || len(p.data) != 1228 {
			t.Fatalf("out packet len=%d cap=%d", len(p.data), cap(p.data))
		}
		p.release()
	}
}

func TestCloseReleasesHandlerStuckInHandshake(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(41150, true, appGame)
	h.setRules(appGame)
	h.tun.blackhole.Store(true)
	h.tun.fromOS(syn(41150, dst(remoteA, 9150), 5))
	waitFor(t, 3*time.Second, "off-tunnel dial", func() bool { return h.eg.dials.Load() == 1 })
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	h.close()
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("teardown with a handshake in flight took %v", d)
	}
}

func TestRealWireGuardDeviceClosesQuickly(t *testing.T) {
	for _, kind := range []tunKind{kindWindows, kindLinux} {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind, noReader: true})
			dialing := make(chan struct{}, 1)
			h.eg.setHook(func(ctx context.Context, _ netip.AddrPort) (net.Conn, error) {
				select {
				case dialing <- struct{}{}:
				default:
				}
				<-ctx.Done()
				return nil, ctx.Err()
			})
			h.cls.set(41100, true, appGame)
			h.setRules(appGame)

			binds := bindtest.NewChannelBinds()
			wgDev := device.NewDevice(h.dev, binds[0], device.NewLogger(device.LogLevelSilent, ""))
			if err := wgDev.IpcSet(wgConfig(t)); err != nil {
				t.Fatal(err)
			}
			if err := wgDev.Up(); err != nil {
				t.Fatal(err)
			}
			go func() { _, _, _ = h.dial(41100, dst(remoteA, 9100), 10*time.Second) }()
			go func() { _, _, _ = h.dial(41101, dst(remoteB, 9101), 10*time.Second) }()
			for i := 0; i < 200; i++ {
				h.tun.fromOS(udpPacket(dst(tunAddr, 41102), dst(remoteB, 9), make([]byte, 600)))
			}
			select {
			case <-dialing:
			case <-time.After(5 * time.Second):
				t.Fatal("bypass dial never started")
			}

			start := time.Now()
			closed := make(chan struct{})
			go func() {
				wgDev.Close()
				close(closed)
			}()
			select {
			case <-closed:
			case <-time.After(2 * time.Second):
				t.Fatal("device.Close hung over the split-tunnel wrapper")
			}
			t.Logf("device.Close took %v", time.Since(start))
			got := make(chan error, 1)
			go func() {
				_, err := wgDev.IpcGet()
				got <- err
			}()
			select {
			case <-got:
			case <-time.After(2 * time.Second):
				t.Fatal("IpcGet hung after Close")
			}
		})
	}
}

func wgConfig(t *testing.T) string {
	var priv, peer [32]byte
	if _, err := rand.Read(priv[:]); err != nil {
		t.Fatal(err)
	}
	if _, err := rand.Read(peer[:]); err != nil {
		t.Fatal(err)
	}
	peer[0] &= 248
	peer[31] = peer[31]&127 | 64
	pub, err := curve25519.X25519(peer[:], curve25519.Basepoint)
	if err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("private_key=%s\nlisten_port=0\npublic_key=%s\nendpoint=127.0.0.1:2\nallowed_ip=0.0.0.0/0\n",
		hex.EncodeToString(priv[:]), hex.EncodeToString(pub))
}

// BenchmarkTunnelPath measures tunnel-bound packets through the pump while rules are active.
func BenchmarkTunnelPath(b *testing.B) {
	for _, kind := range []tunKind{kindWindows, kindLinux} {
		b.Run(kind.String(), func(b *testing.B) {
			h := newHarness(b, harnessOpts{kind: kind, noReader: true})
			h.setRules(appGame)
			startPump(b, h)
			pkt := udpPacket(dst(tunAddr, 40990), dst(remoteB, 9), make([]byte, 1300))
			bufs, sizes := readBufs(h.dev.BatchSize(), 65535+wgOffset)
			h.tun.in <- pkt
			for got := 0; got < 1; {
				k, err := h.dev.Read(bufs, sizes, wgOffset)
				if err != nil {
					b.Fatal(err)
				}
				got += k
			}
			const burst = 64
			b.SetBytes(int64(len(pkt)))
			b.ResetTimer()
			for i := 0; i < b.N; i += burst {
				n := min(burst, b.N-i)
				for j := 0; j < n; j++ {
					h.tun.in <- pkt
				}
				for got := 0; got < n; {
					k, err := h.dev.Read(bufs, sizes, wgOffset)
					if err != nil {
						b.Fatal(err)
					}
					got += k
				}
			}
		})
	}
}
