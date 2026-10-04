package splittunnel

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/adapters/gonet"
	"github.com/sagernet/gvisor/pkg/tcpip/checksum"
	"github.com/sagernet/gvisor/pkg/tcpip/header"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
)

var remoteC = netip.MustParseAddr("192.0.2.33")

type portSnapshot struct {
	exists     bool
	state      flowState
	failSafe   bool
	egressDown bool
	streak     uint8
	ttl        time.Duration
	sess       *udpSession
}

func (h *harness) port(p uint16) portSnapshot {
	e := h.engine()
	e.mu.Lock()
	defer e.mu.Unlock()
	f := e.udp[p]
	if f == nil {
		return portSnapshot{}
	}
	return portSnapshot{exists: true, state: f.state, failSafe: f.failSafe, egressDown: f.egressDown,
		streak: f.failStreak, ttl: time.Until(f.expiresAt), sess: f.sess}
}

func (h *harness) waitTunnelUDP(port, dstPort uint16) {
	h.t.Helper()
	if !h.waitTunnel(3*time.Second, func(i pktInfo) bool { return i.proto == protoUDP && i.srcPort == port && i.dstPort == dstPort }) {
		h.t.Fatalf("UDP %d -> :%d did not reach the tunnel", port, dstPort)
	}
}

func send(t testing.TB, c net.PacketConn, to netip.AddrPort, msg string) {
	t.Helper()
	if _, err := c.WriteTo([]byte(msg), udpAddr(to)); err != nil {
		t.Fatalf("app send: %v", err)
	}
}

func TestUDPBypassRoundTrip(t *testing.T) {
	for _, kind := range allKinds {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind})
			to := dst(remoteA, 9000)
			srv := h.udpServer(to)
			h.cls.set(43000, true, appGame)
			h.setRules(appGame)
			c := h.udpApp(43000)
			for i := 0; i < 5; i++ {
				msg := []byte(fmt.Sprintf("ping %d", i))
				udpExchange(t, c, to, msg, msg)
			}
			if h.tunnelHasPort(43000) {
				t.Fatal("bypassed UDP reached the tunnel")
			}
			if st := h.c.Status(); st.BypassUDP != 1 || !st.AppsActive || st.Counters.Bypassed != 1 {
				t.Fatalf("status = %+v", st)
			}
			src := srv.sources()
			for _, s := range src {
				if s != src[0] {
					t.Fatal("egress port changed within one session")
				}
			}
			if n := h.eg.listens.Load(); n != 1 {
				t.Fatalf("listens = %d, want 1", n)
			}
			if n := h.cls.classifiedCount(43000); n != 1 {
				t.Fatalf("port classified %d times, want once", n)
			}
		})
	}
}

func TestUDPAddressDependentFiltering(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	to := dst(remoteA, 9000)
	srv := h.udpServer(to)
	sameIP := h.udpServer(dst(remoteA, 9001))
	stranger := h.udpServer(dst(remoteC, 9000))
	h.cls.set(43010, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43010)
	udpExchange(t, c, to, []byte("hello"), []byte("hello"))

	egressAddr := srv.lastSource(t)
	_, _ = stranger.conn.WriteToUDPAddrPort([]byte("unsolicited"), egressAddr)
	time.Sleep(20 * time.Millisecond)
	_, _ = sameIP.conn.WriteToUDPAddrPort([]byte("same address"), egressAddr)
	expectUDP(t, c, dst(remoteA, 9001), []byte("same address"), 3*time.Second)
	expectNoUDP(t, c, 300*time.Millisecond)

	udpExchange(t, c, dst(remoteC, 9000), []byte("now a peer"), []byte("now a peer"))
}

func TestUDPPeerCapEvictsOldest(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, lim: func(l *limits) { l.udpPeers = 2 }})
	a, b, cc := dst(remoteA, 9010), dst(remoteB, 9010), dst(remoteC, 9010)
	srvA := h.udpServer(a)
	h.udpServer(b)
	h.udpServer(cc)
	h.cls.set(43020, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43020)
	for _, to := range []netip.AddrPort{a, b, cc} {
		udpExchange(t, c, to, []byte("hi"), []byte("hi"))
	}
	s := h.port(43020).sess
	if s == nil || s.peerCount() != 2 {
		t.Fatal("peer table not capped at 2")
	}
	_, _ = srvA.conn.WriteToUDPAddrPort([]byte("evicted peer"), srvA.lastSource(t))
	expectNoUDP(t, c, 300*time.Millisecond)
	udpExchange(t, c, a, []byte("back again"), []byte("back again"))
	if st := h.c.Status(); st.Counters.Torn != 0 || st.BypassUDP != 1 || h.eg.listens.Load() != 1 {
		t.Fatalf("the peer cap changed the verdict: %+v listens=%d", st, h.eg.listens.Load())
	}
}

// checkReplyFragments verifies every packet the engine wrote to port: within the MTU,
// valid IPv4 header checksums, and fragment trains with one ID, MF on all but the last.
func checkReplyFragments(t *testing.T, h *harness, port uint16) (fragmented int) {
	t.Helper()
	trains := map[uint16][][]byte{}
	for _, p := range h.tun.writtenPackets() {
		if len(p) < 20 || p[9] != protoUDP {
			continue
		}
		ip := header.IPv4(p)
		if ip.DestinationAddress() != tcpipAddr(tunAddr) {
			continue
		}
		if len(p) > testMTU {
			t.Fatalf("reply packet of %d bytes exceeds the MTU", len(p))
		}
		if checksum.Checksum(p[:20], 0) != 0xffff {
			t.Fatal("reply with a bad IPv4 header checksum")
		}
		if ip.More() || ip.FragmentOffset() != 0 {
			trains[ip.ID()] = append(trains[ip.ID()], p)
			continue
		}
		if len(p) >= 24 && binary.BigEndian.Uint16(p[22:24]) == port && ip.ID() != 0 {
			t.Fatal("an unfragmented reply consumed an IP ID")
		}
	}
	for id, train := range trains {
		if id == 0 {
			t.Fatal("fragmented reply without an IP ID")
		}
		next := 0
		for i, p := range train {
			ip := header.IPv4(p)
			if int(ip.FragmentOffset()) != next {
				t.Fatalf("fragment offset %d, want %d", ip.FragmentOffset(), next)
			}
			if last := i == len(train)-1; ip.More() == last {
				t.Fatalf("fragment %d/%d has MF=%v", i+1, len(train), ip.More())
			}
			next += len(p) - 20
		}
	}
	return len(trains)
}

