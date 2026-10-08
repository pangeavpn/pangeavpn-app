package splittunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/tcp"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
)

func TestTCPBypassEchoAndHalfClose(t *testing.T) {
	for _, kind := range allKinds {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind})
			h.eg.setTarget(h.server(echoThenBye))
			h.cls.set(40001, true, appGame)
			h.setRules(appGame)

			c, _ := h.mustDial(40001, dst(remoteA, 7000))
			roundTrip(t, c, "hello through the physical interface")
			big := strings.Repeat("0123456789abcdef", 32<<10)
			errc := make(chan error, 1)
			go func() {
				_, err := c.Write([]byte(big))
				errc <- err
			}()
			got := make([]byte, len(big))
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.ReadFull(c, got); err != nil {
				t.Fatalf("read bulk echo: %v", err)
			}
			if err := <-errc; err != nil {
				t.Fatalf("bulk write: %v", err)
			}
			if string(got) != big {
				t.Fatal("bulk echo corrupted")
			}

			if st := h.c.Status(); st.BypassTCP != 1 || !st.AppsActive || st.Counters.Bypassed != 1 {
				t.Fatalf("status during bypass = %+v", st)
			}
			if err := c.CloseWrite(); err != nil {
				t.Fatalf("CloseWrite: %v", err)
			}
			rest, err := io.ReadAll(c)
			if err != nil || string(rest) != "bye" {
				t.Fatalf("after half-close read %q, %v; want \"bye\" then EOF", rest, err)
			}
			c.Close()
			if h.tunnelHasPort(40001) {
				t.Fatal("packets of a bypassed flow reached the tunnel")
			}
			if n := h.eg.dials.Load(); n != 1 {
				t.Fatalf("egress dials = %d, want 1", n)
			}
			waitFor(t, 3*time.Second, "bypass slot release", func() bool { return h.c.Status().BypassTCP == 0 })
		})
	}
}

func TestTCPResetFromServerReachesApp(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	accepted := make(chan net.Conn, 1)
	h.eg.setTarget(h.server(func(c net.Conn) { accepted <- c }))
	h.cls.set(40002, true, appGame)
	h.setRules(appGame)

	c, _ := h.mustDial(40002, dst(remoteA, 7001))
	var sc net.Conn
	select {
	case sc = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted")
	}
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = sc.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(sc, buf); err != nil {
		t.Fatalf("server read: %v", err)
	}
	resetConn(sc)
	expectReset(t, c)
}

func TestTCPResetFromAppReachesServer(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux})
	accepted := make(chan net.Conn, 1)
	h.eg.setTarget(h.server(func(c net.Conn) { accepted <- c }))
	h.cls.set(40003, true, appGame)
	h.setRules(appGame)

	c, ep := h.mustDial(40003, dst(remoteA, 7002))
	var sc net.Conn
	select {
	case sc = <-accepted:
	case <-time.After(5 * time.Second):
		t.Fatal("server never accepted")
	}
	defer sc.Close()
	roundTripTo(t, c, sc, "ping")
	ep.Abort()
	expectReset(t, sc)
}

// roundTripTo sends msg from the app and reads it at the server.
func roundTripTo(t *testing.T, app, server net.Conn, msg string) {
	t.Helper()
	if _, err := app.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, len(msg))
	_ = server.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(server, buf); err != nil || string(buf) != msg {
		t.Fatalf("server read %q, %v", buf, err)
	}
	_ = server.SetReadDeadline(time.Time{})
}

