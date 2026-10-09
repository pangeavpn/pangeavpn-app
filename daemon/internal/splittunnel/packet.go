package splittunnel

import (
	"encoding/binary"
	"net/netip"
	"sync"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/checksum"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
)

const (
	protoICMP = 1
	protoTCP  = 6
	protoUDP  = 17

	tcpFlagFin = 0x01
	tcpFlagSyn = 0x02
	tcpFlagRst = 0x04
	tcpFlagAck = 0x10

	engineBufSize = 2048
	writeHeadroom = 16
)

// packetBuf is an engine-owned copy of a packet; pump read buffers never outlive the next read.
type packetBuf struct {
	data   []byte
	pooled bool
}

var packetPool = sync.Pool{New: func() any {
	return &packetBuf{data: make([]byte, 0, engineBufSize), pooled: true}
}}

func newPacketBuf(n int) *packetBuf {
	if n > engineBufSize {
		return &packetBuf{data: make([]byte, n)}
	}
	p := packetPool.Get().(*packetBuf)
	p.data = p.data[:n]
	return p
}

func copyPacket(src []byte) *packetBuf {
	p := newPacketBuf(len(src))
	copy(p.data, src)
	return p
}

func (p *packetBuf) release() {
	if p == nil || !p.pooled {
		return
	}
	p.data = p.data[:0]
	packetPool.Put(p)
}

// bufCost is what a queued packet holds against the pending byte budget.
func (p *packetBuf) bufCost() int { return cap(p.data) }

type flowKey struct {
	srcPort uint16
	dst     netip.AddrPort
}

type pktInfo struct {
	proto   uint8
	src     netip.Addr
	dst     netip.Addr
	frag    bool
	more    bool
	ports   bool
	id      uint16
	fragOff int
	ihl     int
	total   int
	udpLen  int
	srcPort uint16
	dstPort uint16
	flags   uint8
	seq     uint32
}

// parseIPv4 validates the IPv4 and transport headers decide() relies on; ok=false means
// "not something the engine understands" and the packet goes to the tunnel.
func parseIPv4(b []byte) (info pktInfo, ok bool) {
	if len(b) < 20 || b[0]>>4 != 4 {
		return info, false
	}
	ihl := int(b[0]&0x0f) * 4
	if ihl < 20 || len(b) < ihl {
		return info, false
	}
	total := int(binary.BigEndian.Uint16(b[2:4]))
	if total < ihl || total > len(b) {
		return info, false
	}
	b = b[:total]
	info.proto = b[9]
	info.src = netip.AddrFrom4([4]byte(b[12:16]))
	info.dst = netip.AddrFrom4([4]byte(b[16:20]))
	info.id = binary.BigEndian.Uint16(b[4:6])
	info.ihl, info.total = ihl, total
	t := b[ihl:]
	ff := binary.BigEndian.Uint16(b[6:8])
	if ff&0x2000 != 0 || ff&0x1fff != 0 {
		info.frag = true
		info.more = ff&0x2000 != 0
		info.fragOff = int(ff&0x1fff) * 8
		if info.proto == protoUDP && info.fragOff == 0 && len(t) >= 8 {
			info.srcPort = binary.BigEndian.Uint16(t[0:2])
			info.dstPort = binary.BigEndian.Uint16(t[2:4])
			info.udpLen = int(binary.BigEndian.Uint16(t[4:6]))
			info.ports = true
		}
		return info, true
	}
	switch info.proto {
	case protoTCP:
		if len(t) < 20 {
			return info, false
		}
		doff := int(t[12]>>4) * 4
		if doff < 20 || doff > len(t) {
			return info, false
		}
		info.srcPort = binary.BigEndian.Uint16(t[0:2])
		info.dstPort = binary.BigEndian.Uint16(t[2:4])
		info.seq = binary.BigEndian.Uint32(t[4:8])
		info.flags = t[13]
	case protoUDP:
		if len(t) < 8 {
			return info, false
		}
		info.srcPort = binary.BigEndian.Uint16(t[0:2])
		info.dstPort = binary.BigEndian.Uint16(t[2:4])
		info.udpLen = int(binary.BigEndian.Uint16(t[4:6]))
		info.ports = true
	}
	return info, true
}

// udpPayload bounds the payload of an unfragmented UDP packet; false when the UDP length
// disagrees with the IP length.
func (info *pktInfo) udpPayload() (lo, hi int, ok bool) {
	if info.udpLen < 8 || info.ihl+info.udpLen > info.total {
		return 0, 0, false
	}
	return info.ihl + 8, info.ihl + info.udpLen, true
}

type quoted struct {
	proto   uint8
	src     netip.Addr
	dst     netip.Addr
	srcPort uint16
	dstPort uint16
}