func TestUDPRepliesLargerThanMTUAreFragmented(t *testing.T) {
	for _, kind := range []tunKind{kindWindows, kindLinux} {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind})
			to := dst(remoteA, 9100)
			h.udpServer(to)
			h.cls.set(43030, true, appGame)
			h.setRules(appGame)
			c := h.udpApp(43030)
			sizes := []int{testMTU - 28, testMTU - 27, 4000, 65000}
			for _, size := range sizes {
				udpExchange(t, c, to, []byte("big:"+strconv.Itoa(size)), pattern(size))
			}
			if n := checkReplyFragments(t, h, 43030); n != 3 {
				t.Fatalf("%d fragmented replies, want 3", n)
			}
		})
	}
}

func TestExcludedAppLargeDatagramsLeaveWhole(t *testing.T) {
	for _, kind := range []tunKind{kindWindows, kindDarwin} {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind})
			to := dst(remoteA, 9200)
			srv := h.udpServer(to)
			h.cls.set(43040, true, appGame)
			h.cls.set(43041, true, appGame)
			h.setRules(appGame)

			fresh := h.udpApp(43040)
			for _, n := range []int{1472, 8000} {
				udpExchange(t, fresh, to, pattern(n), pattern(n))
			}
			live := h.udpApp(43041)
			udpExchange(t, live, to, []byte("small first"), []byte("small first"))
			for _, n := range []int{1472, 8000} {
				udpExchange(t, live, to, pattern(n), pattern(n))
			}

			if got, want := h.eg.writesTo(to), []int{1472, 8000, 11, 1472, 8000}; !slices.Equal(got, want) {
				t.Fatalf("egress writes = %v, want %v (one WriteTo per datagram)", got, want)
			}
			for _, p := range srv.received() {
				if len(p) > 100 && !bytes.Equal(p, pattern(len(p))) {
					t.Fatalf("datagram of %d bytes corrupted", len(p))
				}
			}
			for _, p := range h.tunnelPackets() {
				if info, ok := parseIPv4(p); ok && (info.frag || info.srcPort == 43040 || info.srcPort == 43041) {
					t.Fatalf("part of an excluded datagram reached the tunnel: %+v", info)
				}
			}
		})
	}
}

func TestTunnelUDPFragmentsAreByteIdentical(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux})
	h.cls.set(43050, false, appChat)
	h.cls.set(43051, false, appChat)
	h.setRules(appGame)
	h.recordApp.Store(true)
	c := h.udpApp(43050)
	send(t, c, dst(remoteB, 9300), string(pattern(4000)))

	var app [][]byte
	waitFor(t, 2*time.Second, "app fragments", func() bool {
		app = app[:0]
		for _, p := range h.appPackets() {
			if info, ok := parseIPv4(p); ok && info.frag {
				app = append(app, p)
			}
		}
		return len(app) == 3
	})
	inTunnel := func(want [][]byte) func() bool {
		return func() bool {
			got := h.tunnelPackets()
			for _, w := range want {
				if !slices.ContainsFunc(got, func(p []byte) bool { return bytes.Equal(p, w) }) {
					return false
				}
			}
			return true
		}
	}
	waitFor(t, 3*time.Second, "tunnel fragments byte-identical", inTunnel(app))

	raw := fragment(udpPacket(dst(tunAddr, 43051), dst(remoteB, 9301), pattern(3000)), 4242, testMTU)
	for i := len(raw) - 1; i >= 0; i-- {
		h.tun.fromOS(raw[i])
	}
	waitFor(t, 3*time.Second, "out-of-order fragments in the tunnel", inTunnel(raw))
	got := h.tunnelPackets()
	first := slices.IndexFunc(got, func(p []byte) bool { return bytes.Equal(p, raw[0]) })
	for _, r := range raw[1:] {
		if slices.IndexFunc(got, func(p []byte) bool { return bytes.Equal(p, r) }) < first {
			t.Fatal("a held fragment overtook its first fragment")
		}
	}

	lone := fragment(udpPacket(dst(tunAddr, 43052), dst(remoteB, 9302), pattern(3000)), 4343, testMTU)[1]
	h.tun.fromOS(lone)
	waitFor(t, 4*time.Second, "orphan fragment flushed to the tunnel", inTunnel([][]byte{lone}))
	if h.eg.udpWrites.Load() != 0 {
		t.Fatal("tunnel datagrams reached the egress")
	}
}

func TestUDPOwnerChangeTearsSessionDown(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	a := dst(remoteA, 9400)
	srv := h.udpServer(a)
	h.cls.set(43060, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43060)
	udpExchange(t, c, a, []byte("mine"), []byte("mine"))
	egressAddr := srv.lastSource(t)
	writes := h.eg.udpWrites.Load()

	h.cls.setOwner(43060, 4242, false, appChat)
	start := time.Now()
	waitFor(t, 3*time.Second, "session torn", func() bool { return h.c.Status().BypassUDP == 0 })
	if d := time.Since(start); d > 1600*time.Millisecond {
		t.Fatalf("session torn after %v; the revalidator runs every second", d)
	}
	send(t, c, a, "old peer")
	send(t, c, dst(remoteB, 9401), "new peer")
	h.waitTunnelUDP(43060, 9400)
	h.waitTunnelUDP(43060, 9401)
	if h.eg.udpWrites.Load() != writes {
		t.Fatal("the new owner's traffic left through the old bypass socket")
	}
	_, _ = srv.conn.WriteToUDPAddrPort([]byte("late reply"), egressAddr)
	expectNoUDP(t, c, 300*time.Millisecond)
	if n := h.c.Status().Counters.Torn; n != 1 {
		t.Fatalf("Torn = %d", n)
	}
}