func TestTunnelFlowsReachRead(t *testing.T) {
	for _, kind := range allKinds {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind, peer: true})
			c, _ := h.mustDial(40010, dst(remoteB, 8000))
			roundTrip(t, c, "direct mode, before any rule")
			c.Close()
			if h.device().pumping.Load() {
				t.Fatal("pump started without rules")
			}

			h.cls.set(40011, false, appChat)
			h.setRules(appGame)
			c, _ = h.mustDial(40011, dst(remoteB, 8001))
			roundTrip(t, c, "pump mode, tunnel verdict")
			big := strings.Repeat("t", 300<<10)
			go func() { _, _ = c.Write([]byte(big)) }()
			got := make([]byte, len(big))
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
			if _, err := io.ReadFull(c, got); err != nil || string(got) != big {
				t.Fatalf("bulk tunnel echo: %v", err)
			}
			c.Close()
			if !h.device().pumping.Load() {
				t.Fatal("pump not started after rules")
			}
			if !h.tunnelHasPort(40011) || !h.cls.classifiedPort(40011) {
				t.Fatal("tunnel flow was not classified and read from the wrapper")
			}
			if n := h.eg.dials.Load(); n != 0 {
				t.Fatalf("egress dials = %d, want 0", n)
			}
		})
	}
}

func TestAlwaysTunnelTraffic(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, peer: true})
	h.eg.setTarget(h.server(echoConn))
	for _, p := range []uint16{40020, 40021, 40022} {
		h.cls.set(p, true, appGame)
	}
	h.setRules(appGame)

	c, _ := h.mustDial(40020, dst(remoteA, 53))
	roundTrip(t, c, "dns over tcp")
	c.Close()
	c, _ = h.mustDial(40021, dst(tunDNS, 443))
	roundTrip(t, c, "tunnel resolver")
	c.Close()
	c, _ = h.mustDial(40022, dst(remoteA, 853))
	roundTrip(t, c, "dot")
	c.Close()

	odd := [][]byte{
		syn(40023, dst(netip.MustParseAddr("224.0.0.1"), 80), 1),
		syn(40024, dst(netip.MustParseAddr("255.255.255.255"), 80), 1),
		syn(40025, dst(netip.MustParseAddr("127.0.0.1"), 80), 1),
		syn(40026, dst(netip.MustParseAddr("0.1.2.3"), 80), 1),
		tcpPacket(dst(netip.MustParseAddr("10.9.9.9"), 40027), dst(remoteA, 80), header.TCPFlagSyn, 1, nil),
		udpPacket(dst(tunAddr, 40028), dst(remoteA, 853), []byte("dns over quic")),
		ipv4Packet(tunAddr, remoteA, protoICMP, []byte{8, 0, 0, 0, 0, 1, 0, 1}),
	}
	for _, p := range odd {
		h.tun.fromOS(p)
	}
	for _, port := range []uint16{40023, 40024, 40025, 40026, 40027, 40028} {
		if !h.waitTunnel(2*time.Second, func(i pktInfo) bool { return i.srcPort == port }) {
			t.Fatalf("packet from port %d did not reach the tunnel", port)
		}
	}
	if !h.waitTunnel(2*time.Second, func(i pktInfo) bool { return i.proto == protoICMP }) {
		t.Fatal("ICMP did not reach the tunnel")
	}
	for _, port := range []uint16{40020, 40021, 40022, 40023, 40024, 40025, 40026, 40027, 40028} {
		if h.cls.classifiedPort(port) {
			t.Errorf("port %d was classified; always-tunnel traffic must skip lookups", port)
		}
	}
	if n := h.eg.dials.Load(); n != 0 {
		t.Fatalf("egress dials = %d, want 0", n)
	}
}

func TestPendingTimeoutGoesToTunnel(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true, lim: func(l *limits) { l.pendingTimeout = 300 * time.Millisecond }})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40030, true, appGame)
	h.setRules(appGame)
	release := h.cls.block()
	defer release()

	start := time.Now()
	c, _ := h.mustDial(40030, dst(remoteA, 7030))
	if d := time.Since(start); d < 250*time.Millisecond {
		t.Fatalf("connected after %v; the SYN should have waited for the pending deadline", d)
	}
	roundTrip(t, c, "fail-safe tunnel")
	if !h.tunnelHasPort(40030) || h.eg.dials.Load() != 0 {
		t.Fatal("timed-out flow did not go to the tunnel")
	}
	if n := h.c.Status().Counters.PendingTimeouts; n != 1 {
		t.Fatalf("PendingTimeouts = %d, want 1", n)
	}
	release()
	c.Close()

	time.Sleep(150 * time.Millisecond)
	c, _ = h.mustDial(40030, dst(remoteA, 7030))
	roundTrip(t, c, "reclassified on the next connection")
	c.Close()
	if n := h.eg.dials.Load(); n != 1 {
		t.Fatalf("egress dials = %d after the classifier recovered, want 1", n)
	}
}

