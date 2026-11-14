// Package tunclamp lowers the MSS that TCP SYNs advertise as they cross a TUN,
// for a tunnel whose carrier cannot fit the MTU the host gave the interface.
package tunclamp

import (
	"encoding/binary"
	"sync/atomic"

	"golang.zx2c4.com/wireguard/tun"
)

const (
	ipv4HeaderMin = 20
	ipv6Header    = 40
	tcpHeaderMin  = 20
	protoTCP      = 6
	tcpFlagSYN    = 0x02
	optEnd        = 0
	optNOP        = 1
	optMSS        = 2
	optMSSLen     = 4
)

// Device clamps SYNs both ways: those the host sends, so the far end sizes its
// segments to fit, and those that arrive, so the host does the same.
type Device struct {
	tun.Device
	mtu atomic.Int32
}

func Wrap(dev tun.Device) *Device {
	return &Device{Device: dev}
}

// SetMTU clamps every TCP SYN to fit mtu; 0 turns the clamp off.
func (d *Device) SetMTU(mtu int) {
	d.mtu.Store(int32(mtu))
}

// ClampMTU is the MTU SYNs are clamped to, 0 while the clamp is off.
func (d *Device) ClampMTU() int {
	return int(d.mtu.Load())
}

func (d *Device) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	n, err := d.Device.Read(bufs, sizes, offset)
	if mtu := int(d.mtu.Load()); mtu > 0 {
		for i := 0; i < n; i++ {
			ClampMSS(bufs[i][offset:offset+sizes[i]], mtu)
		}
	}
	return n, err
}

func (d *Device) Write(bufs [][]byte, offset int) (int, error) {
	if mtu := int(d.mtu.Load()); mtu > 0 {
		for _, buf := range bufs {
			ClampMSS(buf[offset:], mtu)
		}
	}
	return d.Device.Write(bufs, offset)
}

// ClampMSS lowers the MSS option of a TCP SYN in pkt to fit mtu and fixes the
// checksum, reporting whether it changed anything. Anything else, including a
// packet too malformed to parse, is left alone.
func ClampMSS(pkt []byte, mtu int) bool {
	tcp, pseudo, maxMSS, ok := tcpSegment(pkt, mtu)
	if !ok || len(tcp) < tcpHeaderMin || tcp[13]&tcpFlagSYN == 0 {
		return false
	}
	dataOffset := int(tcp[12]>>4) * 4
	if dataOffset < tcpHeaderMin || dataOffset > len(tcp) {
		return false
	}
	options := tcp[tcpHeaderMin:dataOffset]
	for i := 0; i < len(options); {
		switch options[i] {
		case optEnd:
			return false
		case optNOP:
			i++
			continue
		}
		if i+1 >= len(options) {
			return false
		}
		length := int(options[i+1])
		if length < 2 || i+length > len(options) {
			return false
		}
		if options[i] == optMSS && length == optMSSLen {
			if int(binary.BigEndian.Uint16(options[i+2:])) <= maxMSS {
				return false
			}
			binary.BigEndian.PutUint16(options[i+2:], uint16(maxMSS))
			setTCPChecksum(tcp, pseudo)
			return true
		}
		i += length
	}
	return false
}

// tcpSegment finds the TCP segment of an unfragmented IPv4 or extension-free
// IPv6 packet, with its pseudo-header sum and the largest MSS that fits mtu.
func tcpSegment(pkt []byte, mtu int) (tcp []byte, pseudo uint32, maxMSS int, ok bool) {
	if len(pkt) == 0 {
		return nil, 0, 0, false
	}
	switch pkt[0] >> 4 {
	case 4:
		if len(pkt) < ipv4HeaderMin || pkt[9] != protoTCP {
			return nil, 0, 0, false
		}
		headerLen := int(pkt[0]&0x0f) * 4
		total := int(binary.BigEndian.Uint16(pkt[2:4]))
		if headerLen < ipv4HeaderMin || total < headerLen || total > len(pkt) {
			return nil, 0, 0, false
		}
		// Only the first fragment carries the TCP header.
		if binary.BigEndian.Uint16(pkt[6:8])&0x1fff != 0 {
			return nil, 0, 0, false
		}
		tcp = pkt[headerLen:total]
		pseudo = sum(pkt[12:20]) + protoTCP + uint32(len(tcp))
		return tcp, pseudo, mtu - ipv4HeaderMin - tcpHeaderMin, true
	case 6:
		if len(pkt) < ipv6Header || pkt[6] != protoTCP {
			return nil, 0, 0, false
		}
		payload := int(binary.BigEndian.Uint16(pkt[4:6]))
		if ipv6Header+payload > len(pkt) {
			return nil, 0, 0, false
		}
		tcp = pkt[ipv6Header : ipv6Header+payload]
		pseudo = sum(pkt[8:40]) + protoTCP + uint32(len(tcp))
		return tcp, pseudo, mtu - ipv6Header - tcpHeaderMin, true
	}
	return nil, 0, 0, false
}

// setTCPChecksum recomputes in full rather than incrementally: the MSS value
// can sit at an odd offset after a NOP, and a SYN is only a few dozen bytes.
func setTCPChecksum(tcp []byte, pseudo uint32) {
	tcp[16], tcp[17] = 0, 0
	binary.BigEndian.PutUint16(tcp[16:], ^fold(pseudo+sum(tcp)))
}

func sum(b []byte) uint32 {
	var s uint32
	for len(b) >= 2 {
		s += uint32(binary.BigEndian.Uint16(b))
		b = b[2:]
	}
	if len(b) == 1 {
		s += uint32(b[0]) << 8
	}
	return s
}

func fold(s uint32) uint16 {
	for s > 0xffff {
		s = (s >> 16) + (s & 0xffff)
	}
	return uint16(s)
}