func TestUDPSharedPortGoesToTunnel(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindDarwin})
	a := dst(remoteA, 9500)
	h.udpServer(a)
	h.cls.set(43070, true, appGame)
	h.cls.setAmbiguous(43070, true)
	h.cls.set(43071, true, appGame)
	h.setRules(appGame)

	shared := h.udpApp(43070)
	send(t, shared, a, "two sockets on this port")
	h.waitTunnelUDP(43070, 9500)
	if h.eg.listens.Load() != 0 {
		t.Fatal("an ambiguous port got a bypass socket")
	}

	c := h.udpApp(43071)
	udpExchange(t, c, a, []byte("alone"), []byte("alone"))
	h.cls.setAmbiguous(43071, true)
	waitFor(t, 3*time.Second, "session torn once the port is shared", func() bool { return h.c.Status().BypassUDP == 0 })
	send(t, c, a, "shared now")
	h.waitTunnelUDP(43071, 9500)
}

func TestUDPFanOutKeepsTablesSmall(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, lim: func(l *limits) { l.udpPeers = 1000 }})
	h.cls.set(43080, false, appChat)
	h.cls.set(43081, true, appGame)
	h.setRules(appGame)
	a := dst(remoteA, 6881)
	h.udpServer(a)
	fanOut := func(port uint16, i int) []byte {
		to := netip.AddrPortFrom(netip.AddrFrom4([4]byte{100, 64 + byte(port&1), byte(i >> 8), byte(i)}), 6881)
		return udpPacket(dst(tunAddr, port), to, []byte("get_peers"))
	}
	inTunnel := func() int {
		n := 0
		for _, p := range h.tunnelPackets() {
			if len(p) >= 22 && binary.BigEndian.Uint16(p[20:22]) == 43080 {
				n++
			}
		}
		return n
	}

	h.tun.fromOS(fanOut(43080, 0))
	waitFor(t, 3*time.Second, "tunnel verdict", func() bool { return inTunnel() == 1 })
	const n = 10000
	for sent := 1; sent < n; {
		for j := 0; j < 256 && sent < n; j++ {
			h.tun.in <- fanOut(43080, sent)
			sent++
		}
		want := sent
		waitFor(t, 5*time.Second, "fan-out chunk in the tunnel", func() bool { return inTunnel() >= want })
	}
	e := h.engine()
	e.mu.Lock()
	entries, pending := len(e.udp), len(e.pendingQ)
	e.mu.Unlock()
	if entries != 1 || pending != 0 {
		t.Fatalf("one tunnel port to %d destinations left %d entries, %d pending", n, entries, pending)
	}
	if got := h.cls.classifiedCount(43080); got != 1 {
		t.Fatalf("tunnel port classified %d times", got)
	}

	c := h.udpApp(43081)
	udpExchange(t, c, a, []byte("join"), []byte("join"))
	before := h.eg.udpWrites.Load()
	const m = 3000
	for sent := 0; sent < m; {
		for j := 0; j < 32 && sent < m; j++ {
			h.tun.in <- fanOut(43081, sent)
			sent++
		}
		want := before + int64(sent)
		waitFor(t, 5*time.Second, "excluded fan-out chunk written", func() bool { return h.eg.udpWrites.Load() >= want })
	}
	if st := h.c.Status().Counters; st.OutDrops != 0 || st.UDPDrops != 0 {
		t.Fatalf("fan-out dropped packets: %+v", st)
	}
	s := h.port(43081).sess
	if s == nil || s.peerCount() != 1000 {
		t.Fatal("excluded fan-out not bounded by the peer cap")
	}
	e.mu.Lock()
	entries = len(e.udp)
	e.mu.Unlock()
	if entries != 2 || h.cls.classifiedCount(43081) != 1 || h.eg.listens.Load() != 1 {
		t.Fatalf("excluded fan-out: entries=%d classified=%d listens=%d", entries, h.cls.classifiedCount(43081), h.eg.listens.Load())
	}
	if h.tunnelHasPort(43081) {
		t.Fatal("excluded fan-out leaked into the tunnel")
	}
}

func TestSetRulesRemovalKeepsOtherAppsUDP(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux})
	h.eg.setTarget(h.server(echoConn))
	a, b := dst(remoteA, 9600), dst(remoteB, 9601)
	srvA, srvB := h.udpServer(a), h.udpServer(b)
	h.cls.set(43090, true, appGame)
	h.cls.set(43091, true, appChat)
	h.cls.set(43092, true, appGame)
	h.setRules(appGame, appChat)

	game, chat := h.udpApp(43090), h.udpApp(43091)
	udpExchange(t, game, a, []byte("game"), []byte("game"))
	udpExchange(t, chat, b, []byte("chat"), []byte("chat"))
	tc, _ := h.mustDial(43092, dst(remoteA, 7600))
	roundTrip(t, tc, "game tcp")
	chatEgress := srvB.lastSource(t)
	gameWrites := len(h.eg.writesTo(a))

	h.setRules(appChat)
	h.cls.set(43090, false, appGame)
	expectReset(t, tc)
	if st := h.c.Status(); st.BypassUDP != 1 {
		t.Fatalf("BypassUDP = %d after removing one app", st.BypassUDP)
	}
	udpExchange(t, chat, b, []byte("still bypassed"), []byte("still bypassed"))
	if srvB.lastSource(t) != chatEgress {
		t.Fatal("an unrelated rule edit changed the other app's egress port")
	}
	send(t, game, a, "now tunnelled")
	h.waitTunnelUDP(43090, 9600)
	if len(h.eg.writesTo(a)) != gameWrites || len(srvA.received()) != gameWrites {
		t.Fatal("the removed app kept using its bypass socket")
	}

	torn, listens := h.c.Status().Counters.Torn, h.eg.listens.Load()
	h.setRules(appChat)
	udpExchange(t, chat, b, []byte("identical rules"), []byte("identical rules"))
	if h.c.Status().Counters.Torn != torn || h.eg.listens.Load() != listens || srvB.lastSource(t) != chatEgress {
		t.Fatal("identical SetRules touched a session")
	}

	h.setRules()
	waitFor(t, 2*time.Second, "sessions closed with the rules", func() bool { return h.c.Status().BypassUDP == 0 })
	send(t, chat, b, "rules off")
	h.waitTunnelUDP(43091, 9601)
}