func TestClassifierFailuresGoToTunnel(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindDarwin, peer: true})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40040, true, appGame)
	h.cls.set(40041, true, appGame)
	h.setRules(appGame)

	h.cls.fail.Store(true)
	c, _ := h.mustDial(40040, dst(remoteA, 7040))
	roundTrip(t, c, "lookup error")
	c.Close()
	h.cls.fail.Store(false)

	h.cls.panics.Store(true)
	c, _ = h.mustDial(40041, dst(remoteA, 7041))
	roundTrip(t, c, "lookup panic")
	c.Close()
	h.cls.panics.Store(false)

	if !h.tunnelHasPort(40040) || !h.tunnelHasPort(40041) || h.eg.dials.Load() != 0 {
		t.Fatal("failed lookups did not fall back to the tunnel")
	}
	if n := h.c.Status().Counters.LookupFails; n < 2 {
		t.Fatalf("LookupFails = %d, want >= 2", n)
	}
	if h.device().tunnelOnly.Load() {
		t.Fatal("a classifier panic must not disable the engine")
	}
	h.cls.set(40042, true, appGame)
	c, _ = h.mustDial(40042, dst(remoteA, 7042))
	roundTrip(t, c, "bypass works again")
	c.Close()
	if h.eg.dials.Load() != 1 {
		t.Fatal("bypass did not recover after classifier errors")
	}
}

func TestPendingCapsSendNewFlowsToTunnel(t *testing.T) {
	for _, tc := range []struct {
		name string
		lim  func(*limits)
	}{
		{"flows", func(l *limits) { l.pendingFlows = 1; l.pendingTimeout = 3 * time.Second }},
		{"bytes", func(l *limits) { l.pendingBytes = engineBufSize; l.pendingTimeout = 3 * time.Second }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kindWindows, peer: true, lim: tc.lim})
			h.eg.setTarget(h.server(echoConn))
			h.cls.set(40050, true, appGame)
			h.cls.set(40051, true, appGame)
			h.setRules(appGame)
			release := h.cls.block()
			defer release()

			first := make(chan error, 1)
			go func() {
				c, _, err := h.dial(40050, dst(remoteA, 7050), 5*time.Second)
				if c != nil {
					c.Close()
				}
				first <- err
			}()
			waitFor(t, 2*time.Second, "first flow pending", func() bool {
				e := h.device().eng.Load()
				if e == nil {
					return false
				}
				e.mu.Lock()
				defer e.mu.Unlock()
				return e.pending == 1
			})
			start := time.Now()
			c, _ := h.mustDial(40051, dst(remoteA, 7051))
			if d := time.Since(start); d > time.Second {
				t.Fatalf("flow over the pending cap waited %v", d)
			}
			roundTrip(t, c, "over the cap")
			c.Close()
			if !h.tunnelHasPort(40051) || h.cls.classifiedPort(40051) {
				t.Fatal("flow over the pending cap was classified instead of tunnelled")
			}
			release()
			if err := <-first; err != nil {
				t.Fatalf("first flow: %v", err)
			}
		})
	}
}

func TestBypassCapSendsExtraFlowsToTunnel(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, peer: true, lim: func(l *limits) { l.bypassTCP = 1 }})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40060, true, appGame)
	h.cls.set(40061, true, appGame)
	h.setRules(appGame)

	c1, _ := h.mustDial(40060, dst(remoteA, 7060))
	roundTrip(t, c1, "first bypass")
	c2, _ := h.mustDial(40061, dst(remoteA, 7061))
	roundTrip(t, c2, "second goes to the tunnel")
	if h.eg.dials.Load() != 1 || !h.tunnelHasPort(40061) || h.tunnelHasPort(40060) {
		t.Fatal("bypass cap not enforced")
	}
	c1.Close()
	c2.Close()
}

