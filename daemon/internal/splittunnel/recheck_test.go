package splittunnel

import (
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
)

func TestBuildTCPReset(t *testing.T) {
	// A pooled buffer that held other bytes must not leak them into the reset.
	dirty := newPacketBuf(engineBufSize)
	for i := range dirty.data {
		dirty.data[i] = 0xff
	}
	dirty.release()

	from, to := dst(remoteA, 443), dst(tunAddr, 40000)
	p := buildTCPReset(from, to, 0xdeadbeef)
	defer p.release()
	if len(p.data) != writeHeadroom+header.IPv4MinimumSize+header.TCPMinimumSize {
		t.Fatalf("len = %d", len(p.data))
	}
	b := p.data[writeHeadroom:]
	info, ok := parseIPv4(b)
	if !ok || info.proto != protoTCP || info.src != remoteA || info.dst != tunAddr ||
		info.srcPort != 443 || info.dstPort != 40000 || info.seq != 0xdeadbeef || info.ack != 0 || info.flags != tcpFlagRst {
		t.Fatalf("reset = %+v, %v", info, ok)
	}
	if !header.IPv4(b).IsChecksumValid() {
		t.Fatal("bad IPv4 checksum")
	}
	tcpHdr := header.TCP(b[header.IPv4MinimumSize:])
	if !tcpHdr.IsChecksumValid(tcpip.AddrFrom4(remoteA.As4()), tcpip.AddrFrom4(tunAddr.As4()), 0, 0) {
		t.Fatal("bad TCP checksum")
	}
	if tcpHdr.DataOffset() != header.TCPMinimumSize || tcpHdr.WindowSize() != 0 {
		t.Fatalf("offset %d window %d", tcpHdr.DataOffset(), tcpHdr.WindowSize())
	}
}

func TestParseIPv4ReadsTheAck(t *testing.T) {
	p := tcpPacket(dst(tunAddr, 40000), dst(remoteA, 443), header.TCPFlagAck, 10, nil)
	header.TCP(p[header.IPv4MinimumSize:]).SetAckNumber(0x01020304)
	if info, ok := parseIPv4(p); !ok || info.ack != 0x01020304 {
		t.Fatalf("ack = %#x, %v", info.ack, ok)
	}
}

// Connections an app opened through the tunnel before it was excluded are reset, so the app
// reconnects outside the VPN; other apps' tunnel connections are looked up and left alone.
func TestExcludingAnAppResetsItsTunnelConnections(t *testing.T) {
	for _, kind := range allKinds {
		t.Run(kind.String(), func(t *testing.T) {
			h := newHarness(t, harnessOpts{kind: kind, peer: true})
			h.eg.setTarget(h.server(echoConn))
			game, _ := h.mustDial(40300, dst(remoteB, 8300))
			chat, _ := h.mustDial(40301, dst(remoteB, 8301))
			roundTrip(t, game, "game, before any rule")
			roundTrip(t, chat, "chat, before any rule")

			h.cls.set(40300, true, appGame)
			h.cls.set(40301, false, appChat)
			h.setRules(appGame)
			if _, err := game.Write([]byte("still here")); err != nil {
				t.Fatalf("game write: %v", err)
			}
			expectReset(t, game)
			roundTrip(t, chat, "chat stays in the tunnel")
			waitFor(t, 2*time.Second, "chat looked up again", func() bool { return h.cls.classifiedCount(40301) > 0 })
			roundTrip(t, chat, "chat, after its lookup")
			if n := h.c.Status().Counters.Moved; n != 1 {
				t.Fatalf("Moved = %d, want 1", n)
			}

			h.cls.set(40302, true, appGame)
			again, _ := h.mustDial(40302, dst(remoteA, 7302))
			roundTrip(t, again, "the game's new connection bypasses")
			if n := h.eg.dials.Load(); n != 1 {
				t.Fatalf("egress dials = %d, want 1", n)
			}
			again.Close()
			chat.Close()
		})
	}
}

// A later rule edit opens a new window: a connection that stayed in the tunnel under the
// old rules is looked up again under the new ones.
func TestAddingAnAppRechecksConnectionsTheOldRulesKept(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, peer: true})
	h.cls.set(40310, false, appGame)
	h.setRules(appChat)
	game, _ := h.mustDial(40310, dst(remoteB, 8310))
	roundTrip(t, game, "game in the tunnel")
	roundTrip(t, game, "game, again")
	e := h.engine()
	waitFor(t, 2*time.Second, "game checked under the first rules", func() bool {
		n := 0
		e.withLock(func() { n = e.rechecked.len() })
		return n > 0
	})
	before := h.cls.classifiedCount(40310)
	roundTrip(t, game, "a cleared connection is not looked up twice")
	if n := h.cls.classifiedCount(40310); n != before {
		t.Fatalf("lookups = %d, want %d", n, before)
	}

	h.cls.set(40310, true, appGame)
	h.setRules(appChat, appGame)
	if _, err := game.Write([]byte("now excluded")); err != nil {
		t.Fatalf("game write: %v", err)
	}
	expectReset(t, game)
	if n := h.c.Status().Counters.Moved; n != 1 {
		t.Fatalf("Moved = %d, want 1", n)
	}
}

// Outside the window an established tunnel connection costs no lookup and is never reset.
func TestNoRecheckOutsideTheWindow(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindLinux, peer: true, lim: func(l *limits) { l.recheckWindow = 0 }})
	game, _ := h.mustDial(40320, dst(remoteB, 8320))
	roundTrip(t, game, "before any rule")
	h.cls.set(40320, true, appGame)
	h.setRules(appGame)
	for i := 0; i < 3; i++ {
		roundTrip(t, game, "game keeps its tunnel connection")
	}
	if n := h.cls.classifiedCount(40320); n != 0 {
		t.Fatalf("lookups = %d, want 0", n)
	}
	if n := h.c.Status().Counters.Moved; n != 0 {
		t.Fatalf("Moved = %d, want 0", n)
	}
	game.Close()
}