func TestNetworkChangedRepinsUDPAndResetsVanishedTCP(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	h.eg.setTarget(h.server(echoConn))
	a := dst(remoteA, 9700)
	srv := h.udpServer(a)
	h.cls.set(43100, true, appGame)
	h.cls.set(43101, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43100)
	udpExchange(t, c, a, []byte("before"), []byte("before"))
	egressAddr := srv.lastSource(t)
	tc, _ := h.mustDial(43101, dst(remoteA, 7700))
	roundTrip(t, tc, "tcp before")

	refreshes := h.eg.refreshes.Load()
	h.c.NetworkChanged()
	if h.eg.refreshes.Load() == refreshes || h.eg.repins.Load() != 0 || h.c.Status().Counters.Torn != 0 {
		t.Fatal("an event with the same interface changed something")
	}
	roundTrip(t, tc, "tcp unaffected")

	wifi := netip.MustParseAddr("192.0.2.10")
	h.c.mu.Lock()
	h.c.upAddrs = func() (map[netip.Addr]struct{}, error) { return map[netip.Addr]struct{}{wifi: {}}, nil }
	h.c.mu.Unlock()
	h.eg.setIdentity(egress.Identity{Index: 2, Name: "wifi0", Addrs: []netip.Addr{wifi}}, nil)
	h.c.NetworkChanged()
	if n := h.eg.repins.Load(); n != 1 {
		t.Fatalf("repins = %d, want 1", n)
	}
	expectReset(t, tc)
	udpExchange(t, c, a, []byte("after"), []byte("after"))
	if srv.lastSource(t) != egressAddr {
		t.Fatal("re-pinning changed the UDP egress port")
	}

	h.eg.setIdentity(egress.Identity{}, egress.ErrNoInterface)
	h.c.NetworkChanged()
	if h.eg.repins.Load() != 1 || h.c.Status().BypassUDP != 1 {
		t.Fatal("losing the network must keep sessions as they are")
	}
}

func TestNetworkChangedForgetsFailSafePorts(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, lim: func(l *limits) { l.pendingTimeout = 200 * time.Millisecond }})
	a := dst(remoteA, 9710)
	h.udpServer(a)
	h.cls.set(43110, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43110)
	release := h.cls.block()
	send(t, c, a, "during a classifier stall")
	h.waitTunnelUDP(43110, 9710)
	release()
	if p := h.port(43110); !p.failSafe || p.state != stateTunnel {
		t.Fatalf("port after a pending timeout = %+v", p)
	}
	h.eg.setIdentity(egress.Identity{Index: 3, Name: "eth0", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.20")}}, nil)
	h.c.NetworkChanged()
	if h.port(43110).exists {
		t.Fatal("fail-safe port kept across a network change")
	}
	udpExchange(t, c, a, []byte("bypass again"), []byte("bypass again"))
}

func TestBlockedUDPWriteNeverStallsTunnel(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, peer: true})
	a := dst(remoteA, 9800)
	h.udpServer(a)
	h.cls.set(43120, true, appGame)
	h.cls.set(43122, false, appChat)
	h.setRules(appGame)
	c := h.udpApp(43120)
	udpExchange(t, c, a, []byte("warm up"), []byte("warm up"))

	release := h.eg.blockWrites()
	defer release()
	for i := 0; i < 500; i++ {
		h.tun.in <- udpPacket(dst(tunAddr, 43120), a, make([]byte, 1200))
	}
	start := time.Now()
	h.tun.fromOS(tunnelUDP(43121, []byte("marker")))
	if !h.waitTunnel(time.Second, func(i pktInfo) bool { return i.srcPort == 43121 }) {
		t.Fatal("tunnel traffic stalled behind a blocked UDP send")
	}
	t.Logf("marker after a blocked bypass burst took %v", time.Since(start))
	waitFor(t, 2*time.Second, "send-queue drops", func() bool { return h.c.Status().Counters.UDPDrops > 0 })
	tc, _ := h.mustDial(43122, dst(remoteB, 7800))
	roundTrip(t, tc, "classification continues")
	tc.Close()
}