func TestSYNWithNewISNOnLingeringEntryReclassifies(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
	h.eg.setTarget(h.server(echoThenBye))
	h.cls.set(40070, true, appGame)
	h.setRules(appGame)
	key := flowKey{srcPort: 40070, dst: dst(remoteA, 7070)}

	c, _ := h.mustDial(40070, dst(remoteA, 7070))
	roundTrip(t, c, "first connection bypasses")

	e := h.engine()
	e.mu.Lock()
	live := e.tcp[key]
	isn := live.isn
	e.mu.Unlock()
	h.tun.fromOS(syn(40070, key.dst, isn))
	time.Sleep(50 * time.Millisecond)
	e.mu.Lock()
	same := e.tcp[key] == live && live.state == stateBypass
	e.mu.Unlock()
	if !same {
		t.Fatal("a retransmitted SYN with the same ISN replaced the live entry")
	}
	roundTrip(t, c, "still alive after duplicate SYN")

	_ = c.CloseWrite()
	_, _ = io.ReadAll(c)
	c.Close()
	waitFor(t, 3*time.Second, "entry lingering", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		f := e.tcp[key]
		return f != nil && f.state == stateLinger
	})

	h.cls.set(40070, false, appChat)
	time.Sleep(150 * time.Millisecond)
	c, _ = h.mustDial(40070, dst(remoteA, 7070))
	roundTrip(t, c, "port reused by a tunnelled app")
	c.Close()
	if h.eg.dials.Load() != 1 {
		t.Fatalf("egress dials = %d; the reused port must not inherit the bypass", h.eg.dials.Load())
	}
	if !h.tunnelHasPort(40070) {
		t.Fatal("new connection on the lingering key did not reach the tunnel")
	}
}

func TestForwarderRefusesSYNWithoutDialMark(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	h.eg.setTarget(h.server(echoConn))
	h.setRules(appGame)
	h.tun.fromOS(tunnelUDP(40080, []byte("start the pump")))
	e := h.engine()

	e.link.deliver(syn(40081, dst(remoteA, 7081), 99))
	waitFor(t, 2*time.Second, "RST for the unmarked SYN", func() bool {
		for _, p := range h.tun.writtenPackets() {
			if info, ok := parseIPv4(p); ok && info.dstPort == 40081 && info.flags&tcpFlagRst != 0 {
				return true
			}
		}
		return false
	})
	if n := h.eg.dials.Load(); n != 0 {
		t.Fatalf("forwarder dialled %d times for a SYN that was never classified", n)
	}
}

func TestSYNIntoGVisorTimeWaitConnects(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	h.eg.setTarget(h.server(func(c net.Conn) {
		defer c.Close()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c, buf); err == nil {
			_, _ = c.Write([]byte("pong"))
		}
	}))
	h.cls.set(40090, true, appGame)
	h.setRules(appGame)
	to := dst(remoteA, 7090)

	c, _ := h.mustDial(40090, to)
	roundTripPong(t, c)
	if b, err := io.ReadAll(c); err != nil || len(b) != 0 {
		t.Fatalf("want EOF after server close, got %q %v", b, err)
	}
	c.Close()
	waitFor(t, 3*time.Second, "engine endpoint in TIME_WAIT", func() bool {
		e := h.device().eng.Load()
		e.mu.Lock()
		defer e.mu.Unlock()
		f := e.tcp[flowKey{srcPort: 40090, dst: to}]
		return f != nil && f.state == stateLinger
	})

	start := time.Now()
	c, _, err := h.dial(40090, to, 8*time.Second)
	if err != nil {
		t.Fatalf("reconnect on a TIME_WAIT tuple: %v", err)
	}
	elapsed := time.Since(start)
	t.Logf("reconnect through gVisor TIME_WAIT took %v", elapsed)
	if elapsed > 4500*time.Millisecond {
		t.Fatalf("reconnect took %v; TIME_WAIT must be short", elapsed)
	}
	roundTripPong(t, c)
	c.Close()
}

func roundTripPong(t *testing.T, c net.Conn) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "pong" {
		t.Fatalf("read %q, %v", buf, err)
	}
	_ = c.SetDeadline(time.Time{})
}

