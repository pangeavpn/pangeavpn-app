package splittunnel

import (
	"encoding/binary"
	"net/netip"
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/checksum"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/tcp"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/udp"
)

func ipv4Packet(src, dst netip.Addr, proto uint8, transport []byte) []byte {
	b := make([]byte, header.IPv4MinimumSize+len(transport))
	ip := header.IPv4(b)
	ip.Encode(&header.IPv4Fields{
		TotalLength: uint16(len(b)),
		TTL:         64,
		Protocol:    proto,
		SrcAddr:     tcpip.AddrFrom4(src.As4()),
		DstAddr:     tcpip.AddrFrom4(dst.As4()),
	})
	ip.SetChecksum(^ip.CalculateChecksum())
	copy(b[header.IPv4MinimumSize:], transport)
	return b
}

func tcpPacket(src, dst netip.AddrPort, flags header.TCPFlags, seq uint32, payload []byte) []byte {
	t := make(header.TCP, header.TCPMinimumSize+len(payload))
	t.Encode(&header.TCPFields{
		SrcPort:    src.Port(),
		DstPort:    dst.Port(),
		SeqNum:     seq,
		DataOffset: header.TCPMinimumSize,
		Flags:      flags,
		WindowSize: 65535,
	})
	copy(t[header.TCPMinimumSize:], payload)
	xsum := header.PseudoHeaderChecksum(tcp.ProtocolNumber, tcpip.AddrFrom4(src.Addr().As4()), tcpip.AddrFrom4(dst.Addr().As4()), uint16(len(t)))
	xsum = checksum.Checksum(payload, xsum)
	t.SetChecksum(^t.CalculateChecksum(xsum))
	return ipv4Packet(src.Addr(), dst.Addr(), protoTCP, t)
}

func udpPacket(src, dst netip.AddrPort, payload []byte) []byte {
	u := make(header.UDP, header.UDPMinimumSize+len(payload))
	u.Encode(&header.UDPFields{SrcPort: src.Port(), DstPort: dst.Port(), Length: uint16(len(u))})
	copy(u[header.UDPMinimumSize:], payload)
	xsum := header.PseudoHeaderChecksum(udp.ProtocolNumber, tcpip.AddrFrom4(src.Addr().As4()), tcpip.AddrFrom4(dst.Addr().As4()), uint16(len(u)))
	xsum = checksum.Checksum(payload, xsum)
	u.SetChecksum(^u.CalculateChecksum(xsum))
	return ipv4Packet(src.Addr(), dst.Addr(), protoUDP, u)
}

func syn(srcPort uint16, to netip.AddrPort, isn uint32) []byte {
	return tcpPacket(netip.AddrPortFrom(tunAddr, srcPort), to, header.TCPFlagSyn, isn, nil)
}

func TestParseIPv4(t *testing.T) {
	s := syn(40000, dst(remoteA, 443), 77)
	info, ok := parseIPv4(s)
	if !ok || info.proto != protoTCP || info.src != tunAddr || info.dst != remoteA || info.srcPort != 40000 || info.dstPort != 443 || info.seq != 77 || info.flags != tcpFlagSyn || info.frag {
		t.Fatalf("parse SYN = %+v, %v", info, ok)
	}
	u := udpPacket(dst(tunAddr, 5000), dst(remoteB, 9), []byte("x"))
	if info, ok := parseIPv4(u); !ok || info.proto != protoUDP || info.srcPort != 5000 || info.dstPort != 9 {
		t.Fatalf("parse UDP = %+v, %v", info, ok)
	}

	bad := map[string][]byte{
		"empty":      nil,
		"short":      s[:19],
		"ipv6":       append([]byte{0x60}, s[1:]...),
		"ihl<5":      append([]byte{0x44}, s[1:]...),
		"total>len":  s[:30],
		"tcp short":  ipv4Packet(tunAddr, remoteA, protoTCP, make([]byte, 10)),
		"tcp doff":   ipv4Packet(tunAddr, remoteA, protoTCP, append(make([]byte, 12), 0xf0, 0, 0, 0, 0, 0, 0, 0)),
		"udp short":  ipv4Packet(tunAddr, remoteA, protoUDP, make([]byte, 4)),
		"tcp doff<5": ipv4Packet(tunAddr, remoteA, protoTCP, append(make([]byte, 12), 0x40, 0, 0, 0, 0, 0, 0, 0)),
	}
	for name, b := range bad {
		if _, ok := parseIPv4(b); ok {
			t.Errorf("%s: parsed, want rejection", name)
		}
	}

	frag := append([]byte(nil), u...)
	binary.BigEndian.PutUint16(frag[6:8], 0x2000)
	if info, ok := parseIPv4(frag); !ok || !info.frag {
		t.Errorf("MF packet not flagged as fragment: %+v", info)
	}
	binary.BigEndian.PutUint16(frag[6:8], 0x0010)
	if info, ok := parseIPv4(frag); !ok || !info.frag {
		t.Errorf("offset packet not flagged as fragment: %+v", info)
	}
	binary.BigEndian.PutUint16(frag[6:8], 0x4000)
	if info, ok := parseIPv4(frag); !ok || info.frag {
		t.Errorf("DF-only packet flagged as fragment: %+v", info)
	}

	padded := append(append([]byte(nil), s...), 1, 2, 3)
	if info, ok := parseIPv4(padded); !ok || info.seq != 77 {
		t.Errorf("trailing padding broke parse: %+v %v", info, ok)
	}
}