func TestBlockedUDPListenFailsSafe(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true, lim: func(l *limits) { l.listenTimeout = 300 * time.Millisecond }})
	a := dst(remoteA, 9810)
	h.udpServer(a)
	listening := make(chan struct{}, 1)
	h.eg.mu.Lock()
	h.eg.listenHook = func(ctx context.Context) (net.PacketConn, error) {
		select {
		case listening <- struct{}{}:
		default:
		}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	h.eg.mu.Unlock()
	h.cls.set(43130, true, appGame)
	h.cls.set(43131, true, appGame)
	h.cls.set(43133, false, appChat)
	h.setRules(appGame)

	c := h.udpApp(43130)
	send(t, c, a, "first")
	select {
	case <-listening:
	case <-time.After(3 * time.Second):
		t.Fatal("socket open never started")
	}
	h.tun.fromOS(tunnelUDP(43132, []byte("marker")))
	if !h.waitTunnel(time.Second, func(i pktInfo) bool { return i.srcPort == 43132 }) {
		t.Fatal("tunnel traffic stalled behind a blocked socket open")
	}
	tc, _ := h.mustDial(43133, dst(remoteB, 7810))
	roundTrip(t, tc, "classification continues while a socket opens")
	tc.Close()

	waitFor(t, 3*time.Second, "fail-safe after the open timeout", func() bool {
		p := h.port(43130)
		return p.failSafe && p.egressDown && p.sess == nil
	})
	send(t, c, a, "after")
	h.waitTunnelUDP(43130, 9810)

	h.eg.mu.Lock()
	h.eg.listenHook = nil
	h.eg.mu.Unlock()
	listens := h.eg.listens.Load()
	c2 := h.udpApp(43131)
	send(t, c2, a, "while egress is known bad")
	h.waitTunnelUDP(43131, 9810)
	if h.eg.listens.Load() != listens {
		t.Fatal("a new bypass verdict opened a socket during the egress back-off")
	}

	h.eg.setIdentity(egress.Identity{Index: 4, Name: "eth1", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.30")}}, nil)
	h.c.NetworkChanged()
	udpExchange(t, c, a, []byte("bypass after the change"), []byte("bypass after the change"))
}

func TestUDPFailSafeExpiresWithBackoff(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, lim: func(l *limits) {
		l.pendingTimeout = 150 * time.Millisecond
		l.failSafeBase = 300 * time.Millisecond
		l.failSafeMax = time.Second
	}})
	a := dst(remoteA, 9900)
	h.udpServer(a)
	h.cls.set(43140, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43140)
	release := h.cls.block()
	defer release()

	send(t, c, a, "1")
	h.waitTunnelUDP(43140, 9900)
	if p := h.port(43140); !p.failSafe || p.streak != 1 || p.ttl > 300*time.Millisecond {
		t.Fatalf("first fail-safe = %+v", p)
	}
	time.Sleep(350 * time.Millisecond)
	send(t, c, a, "2")
	waitFor(t, 2*time.Second, "second fail-safe", func() bool { p := h.port(43140); return p.failSafe && p.streak == 2 })
	if p := h.port(43140); p.ttl < 400*time.Millisecond {
		t.Fatalf("back-off did not grow: %+v", p)
	}
	release()
	classified := h.cls.classifiedCount(43140)
	send(t, c, a, "3")
	time.Sleep(50 * time.Millisecond)
	if h.cls.classifiedCount(43140) != classified {
		t.Fatal("a live fail-safe verdict was looked up again")
	}
	time.Sleep(650 * time.Millisecond)
	udpExchange(t, c, a, []byte("4"), []byte("4"))
	if p := h.port(43140); p.state != stateBypass || p.failSafe || p.streak != 0 {
		t.Fatalf("after recovery = %+v", p)
	}
}

func TestUDPTunnelPortRevalidates(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, lim: func(l *limits) { l.udpRevalidate = 300 * time.Millisecond }})
	a := dst(remoteA, 9950)
	h.udpServer(a)
	h.cls.set(43150, false, appChat)
	h.setRules(appGame)
	c := h.udpApp(43150)
	send(t, c, a, "tunnel 1")
	h.waitTunnelUDP(43150, 9950)

	h.cls.set(43150, true, appGame)
	send(t, c, a, "tunnel 2")
	time.Sleep(50 * time.Millisecond)
	if h.cls.classifiedCount(43150) != 1 {
		t.Fatal("a fresh tunnel verdict was looked up again")
	}
	time.Sleep(300 * time.Millisecond)
	send(t, c, a, "tunnel 3")
	waitFor(t, 2*time.Second, "flip to bypass", func() bool { return h.c.Status().BypassUDP == 1 })
	udpExchange(t, c, a, []byte("bypass now"), []byte("bypass now"))
	if n := h.cls.classifiedCount(43150); n != 2 {
		t.Fatalf("classified %d times, want 2", n)
	}
}

func TestUDPNewDestinationWaitsForOwnerCheck(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, lim: func(l *limits) {
		l.udpFresh = 100 * time.Millisecond
		l.validateEvery = time.Hour
		l.validateStale = time.Hour
		l.pendingTimeout = 3 * time.Second
	}})
	a, b, cc := dst(remoteA, 9960), dst(remoteB, 9961), dst(remoteC, 9962)
	h.udpServer(a)
	h.udpServer(b)
	h.udpServer(cc)
	h.cls.set(43160, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43160)
	udpExchange(t, c, a, []byte("first"), []byte("first"))
	waitFor(t, 2*time.Second, "first owner check", func() bool { return h.cls.valCalls.Load() > 0 })
	time.Sleep(250 * time.Millisecond)

	release := h.cls.blockValidate()
	send(t, c, b, "to b")
	time.Sleep(100 * time.Millisecond)
	if len(h.eg.writesTo(b)) != 0 || h.tunnelHasPort(43160) {
		t.Fatal("a new destination left before the owner was re-checked")
	}
	udpExchange(t, c, a, []byte("known peer"), []byte("known peer"))
	release()
	expectUDP(t, c, b, []byte("to b"), 3*time.Second)

	time.Sleep(250 * time.Millisecond)
	h.cls.setOwner(43160, 777, false, appChat)
	send(t, c, cc, "to c")
	h.waitTunnelUDP(43160, 9962)
	if len(h.eg.writesTo(cc)) != 0 || h.c.Status().BypassUDP != 0 {
		t.Fatal("a datagram held for the owner check bypassed after the owner changed")
	}
}

func TestUDPHeldDestinationFailsSafeWhenCheckStalls(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, lim: func(l *limits) {
		l.udpFresh = 100 * time.Millisecond
		l.validateEvery = time.Hour
		l.pendingTimeout = 200 * time.Millisecond
	}})
	a, b := dst(remoteA, 9970), dst(remoteB, 9971)
	h.udpServer(a)
	h.udpServer(b)
	h.cls.set(43170, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43170)
	udpExchange(t, c, a, []byte("first"), []byte("first"))
	waitFor(t, 2*time.Second, "first owner check", func() bool { return h.cls.valCalls.Load() > 0 })
	time.Sleep(250 * time.Millisecond)
	release := h.cls.blockValidate()
	defer release()
	send(t, c, b, "check never answers")
	h.waitTunnelUDP(43170, 9971)
	if p := h.port(43170); p.sess != nil || !p.failSafe || h.c.Status().Counters.Torn != 1 {
		t.Fatalf("port after a stalled owner check = %+v", p)
	}
}