func TestSetRulesResetsOnlyRemovedApps(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40100, true, appGame)
	h.cls.set(40101, true, appChat)
	h.setRules(appGame, appChat)

	game, _ := h.mustDial(40100, dst(remoteA, 7100))
	chat, _ := h.mustDial(40101, dst(remoteA, 7101))
	roundTrip(t, game, "game")
	roundTrip(t, chat, "chat")

	activeLines := func() int {
		n := 0
		for _, l := range h.logs.all() {
			if strings.Contains(l, "app rules active") {
				n++
			}
		}
		return n
	}
	before := activeLines()
	h.setRules(appChat)
	expectReset(t, game)
	roundTrip(t, chat, "chat survives an unrelated rule edit")
	if n := h.c.Status().Counters.Torn; n != 1 {
		t.Fatalf("Torn = %d, want 1", n)
	}

	h.setRules(appChat)
	if n := h.c.Status().Counters.Torn; n != 1 || activeLines() != before+1 {
		t.Fatalf("identical SetRules was not a no-op (torn=%d)", n)
	}
	chat.Close()
}

func TestSetRulesResetsBeforePermitIsWithdrawn(t *testing.T) {
	var (
		mu    sync.Mutex
		calls []bool
		torn  = map[bool]uint64{}
		h     *harness
	)
	permit := func(_ context.Context, on bool) error {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, on)
		torn[on] = h.c.Status().Counters.Torn
		return nil
	}
	h = newHarness(t, harnessOpts{kind: kindWindows, permit: permit})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40110, true, appGame)
	h.setRules(appGame)
	waitFor(t, 2*time.Second, "permit granted", func() bool { return h.c.permitted.Load() })

	c, _ := h.mustDial(40110, dst(remoteA, 7110))
	roundTrip(t, c, "bypassed")
	h.setRules()
	expectReset(t, c)
	waitFor(t, 2*time.Second, "permit withdrawn", func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(calls) == 2
	})
	mu.Lock()
	defer mu.Unlock()
	if calls[0] != true || calls[1] != false {
		t.Fatalf("permit calls = %v", calls)
	}
	if torn[false] != 1 {
		t.Fatalf("permit withdrawn before the bypassed connection was reset (torn=%d)", torn[false])
	}
}

func TestEgressPermitFailureKeepsAppsInTunnel(t *testing.T) {
	var fail atomic.Bool
	fail.Store(true)
	var calls atomic.Int64
	permit := func(_ context.Context, on bool) error {
		calls.Add(1)
		if on && fail.Load() {
			return errors.New("filter add failed")
		}
		return nil
	}
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true, permit: permit})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40120, true, appGame)
	h.cls.set(40121, true, appGame)
	h.setRules(appGame)
	waitFor(t, 2*time.Second, "permit attempt", func() bool { return calls.Load() >= 1 })

	c, _ := h.mustDial(40120, dst(remoteA, 7120))
	roundTrip(t, c, "gate closed")
	c.Close()
	if !h.tunnelHasPort(40120) || h.cls.classifiedPort(40120) || h.eg.dials.Load() != 0 {
		t.Fatal("flow bypassed while the egress permit was missing")
	}
	if st := h.c.Status(); st.UnavailableReason != ReasonPermitFailed || st.AppsActive {
		t.Fatalf("status = %+v, want permitFailed", st)
	}
	h.cls.set(40122, true, appGame)
	u := h.udpApp(40122)
	send(t, u, dst(remoteA, 7122), "udp while the gate is closed")
	h.waitTunnelUDP(40122, 7122)
	if h.cls.classifiedPort(40122) || h.eg.listens.Load() != 0 {
		t.Fatal("UDP bypassed while the egress permit was missing")
	}

	fail.Store(false)
	h.c.NetworkChanged()
	waitFor(t, 2*time.Second, "permit retried", func() bool { return h.c.Status().AppsActive })
	if st := h.c.Status(); st.UnavailableReason != "" {
		t.Fatalf("reason after recovery = %q", st.UnavailableReason)
	}
	c, _ = h.mustDial(40121, dst(remoteA, 7121))
	roundTrip(t, c, "gate open")
	c.Close()
	if h.eg.dials.Load() != 1 {
		t.Fatal("flow did not bypass once the permit was granted")
	}
}