// icmpQuote returns the TCP/UDP header quoted by an ICMP destination-unreachable,
// time-exceeded or parameter-problem message.
func icmpQuote(icmp []byte) (q quoted, ok bool) {
	if len(icmp) < 8+20+8 {
		return q, false
	}
	switch icmp[0] {
	case 3, 11, 12:
	default:
		return q, false
	}
	in := icmp[8:]
	ihl := int(in[0]&0x0f) * 4
	if in[0]>>4 != 4 || ihl < 20 || len(in) < ihl+8 || binary.BigEndian.Uint16(in[6:8])&0x1fff != 0 {
		return q, false
	}
	q.proto = in[9]
	if q.proto != protoTCP && q.proto != protoUDP {
		return q, false
	}
	q.src = netip.AddrFrom4([4]byte(in[12:16]))
	q.dst = netip.AddrFrom4([4]byte(in[16:20]))
	q.srcPort = binary.BigEndian.Uint16(in[ihl : ihl+2])
	q.dstPort = binary.BigEndian.Uint16(in[ihl+2 : ihl+4])
	return q, true
}

const maxUDPPayload = 65535 - header.IPv4MinimumSize - header.UDPMinimumSize

// buildUDPReply appends the IPv4 packets carrying payload from -> to, fragmented at mtu.
// Each buffer keeps writeHeadroom spare bytes in front; nextID is only used when fragmenting.
func buildUDPReply(out []*packetBuf, from, to netip.AddrPort, payload []byte, mtu int, nextID func() uint16) ([]*packetBuf, bool) {
	if len(payload) > maxUDPPayload {
		return out, false
	}
	var uh [header.UDPMinimumSize]byte
	udpLen := len(uh) + len(payload)
	binary.BigEndian.PutUint16(uh[0:2], from.Port())
	binary.BigEndian.PutUint16(uh[2:4], to.Port())
	binary.BigEndian.PutUint16(uh[4:6], uint16(udpLen))
	src, dst := tcpip.AddrFrom4(from.Addr().As4()), tcpip.AddrFrom4(to.Addr().As4())
	xsum := header.PseudoHeaderChecksum(header.UDPProtocolNumber, src, dst, uint16(udpLen))
	xsum = checksum.Checksum(uh[:], xsum)
	xsum = checksum.Checksum(payload, xsum)
	if xsum = ^xsum; xsum == 0 {
		xsum = 0xffff
	}
	binary.BigEndian.PutUint16(uh[6:8], xsum)

	if header.IPv4MinimumSize+udpLen <= mtu {
		return append(out, replyPacket(uh[:], payload, 0, udpLen, 0, 0, from.Addr(), to.Addr())), true
	}
	step := (mtu - header.IPv4MinimumSize) &^ 7
	if step < 8 {
		return out, false
	}
	id := nextID()
	for off := 0; off < udpLen; off += step {
		n := min(step, udpLen-off)
		ff := uint16(off / 8)
		if off+n < udpLen {
			ff |= 0x2000
		}
		out = append(out, replyPacket(uh[:], payload, off, n, id, ff, from.Addr(), to.Addr()))
	}
	return out, true
}

// replyPacket builds one IPv4 packet carrying bytes [off, off+n) of the UDP header
// followed by the payload.
func replyPacket(uh, payload []byte, off, n int, id, ff uint16, src, dst netip.Addr) *packetBuf {
	p := newPacketBuf(writeHeadroom + header.IPv4MinimumSize + n)
	ip := p.data[writeHeadroom:]
	ip[0], ip[1] = 0x45, 0
	binary.BigEndian.PutUint16(ip[2:4], uint16(header.IPv4MinimumSize+n))
	binary.BigEndian.PutUint16(ip[4:6], id)
	binary.BigEndian.PutUint16(ip[6:8], ff)
	ip[8], ip[9], ip[10], ip[11] = 64, protoUDP, 0, 0
	s4, d4 := src.As4(), dst.As4()
	copy(ip[12:16], s4[:])
	copy(ip[16:20], d4[:])
	binary.BigEndian.PutUint16(ip[10:12], ^checksum.Checksum(ip[:header.IPv4MinimumSize], 0))
	body := ip[header.IPv4MinimumSize:]
	if off < len(uh) {
		c := copy(body, uh[off:])
		copy(body[c:], payload[:n-c])
		return p
	}
	copy(body, payload[off-len(uh):off-len(uh)+n])
	return p
}

func alwaysTunnelDst(dst netip.Addr) bool {
	b := dst.As4()
	return b[0] == 0 || b[0] == 127 || dst.IsMulticast() || b == [4]byte{255, 255, 255, 255}
}