type slowClassifier struct {
	*fakeClassifier
	delay time.Duration
}

func (s *slowClassifier) Classify(r *procmatch.Rules, flows []procmatch.FlowID) []procmatch.Result {
	time.Sleep(s.delay)
	return s.fakeClassifier.Classify(r, flows)
}

func TestUDPOwnerChecksKeepUpUnderClassifyLoad(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, lim: func(l *limits) {
		l.sweepEvery = 20 * time.Millisecond
		l.scanEvery = 50 * time.Millisecond
		l.validateEvery = 100 * time.Millisecond
		l.validateStale = time.Second
	}})
	slow := &slowClassifier{fakeClassifier: h.cls, delay: 20 * time.Millisecond}
	h.c.opts.NewClassifier = func() (procmatch.Classifier, error) { return slow, nil }
	a := dst(remoteA, 9990)
	h.udpServer(a)
	h.cls.set(43190, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43190)
	udpExchange(t, c, a, []byte("before load"), []byte("before load"))
	waitFor(t, 2*time.Second, "first owner check", func() bool { return h.cls.valCalls.Load() > 0 })

	// New flows arrive faster than Classify returns, so the pending queue is never empty.
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		tk := time.NewTicker(5 * time.Millisecond)
		defer tk.Stop()
		for port := uint16(20000); ; port++ {
			select {
			case <-stop:
				return
			case <-tk.C:
				h.tun.fromOS(syn(port, dst(remoteB, 443), 1))
			}
		}
	}()
	v0 := h.cls.valCalls.Load()
	time.Sleep(2 * time.Second)
	close(stop)
	<-done
	if n := h.cls.valCalls.Load() - v0; n < 5 {
		t.Errorf("%d owner checks in 2 s of classify load, want about one per validateEvery", n)
	}
	if st := h.c.Status(); st.BypassUDP != 1 || st.Counters.Torn != 0 {
		t.Fatalf("UDP session retired under classify load: %+v", st)
	}
	udpExchange(t, c, a, []byte("after load"), []byte("after load"))
	if n := h.eg.listens.Load(); n != 1 {
		t.Fatalf("listens = %d, want the same egress socket throughout", n)
	}
}

func TestMaxDatagramAtMinMTUFitsUDPQueues(t *testing.T) {
	const size, mtu = 65507, 1280
	lim := func(l *limits) {
		l.pendingTimeout = 10 * time.Second
		l.udpFresh = 50 * time.Millisecond
		l.validateEvery = time.Hour
		l.validateStale = time.Hour
	}
	queuedAll := func(h *harness, e *engine, port uint16, n int) func() bool {
		return func() bool {
			e.mu.Lock()
			defer e.mu.Unlock()
			f := e.udp[port]
			return f != nil && len(f.queue)+int(h.device().stats.udpDrops.Load()) == n
		}
	}
	t.Run("pending", func(t *testing.T) {
		h := newHarness(t, harnessOpts{kind: kindWindows, lim: lim})
		a := dst(remoteA, 9710)
		h.udpServer(a)
		h.cls.set(43200, true, appGame)
		h.setRules(appGame)
		h.tun.fromOS(tunnelUDP(40500, []byte("kick")))
		e := h.engine()
		release := h.cls.block()
		defer release()
		frags := fragment(udpPacket(dst(tunAddr, 43200), a, pattern(size)), 901, mtu)
		for _, f := range frags {
			h.tun.fromOS(f)
		}
		waitFor(t, 3*time.Second, "fragments queued on the pending port", queuedAll(h, e, 43200, len(frags)))
		release()
		waitFor(t, 3*time.Second, "datagram sent", func() bool { return len(h.eg.writesTo(a)) > 0 })
		if got := h.eg.writesTo(a); len(got) != 1 || got[0] != size {
			t.Fatalf("egress writes = %v, want [%d]", got, size)
		}
		if n := h.c.Status().Counters.UDPDrops; n != 0 {
			t.Fatalf("UDPDrops = %d, want 0", n)
		}
	})
	t.Run("held", func(t *testing.T) {
		h := newHarness(t, harnessOpts{kind: kindWindows, lim: lim})
		a, b := dst(remoteA, 9711), dst(remoteB, 9712)
		h.udpServer(a)
		h.udpServer(b)
		h.cls.set(43201, true, appGame)
		h.setRules(appGame)
		h.tun.fromOS(udpPacket(dst(tunAddr, 43201), a, []byte("first")))
		waitFor(t, 3*time.Second, "bypass session", func() bool { return len(h.eg.writesTo(a)) == 1 })
		waitFor(t, 2*time.Second, "first owner check", func() bool { return h.cls.valCalls.Load() > 0 })
		time.Sleep(100 * time.Millisecond)
		e := h.engine()
		release := h.cls.blockValidate()
		defer release()
		frags := fragment(udpPacket(dst(tunAddr, 43201), b, pattern(size)), 902, mtu)
		for _, f := range frags {
			h.tun.fromOS(f)
		}
		waitFor(t, 3*time.Second, "fragments held for the owner check", queuedAll(h, e, 43201, len(frags)))
		release()
		waitFor(t, 3*time.Second, "datagram sent", func() bool { return len(h.eg.writesTo(b)) > 0 })
		if got := h.eg.writesTo(b); len(got) != 1 || got[0] != size {
			t.Fatalf("egress writes = %v, want [%d]", got, size)
		}
		if n := h.c.Status().Counters.UDPDrops; n != 0 {
			t.Fatalf("UDPDrops = %d, want 0", n)
		}
	})
}