func TestDialFailureRefusesApp(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	h.cls.set(40130, true, appGame)
	h.setRules(appGame)
	_, _, err := h.dial(40130, dst(remoteA, 7130), 5*time.Second)
	if err == nil {
		t.Fatal("connect succeeded although the off-tunnel dial failed")
	}
	if h.tunnelHasPort(40130) {
		t.Fatal("refused bypass flow leaked into the tunnel")
	}
	e := h.engine()
	waitFor(t, 2*time.Second, "dial-failure linger", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		f := e.tcp[flowKey{srcPort: 40130, dst: dst(remoteA, 7130)}]
		return f != nil && f.state == stateLinger && !f.live && f.deadline.Sub(f.endedAt) == e.lim.dialFailLinger
	})
}

func TestRulesChangeDuringClassifyRecomputesVerdict(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(40140, true, appGame)
	h.cls.set(40141, false, appChat)
	h.setRules(appGame)
	h.tun.fromOS(tunnelUDP(40142, []byte("kick")))
	h.engine()

	type dialed struct {
		c   net.Conn
		err error
	}
	dialDuringRuleChange := func(port uint16, to netip.AddrPort, apps ...string) {
		t.Helper()
		release := h.cls.block()
		defer release()
		done := make(chan dialed, 1)
		go func() {
			c, _, err := h.dial(port, to, 5*time.Second)
			done <- dialed{c, err}
		}()
		waitFor(t, 2*time.Second, "classify in flight", func() bool { return h.cls.classifiedPort(port) })
		h.setRules(apps...)
		release()
		r := <-done
		if r.err != nil {
			t.Fatal(r.err)
		}
		roundTrip(t, r.c, "verdict for the rules in force")
		r.c.Close()
	}

	dialDuringRuleChange(40140, dst(remoteA, 7140), appChat)
	if h.eg.dials.Load() != 0 || !h.tunnelHasPort(40140) {
		t.Fatal("a bypass verdict for removed rules was installed")
	}
	dialDuringRuleChange(40141, dst(remoteA, 7141), appGame, appChat)
	if h.eg.dials.Load() != 1 || h.tunnelHasPort(40141) {
		t.Fatal("verdict was not recomputed for the rules in force")
	}
}

func TestStackFailureKeepsDeviceTunnelOnly(t *testing.T) {
	orig := newEngineStack
	newEngineStack = func(*linkEndpoint, limits, func(*tcp.ForwarderRequest)) (*stack.Stack, error) {
		return nil, errors.New("stack unavailable")
	}
	defer func() { newEngineStack = orig }()
	h := newHarness(t, harnessOpts{kind: kindLinux, peer: true})
	h.cls.set(40150, true, appGame)
	h.setRules(appGame)
	c, _ := h.mustDial(40150, dst(remoteA, 7150))
	roundTrip(t, c, "tunnel-only device")
	c.Close()
	if h.device().pumping.Load() || !h.tunnelHasPort(40150) {
		t.Fatal("device without a stack must stay in direct mode")
	}
	if st := h.c.Status(); st.UnavailableReason != ReasonStackFailed || st.AppsActive {
		t.Fatalf("status = %+v, want stackFailed", st)
	}
}