func TestAlwaysTunnelDst(t *testing.T) {
	for _, s := range []string{"0.0.0.0", "0.1.2.3", "127.0.0.1", "127.255.0.9", "224.0.0.251", "239.255.255.250", "255.255.255.255"} {
		if !alwaysTunnelDst(netip.MustParseAddr(s)) {
			t.Errorf("%s should always tunnel", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "198.51.100.7", "10.0.0.1", "192.168.1.255"} {
		if alwaysTunnelDst(netip.MustParseAddr(s)) {
			t.Errorf("%s should be classifiable", s)
		}
	}
}

func TestPacketBufPoolSizes(t *testing.T) {
	small := copyPacket(make([]byte, 1500))
	if cap(small.data) != engineBufSize || len(small.data) != 1500 || !small.pooled {
		t.Fatalf("small packet buf cap=%d len=%d pooled=%v", cap(small.data), len(small.data), small.pooled)
	}
	small.release()
	big := copyPacket(make([]byte, 9000))
	if len(big.data) != 9000 || big.pooled {
		t.Fatalf("big packet buf len=%d pooled=%v, want exact allocation", len(big.data), big.pooled)
	}
	big.release()
}

func TestFifoCache(t *testing.T) {
	var c fifoCache
	c.init(3)
	now := time.Now()
	k := func(p uint16) flowKey { return flowKey{srcPort: p, dst: dst(remoteA, 80)} }
	for p := uint16(1); p <= 4; p++ {
		c.put(k(p), uint32(p), now.Add(time.Minute))
	}
	if c.len() != 3 || c.get(k(1), now) != nil {
		t.Fatalf("oldest entry not evicted at cap: len=%d", c.len())
	}
	if ce := c.get(k(4), now); ce == nil || ce.isn != 4 {
		t.Fatalf("entry 4 = %+v", ce)
	}
	c.put(k(5), 5, now.Add(time.Second))
	if c.get(k(5), now.Add(2*time.Second)) != nil {
		t.Fatal("expired entry returned")
	}
	c.remove(k(4))
	if c.get(k(4), now) != nil {
		t.Fatal("removed entry returned")
	}
	c.put(k(6), 6, now.Add(-time.Second))
	c.sweep(now)
	if c.get(k(3), now) == nil {
		t.Fatal("live entry swept")
	}
}

// newDecideEngine builds an engine whose decide() can be driven without a pump.
func newDecideEngine(t testing.TB) *engine {
	c := NewController(ControllerOptions{})
	c.compile = fakeCompile
	ft := newFakeTUN(kindWindows)
	d := c.WrapTUN(ft, testTunnelInfo()).(*Device)
	c.SetRules([]string{appGame}, nil)
	e, err := newEngine(d, 2048, wgOffset)
	if err != nil {
		t.Fatal(err)
	}
	d.eng.Store(e)
	t.Cleanup(func() {
		e.shutdown()
		_ = d.Close()
		_ = c.Close()
	})
	return e
}

func FuzzDecide(f *testing.F) {
	f.Add(syn(40000, dst(remoteA, 443), 1))
	f.Add(tcpPacket(dst(tunAddr, 40000), dst(remoteA, 443), header.TCPFlagAck, 9, []byte("data")))
	f.Add(tcpPacket(dst(tunAddr, 40001), dst(remoteA, 53), header.TCPFlagSyn, 1, nil))
	f.Add(udpPacket(dst(tunAddr, 5000), dst(remoteB, 9), []byte("u")))
	f.Add(udpPacket(dst(tunAddr, 5001), dst(remoteB, 53), []byte("dns")))
	f.Add(syn(40002, dst(netip.MustParseAddr("224.0.0.1"), 80), 3))
	f.Add([]byte{0x45, 0, 0, 20, 0, 0, 0x20, 0, 64, 6, 0, 0, 10, 7, 0, 2, 1, 2, 3, 4})
	f.Add([]byte{0x4f})
	for _, fr := range fragment(udpPacket(dst(tunAddr, 5002), dst(remoteA, 9), make([]byte, 3000)), 7, 1420) {
		f.Add(fr)
	}
	f.Add(icmpError(3, udpPacket(dst(remoteA, 9), dst(tunAddr, 5000), []byte("reply"))))
	f.Add(icmpError(11, syn(40000, dst(remoteA, 443), 1)))
	e := newDecideEngine(f)
	f.Fuzz(func(t *testing.T, pkt []byte) {
		var sp spill
		act := e.decide(pkt, time.Now(), &sp)
		for _, it := range sp.items {
			if it.act != actTunnel && it.act != actStack {
				t.Fatalf("spilled action %d", it.act)
			}
		}
		sp.release()
		if act > actHeld {
			t.Fatalf("unknown action %d", act)
		}
		info, ok := parseIPv4(pkt)
		transport := info.proto == protoTCP || info.proto == protoUDP
		forced := !ok ||
			(info.frag && (info.proto != protoUDP || info.src != tunAddr)) ||
			(!info.frag && !transport && info.proto != protoICMP) ||
			(!info.frag && transport && (info.src != tunAddr || info.dstPort == 53 || info.dstPort == 853 ||
				alwaysTunnelDst(info.dst) || info.dst == tunDNS))
		if forced && act != actTunnel {
			t.Fatalf("decide = %d for always-tunnel packet %+v", act, info)
		}
		if ok && !info.frag && info.proto == protoICMP && act != actTunnel && act != actDrop {
			t.Fatalf("decide = %d for ICMP", act)
		}
	})
}

// fragment splits an IPv4 packet the way an OS without DF does, with IP ID id.
func fragment(pkt []byte, id uint16, mtu int) [][]byte {
	ihl := int(pkt[0]&0x0f) * 4
	body := pkt[ihl:]
	step := (mtu - ihl) &^ 7
	var out [][]byte
	for off := 0; off < len(body); off += step {
		n := min(step, len(body)-off)
		b := make([]byte, ihl+n)
		copy(b, pkt[:ihl])
		copy(b[ihl:], body[off:off+n])
		ff := uint16(off / 8)
		if off+n < len(body) {
			ff |= 0x2000
		}
		binary.BigEndian.PutUint16(b[2:4], uint16(len(b)))
		binary.BigEndian.PutUint16(b[4:6], id)
		binary.BigEndian.PutUint16(b[6:8], ff)
		b[10], b[11] = 0, 0
		binary.BigEndian.PutUint16(b[10:12], ^checksum.Checksum(b[:ihl], 0))
		out = append(out, b)
	}
	return out
}

// icmpError builds the ICMP error the app's OS sends from the tunnel address about quotedPkt.
func icmpError(typ uint8, quotedPkt []byte) []byte {
	q := quotedPkt[:min(len(quotedPkt), 28)]
	m := make([]byte, 8+len(q))
	m[0] = typ
	copy(m[8:], q)
	binary.BigEndian.PutUint16(m[2:4], ^checksum.Checksum(m, 0))
	src := netip.AddrFrom4([4]byte(quotedPkt[16:20]))
	to := netip.AddrFrom4([4]byte(quotedPkt[12:16]))
	return ipv4Packet(src, to, protoICMP, m)
}