func TestICMPErrorsAboutBypassedFlowsAreDropped(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	h.eg.setTarget(h.server(echoConn))
	a := dst(remoteA, 9980)
	h.udpServer(a)
	h.cls.set(43180, true, appGame)
	h.cls.set(43181, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43180)
	udpExchange(t, c, a, []byte("x"), []byte("x"))
	tc, _ := h.mustDial(43181, dst(remoteA, 7980))
	roundTrip(t, tc, "y")

	h.tun.fromOS(icmpError(3, udpPacket(a, dst(tunAddr, 43180), []byte("reply"))))
	h.tun.fromOS(icmpError(11, tcpPacket(dst(remoteA, 7980), dst(tunAddr, 43181), header.TCPFlagAck, 1, nil)))
	h.tun.fromOS(icmpError(3, udpPacket(dst(remoteB, 4000), dst(tunAddr, 43189), []byte("unrelated"))))
	if !h.waitTunnel(2*time.Second, func(i pktInfo) bool { return i.proto == protoICMP }) {
		t.Fatal("unrelated ICMP error did not reach the tunnel")
	}
	n := 0
	for _, p := range h.tunnelPackets() {
		if info, ok := parseIPv4(p); ok && info.proto == protoICMP {
			n++
			if q, ok := icmpQuote(p[20:]); !ok || q.dstPort != 43189 {
				t.Fatal("an ICMP error about a bypassed flow reached the tunnel")
			}
		}
	}
	if n != 1 {
		t.Fatalf("%d ICMP errors in the tunnel, want 1", n)
	}
	tc.Close()
}

func TestControllerCloseClosesUDPSessions(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindDarwin})
	a := dst(remoteA, 9990)
	srv := h.udpServer(a)
	h.cls.set(43190, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43190)
	udpExchange(t, c, a, []byte("x"), []byte("x"))
	if err := h.c.Close(); err != nil {
		t.Fatal(err)
	}
	if st := h.c.Status(); st.BypassUDP != 0 {
		t.Fatalf("BypassUDP = %d after Controller.Close", st.BypassUDP)
	}
	send(t, c, a, "tunnel-only now")
	h.waitTunnelUDP(43190, 9990)
	_, _ = srv.conn.WriteToUDPAddrPort([]byte("late"), srv.lastSource(t))
	expectNoUDP(t, c, 200*time.Millisecond)
}

func TestUDPSendErrorsKeepSession(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, lim: func(l *limits) { l.unreachableEvery = time.Hour }})
	a := dst(remoteA, 9995)
	srv := h.udpServer(a)
	h.cls.set(43195, true, appGame)
	h.setRules(appGame)
	c := h.udpApp(43195)
	udpExchange(t, c, a, []byte("x"), []byte("x"))
	h.c.lastHint.Store(0)

	h.eg.setWriteErr(syscall.EACCES)
	send(t, c, a, "blocked")
	waitFor(t, 2*time.Second, "send failure counted", func() bool { return h.c.Status().Counters.UDPDrops > 0 })
	refreshes := h.eg.refreshes.Load()
	h.eg.setWriteErr(unreachableErr(t))
	send(t, c, a, "unreachable")
	waitFor(t, 2*time.Second, "network re-check after an unreachable send", func() bool { return h.eg.refreshes.Load() > refreshes })
	h.eg.setWriteErr(nil)
	udpExchange(t, c, a, []byte("y"), []byte("y"))
	if h.eg.listens.Load() != 1 || len(srv.sources()) != 2 || srv.sources()[0] != srv.sources()[1] {
		t.Fatal("send errors replaced the session")
	}
}