func TestEndedBypassKeyDropsStraySegments(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true, lim: func(l *limits) {
		l.linger = 100 * time.Millisecond
		l.scanEvery = 50 * time.Millisecond
	}})
	h.eg.setTarget(h.server(echoThenBye))
	h.cls.set(40160, true, appGame)
	h.setRules(appGame)
	to := dst(remoteA, 7160)
	key := flowKey{srcPort: 40160, dst: to}

	c, _ := h.mustDial(40160, to)
	roundTrip(t, c, "bypassed")
	_ = c.CloseWrite()
	_, _ = io.ReadAll(c)
	c.Close()
	e := h.engine()
	waitFor(t, 3*time.Second, "key in the ended table", func() bool {
		e.mu.Lock()
		defer e.mu.Unlock()
		return e.tcp[key] == nil && e.endedTCP.get(key, time.Now()) != nil
	})

	h.tun.fromOS(tcpPacket(dst(tunAddr, 40160), to, header.TCPFlagAck|header.TCPFlagRst, 5, nil))
	h.tun.fromOS(tunnelUDP(40161, []byte("marker")))
	if !h.waitTunnel(2*time.Second, func(i pktInfo) bool { return i.srcPort == 40161 }) {
		t.Fatal("marker never reached the tunnel")
	}
	if h.tunnelHasPort(40160) {
		t.Fatal("a stray segment of an ended bypass connection went into the tunnel")
	}

	h.cls.set(40160, false, appChat)
	time.Sleep(150 * time.Millisecond)
	c, _ = h.mustDial(40160, to)
	roundTrip(t, c, "a new SYN on the ended key is classified again")
	c.Close()
	if !h.tunnelHasPort(40160) || h.eg.dials.Load() != 1 {
		t.Fatal("new connection on an ended key was not reclassified")
	}
}

func TestTunnelEntryAbsorbsSYNRetransmits(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux})
	h.cls.set(40170, false, appChat)
	h.setRules(appGame)
	to := dst(remoteA, 7170)
	h.tun.fromOS(syn(40170, to, 1000))
	if !h.waitTunnel(2*time.Second, func(i pktInfo) bool { return i.srcPort == 40170 }) {
		t.Fatal("first SYN never reached the tunnel")
	}
	before := h.cls.flows.Load()
	h.tun.fromOS(syn(40170, to, 1000))
	h.tun.fromOS(tcpPacket(dst(tunAddr, 40170), to, header.TCPFlagAck, 1001, []byte("data")))
	waitFor(t, 2*time.Second, "retransmit and data in the tunnel", func() bool {
		n := 0
		for _, p := range h.tunnelPackets() {
			if info, ok := parseIPv4(p); ok && info.srcPort == 40170 {
				n++
			}
		}
		return n == 3
	})
	if h.cls.flows.Load() != before {
		t.Fatal("a SYN retransmit with the same ISN was classified again")
	}
	h.tun.fromOS(syn(40170, to, 5000))
	waitFor(t, 2*time.Second, "new ISN classified", func() bool { return h.cls.flows.Load() == before+1 })
}

func TestDialFinishingAfterCloseIsClosed(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows})
	accepted := make(chan net.Conn, 1)
	target := h.server(func(c net.Conn) { accepted <- c })
	dialing := make(chan struct{})
	h.eg.setHook(func(ctx context.Context, _ netip.AddrPort) (net.Conn, error) {
		close(dialing)
		<-ctx.Done()
		return net.Dial("tcp4", target)
	})
	h.cls.set(40180, true, appGame)
	h.setRules(appGame)
	h.tun.fromOS(syn(40180, dst(remoteA, 7180), 1))
	select {
	case <-dialing:
	case <-time.After(3 * time.Second):
		t.Fatal("dial never started")
	}
	_ = h.dev.Close()
	var sc net.Conn
	select {
	case sc = <-accepted:
	case <-time.After(3 * time.Second):
		t.Fatal("late dial never connected")
	}
	defer sc.Close()
	_ = sc.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := sc.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection dialled after Close was left open")
	} else if ne, ok := err.(net.Error); ok && ne.Timeout() {
		t.Fatal("connection dialled after Close was left open")
	}
}

type panicMatcher struct{ fakeRules }

func (panicMatcher) MatchChain([]string) bool { panic("matcher bug") }
func (panicMatcher) sameRules(matcher) bool   { return false }

func panicCompile(apps, never []string) (*ruleSet, []procmatch.RuleError) {
	rs, errs := fakeCompile(apps, never)
	return &ruleSet{match: panicMatcher{rs.match.(fakeRules)}, apps: rs.apps}, errs
}

// expectFaultedTunnelOnly checks a panic inside a locked engine section left the device
// tunnel-only with its flow mutex free, so tunnel traffic still flows.
func expectFaultedTunnelOnly(t *testing.T, h *harness, e *engine, port uint16) {
	t.Helper()
	for deadline := time.Now().Add(2 * time.Second); !h.device().tunnelOnly.Load() && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	free := false
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if free = e.mu.TryLock(); free {
			break
		}
	}
	// Releases our TryLock, or the lock a panicking section left held so cleanup can finish.
	e.mu.Unlock()
	if !free {
		t.Fatal("a recovered panic left the flow mutex held")
	}
	if !h.device().tunnelOnly.Load() {
		t.Fatal("the device did not switch to tunnel-only after the panic")
	}
	c, _ := h.mustDial(port, dst(remoteB, 7400))
	roundTrip(t, c, "tunnel after the fault")
	c.Close()
}

func expectQuickClose(t *testing.T, h *harness) {
	t.Helper()
	start := time.Now()
	if err := h.dev.Close(); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Device.Close took %v after the fault", d)
	}
}

// gVisor runs handleTCP on a goroutine of its own, outside safely: a panic in the bypass
// dial must still end in tunnel-only operation, not a crashed daemon.
func TestPanicInBypassDialFailsSafe(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
	h.eg.mu.Lock()
	h.eg.hook = func(context.Context, netip.AddrPort) (net.Conn, error) { panic("egress bug") }
	h.eg.mu.Unlock()
	h.cls.set(41270, true, appGame)
	h.setRules(appGame)
	go func() {
		if c, _, _ := h.dial(41270, dst(remoteA, 7270), 3*time.Second); c != nil {
			c.Close()
		}
	}()
	expectFaultedTunnelOnly(t, h, h.engine(), 41271)
}

func TestPanicInLockedEngineSectionFailsSafe(t *testing.T) {
	t.Run("worker", func(t *testing.T) {
		h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
		h.eg.setTarget(h.server(echoConn))
		h.cls.set(41250, true, appGame)
		h.setRules(appGame)
		release := h.cls.block()
		defer release()
		dialed := make(chan error, 1)
		go func() {
			c, _, err := h.dial(41250, dst(remoteA, 7250), 5*time.Second)
			if c != nil {
				c.Close()
			}
			dialed <- err
		}()
		waitFor(t, 2*time.Second, "classify in flight", func() bool { return h.cls.classifiedPort(41250) })
		e := h.engine()
		h.c.compile = panicCompile
		h.c.SetRules([]string{appGame, appChat}, nil)
		release()
		expectFaultedTunnelOnly(t, h, e, 41251)
		if err := <-dialed; err != nil {
			t.Fatalf("flow pending during the fault: %v", err)
		}
		expectQuickClose(t, h)
	})
	t.Run("setRules", func(t *testing.T) {
		h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
		h.eg.setTarget(h.server(echoConn))
		h.cls.set(41260, true, appGame)
		h.setRules(appGame)
		c, _ := h.mustDial(41260, dst(remoteA, 7260))
		roundTrip(t, c, "bypassed")
		e := h.engine()
		h.c.compile = panicCompile
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("SetRules let an engine panic escape: %v", r)
				}
			}()
			h.c.SetRules([]string{appGame, appChat}, nil)
		}()
		expectFaultedTunnelOnly(t, h, e, 41261)
		expectReset(t, c)
		expectQuickClose(t, h)
	})
}

func TestLookupFailsCountOnlyMissingSockets(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, peer: true})
	h.cls.mu.Lock()
	h.cls.owners[41270] = procmatch.Owner{SockID: 0x5150}
	h.cls.mu.Unlock()
	h.setRules(appGame)
	c, _ := h.mustDial(41270, dst(remoteB, 9270))
	roundTrip(t, c, "socket held by no candidate process")
	c.Close()
	if n := h.c.Status().Counters.LookupFails; n != 0 {
		t.Fatalf("LookupFails = %d for a socket that was found, want 0", n)
	}
	c, _ = h.mustDial(41271, dst(remoteB, 9271))
	roundTrip(t, c, "no socket row")
	c.Close()
	if n := h.c.Status().Counters.LookupFails; n != 1 {
		t.Fatalf("LookupFails = %d after a missing socket, want 1", n)
	}
}