func TestUDPLogsCarryNoFlowData(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, lim: func(l *limits) {
		l.pendingTimeout = 200 * time.Millisecond
		l.listenTimeout = 200 * time.Millisecond
	}})
	servers := map[uint16]netip.AddrPort{}
	for i := uint16(0); i < 6; i++ {
		servers[i] = dst(remoteA, 9600+i)
		h.udpServer(servers[i])
	}
	h.setRules(appGame, appChat)
	for i := uint16(0); i < 6; i++ {
		h.cls.set(43200+i, i%2 == 0, appGame)
	}
	conns := make([]*gonet.UDPConn, 6)
	for i := uint16(0); i < 6; i++ {
		conns[i] = h.udpApp(43200 + i)
		send(t, conns[i], servers[i], "hello")
		send(t, conns[i], dst(remoteB, 9700+i), "fan")
	}
	waitFor(t, 3*time.Second, "sessions", func() bool { return h.c.Status().BypassUDP == 3 })
	h.eg.setWriteErr(unreachableErr(t))
	send(t, conns[0], servers[0], "unreachable")
	time.Sleep(100 * time.Millisecond)
	h.eg.setWriteErr(nil)
	h.cls.setOwner(43202, 1, false, appChat)
	h.eg.setIdentity(egress.Identity{Index: 9, Name: "wifi9", Addrs: []netip.Addr{netip.MustParseAddr("192.0.2.99")}}, nil)
	h.c.NetworkChanged()
	h.eg.mu.Lock()
	h.eg.listenHook = func(ctx context.Context) (net.PacketConn, error) { <-ctx.Done(); return nil, ctx.Err() }
	h.eg.mu.Unlock()
	h.cls.set(43210, true, appGame)
	send(t, h.udpApp(43210), dst(remoteC, 9800), "no socket")
	time.Sleep(400 * time.Millisecond)
	h.tun.fromOS(fragment(udpPacket(dst(tunAddr, 43211), dst(remoteC, 9801), pattern(3000)), 5, testMTU)[1])
	h.setRules(appChat)
	h.close()

	lines := h.logs.all()
	if len(lines) > 15 {
		t.Fatalf("%d log lines; logging must not scale with traffic:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	forbidden := []string{tunAddr.String(), remoteA.String(), remoteB.String(), remoteC.String(), "127.0.0.1", "192.0.2.99", "/apps/"}
	for p := 9600; p < 9810; p++ {
		forbidden = append(forbidden, strconv.Itoa(p))
	}
	for p := 43200; p < 43212; p++ {
		forbidden = append(forbidden, strconv.Itoa(p))
	}
	for _, l := range lines {
		for _, f := range forbidden {
			if strings.Contains(l, f) {
				t.Errorf("log line %q leaks %q", l, f)
			}
		}
	}
	t.Logf("captured log:\n%s", strings.Join(lines, "\n"))
}

func TestBuildUDPReply(t *testing.T) {
	from, to := dst(remoteA, 5353), dst(tunAddr, 40000)
	var id uint16
	next := func() uint16 { id++; return id }
	for _, size := range []int{0, 1, 1391, 1392, 1393, 4000, maxUDPPayload} {
		payload := pattern(size)
		pkts, ok := buildUDPReply(nil, from, to, payload, testMTU, next)
		if !ok {
			t.Fatalf("size %d refused", size)
		}
		var whole []byte
		for i, p := range pkts {
			ip := header.IPv4(p.data[writeHeadroom:])
			if !ip.IsValid(len(ip)) || checksum.Checksum(ip[:20], 0) != 0xffff || len(ip) > testMTU {
				t.Fatalf("size %d: bad packet %d", size, i)
			}
			if ip.TTL() != 64 || ip.Flags()&header.IPv4FlagDontFragment != 0 {
				t.Fatalf("size %d: TTL %d flags %#x", size, ip.TTL(), ip.Flags())
			}
			if int(ip.FragmentOffset()) != len(whole) || ip.More() != (i < len(pkts)-1) {
				t.Fatalf("size %d: fragment %d offset/MF wrong", size, i)
			}
			if len(pkts) == 1 && ip.ID() != 0 {
				t.Fatalf("size %d: unfragmented reply used IP ID %d", size, ip.ID())
			}
			whole = append(whole, ip.Payload()...)
			p.release()
		}
		u := header.UDP(whole)
		xsum := header.PseudoHeaderChecksum(header.UDPProtocolNumber, tcpipAddr(remoteA), tcpipAddr(tunAddr), uint16(len(whole)))
		if u.Checksum() == 0 || checksum.Checksum(whole, xsum) != 0xffff {
			t.Fatalf("size %d: bad UDP checksum", size)
		}
		if int(u.Length()) != len(whole) || u.SourcePort() != 5353 || u.DestinationPort() != 40000 || !bytes.Equal(u.Payload(), payload) {
			t.Fatalf("size %d: reassembled datagram differs", size)
		}
	}
	if _, ok := buildUDPReply(nil, from, to, make([]byte, maxUDPPayload+1), testMTU, next); ok {
		t.Fatal("oversized payload accepted")
	}
}

func TestFragmentTrackerHoldsUntilFirstOrTimeout(t *testing.T) {
	e := newDecideEngine(t)
	now := time.Now()
	raw := fragment(udpPacket(dst(tunAddr, 5100), dst(remoteA, 9), pattern(3000)), 99, testMTU)
	var sp spill
	for _, i := range []int{2, 1} {
		if act := e.decide(raw[i], now, &sp); act != actHeld {
			t.Fatalf("orphan fragment %d: action %d, want held", i, act)
		}
	}
	e.mu.Lock()
	held := e.frags.len()
	e.mu.Unlock()
	if held != 1 || len(sp.items) != 0 {
		t.Fatalf("tracker holds %d records", held)
	}
	e.sweep(now.Add(3 * time.Second))
	for _, want := range [][]byte{raw[2], raw[1]} {
		select {
		case p := <-e.out:
			if !bytes.Equal(p.data, want) {
				t.Fatal("flushed fragment differs")
			}
			p.release()
		default:
			t.Fatal("expired fragments were not flushed to the tunnel")
		}
	}

	e.lim.fragBytes = 2 * engineBufSize
	raw = fragment(udpPacket(dst(tunAddr, 5101), dst(remoteA, 9), pattern(6000)), 100, testMTU)
	acts := []action{}
	for _, p := range raw[1:4] {
		acts = append(acts, e.decide(p, now, &sp))
	}
	if acts[0] != actHeld || acts[1] != actHeld || acts[2] != actDrop || len(sp.items) != 3 {
		t.Fatalf("byte cap: actions %v, %d spilled", acts, len(sp.items))
	}
	for _, it := range sp.items {
		if it.act != actTunnel {
			t.Fatal("over-cap fragments must go to the tunnel")
		}
	}
	sp.release()
	if act := e.decide(raw[4], now, &sp); act != actTunnel {
		t.Fatalf("fragment of a flushed datagram: action %d", act)
	}
}

func tcpipAddr(a netip.Addr) tcpip.Address { return tcpip.AddrFrom4(a.As4()) }

// unreachableErr is an error egress.IsUnreachable recognises on this OS.
func unreachableErr(t testing.TB) error {
	t.Helper()
	for _, e := range []syscall.Errno{syscall.Errno(10051), syscall.ENETUNREACH} {
		if egress.IsUnreachable(e) {
			return &net.OpError{Op: "write", Net: "udp", Err: e}
		}
	}
	t.Skip("no unreachable errno on this OS")
	return nil
}

func TestUDPListenUnreachableRechecksNetwork(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux})
	a := dst(remoteA, 9996)
	h.cls.set(43196, true, appGame)
	h.setRules(appGame)
	h.c.lastHint.Store(0)
	fail := unreachableErr(t)
	h.eg.mu.Lock()
	h.eg.listenHook = func(context.Context) (net.PacketConn, error) { return nil, fail }
	h.eg.mu.Unlock()
	refreshes := h.eg.refreshes.Load()
	c := h.udpApp(43196)
	send(t, c, a, "socket fails")
	waitFor(t, 2*time.Second, "network re-check after an unreachable listen", func() bool { return h.eg.refreshes.Load() > refreshes })
}
