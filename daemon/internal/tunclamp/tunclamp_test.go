package tunclamp

import (
	"bytes"
	"encoding/binary"
	"net/netip"
	"os"
	"testing"

	"golang.zx2c4.com/wireguard/tun"
)

var (
	src4 = netip.MustParseAddr("10.10.0.2")
	dst4 = netip.MustParseAddr("93.184.216.34")
	src6 = netip.MustParseAddr("fd00::2")
	dst6 = netip.MustParseAddr("2606:2800:220:1::1")
)

func mssOption(mss uint16) []byte {
	return []byte{optMSS, optMSSLen, byte(mss >> 8), byte(mss)}
}

// segment builds a TCP header with the given flags and options, padded to a
// whole number of words, followed by payload.
func segment(flags byte, options, payload []byte) []byte {
	for len(options)%4 != 0 {
		options = append(options, optEnd)
	}
	tcp := make([]byte, tcpHeaderMin+len(options)+len(payload))
	binary.BigEndian.PutUint16(tcp[0:], 50000)
	binary.BigEndian.PutUint16(tcp[2:], 443)
	tcp[12] = byte((tcpHeaderMin + len(options)) / 4 << 4)
	tcp[13] = flags
	binary.BigEndian.PutUint16(tcp[14:], 65535)
	copy(tcp[tcpHeaderMin:], options)
	copy(tcp[tcpHeaderMin+len(options):], payload)
	return tcp
}

// checksum is a from-scratch RFC 1071 sum, independent of the package's own.
func checksum(chunks ...[]byte) uint16 {
	var all []byte
	for _, c := range chunks {
		all = append(all, c...)
	}
	if len(all)%2 == 1 {
		all = append(all, 0)
	}
	var s uint64
	for i := 0; i < len(all); i += 2 {
		s += uint64(all[i])<<8 | uint64(all[i+1])
	}
	for s > 0xffff {
		s = s>>16 + s&0xffff
	}
	return ^uint16(s)
}

func pseudo4(src, dst netip.Addr, n int) []byte {
	p := append(src.AsSlice(), dst.AsSlice()...)
	return append(p, 0, protoTCP, byte(n>>8), byte(n))
}

func pseudo6(src, dst netip.Addr, n int) []byte {
	p := append(src.AsSlice(), dst.AsSlice()...)
	return append(p, byte(n>>24), byte(n>>16), byte(n>>8), byte(n), 0, 0, 0, protoTCP)
}

func ipv4Packet(tcp []byte) []byte {
	pkt := make([]byte, ipv4HeaderMin+len(tcp))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:], uint16(len(pkt)))
	pkt[8] = 64
	pkt[9] = protoTCP
	copy(pkt[12:16], src4.AsSlice())
	copy(pkt[16:20], dst4.AsSlice())
	binary.BigEndian.PutUint16(pkt[10:], checksum(pkt[:ipv4HeaderMin]))
	binary.BigEndian.PutUint16(tcp[16:], checksum(pseudo4(src4, dst4, len(tcp)), tcp))
	copy(pkt[ipv4HeaderMin:], tcp)
	return pkt
}

func ipv6Packet(tcp []byte) []byte {
	pkt := make([]byte, ipv6Header+len(tcp))
	pkt[0] = 0x60
	binary.BigEndian.PutUint16(pkt[4:], uint16(len(tcp)))
	pkt[6] = protoTCP
	pkt[7] = 64
	copy(pkt[8:24], src6.AsSlice())
	copy(pkt[24:40], dst6.AsSlice())
	binary.BigEndian.PutUint16(tcp[16:], checksum(pseudo6(src6, dst6, len(tcp)), tcp))
	copy(pkt[ipv6Header:], tcp)
	return pkt
}

func mssOf(t *testing.T, tcp []byte) uint16 {
	t.Helper()
	opts := tcp[tcpHeaderMin : int(tcp[12]>>4)*4]
	for i := 0; i < len(opts); {
		if opts[i] == optNOP {
			i++
			continue
		}
		if opts[i] == optMSS {
			return binary.BigEndian.Uint16(opts[i+2:])
		}
		i += int(opts[i+1])
	}
	t.Fatal("no MSS option")
	return 0
}

func TestClampsAnIPv4SYNAndKeepsItsChecksumValid(t *testing.T) {
	pkt := ipv4Packet(segment(tcpFlagSYN, mssOption(1460), nil))
	if !ClampMSS(pkt, 1280) {
		t.Fatal("ClampMSS left a 1460 MSS alone under a 1280 MTU")
	}
	tcp := pkt[ipv4HeaderMin:]
	if got := mssOf(t, tcp); got != 1240 {
		t.Fatalf("MSS = %d, want 1240", got)
	}
	if checksum(pseudo4(src4, dst4, len(tcp)), tcp) != 0 {
		t.Fatal("TCP checksum no longer verifies")
	}
}

func TestClampsASYNACKArrivingFromTheTunnel(t *testing.T) {
	pkt := ipv4Packet(segment(tcpFlagSYN|0x10, mssOption(1340), nil))
	if !ClampMSS(pkt, 1280) || mssOf(t, pkt[ipv4HeaderMin:]) != 1240 {
		t.Fatal("a SYN-ACK was not clamped")
	}
}

func TestClampsAnIPv6SYNToItsLargerHeader(t *testing.T) {
	pkt := ipv6Packet(segment(tcpFlagSYN, mssOption(1440), nil))
	if !ClampMSS(pkt, 1280) {
		t.Fatal("ClampMSS left an IPv6 SYN alone")
	}
	tcp := pkt[ipv6Header:]
	if got := mssOf(t, tcp); got != 1220 {
		t.Fatalf("MSS = %d, want 1220", got)
	}
	if checksum(pseudo6(src6, dst6, len(tcp)), tcp) != 0 {
		t.Fatal("TCP checksum no longer verifies")
	}
}

// One NOP puts the MSS value on an odd offset, where a word-wise incremental
// update would get the checksum wrong.
func TestClampsAnMSSBehindOtherOptions(t *testing.T) {
	options := append([]byte{optNOP}, mssOption(1460)...)
	options = append(options, optNOP, 3, 3, 7) // window scale
	options = append(options, 4, 2)            // SACK permitted
	pkt := ipv4Packet(segment(tcpFlagSYN, options, nil))
	if !ClampMSS(pkt, 1280) {
		t.Fatal("ClampMSS missed an MSS behind a NOP")
	}
	tcp := pkt[ipv4HeaderMin:]
	if got := mssOf(t, tcp); got != 1240 {
		t.Fatalf("MSS = %d, want 1240", got)
	}
	if checksum(pseudo4(src4, dst4, len(tcp)), tcp) != 0 {
		t.Fatal("TCP checksum no longer verifies")
	}
}

func TestLeavesPacketsThatAlreadyFitOrAreNotSYNs(t *testing.T) {
	cases := map[string][]byte{
		"small MSS":   ipv4Packet(segment(tcpFlagSYN, mssOption(1200), nil)),
		"exact MSS":   ipv4Packet(segment(tcpFlagSYN, mssOption(1240), nil)),
		"plain ACK":   ipv4Packet(segment(0x10, mssOption(1460), []byte("data"))),
		"no MSS":      ipv4Packet(segment(tcpFlagSYN, []byte{optNOP, 3, 3, 7}, nil)),
		"end of opts": ipv4Packet(segment(tcpFlagSYN, append([]byte{optEnd}, mssOption(1460)...), nil)),
	}
	for name, pkt := range cases {
		before := bytes.Clone(pkt)
		if ClampMSS(pkt, 1280) || !bytes.Equal(pkt, before) {
			t.Errorf("%s: packet was changed", name)
		}
	}
}

func TestLeavesWhatItCannotParseAlone(t *testing.T) {
	syn := ipv4Packet(segment(tcpFlagSYN, mssOption(1460), nil))
	udp := bytes.Clone(syn)
	udp[9] = 17
	fragment := bytes.Clone(syn)
	binary.BigEndian.PutUint16(fragment[6:], 185)
	badOffset := bytes.Clone(syn)
	badOffset[ipv4HeaderMin+12] = 0xf0
	zeroLenOption := ipv4Packet(segment(tcpFlagSYN, []byte{3, 0, 0, 0}, nil))
	longTotal := bytes.Clone(syn)
	binary.BigEndian.PutUint16(longTotal[2:], 2000)
	cases := map[string][]byte{
		"empty": {}, "short": syn[:10], "udp": udp, "later fragment": fragment,
		"data offset past end": badOffset, "zero-length option": zeroLenOption,
		"total past buffer": longTotal, "ipv6 short": ipv6Packet(segment(tcpFlagSYN, mssOption(1460), nil))[:30],
	}
	for name, pkt := range cases {
		before := bytes.Clone(pkt)
		if ClampMSS(pkt, 1280) || !bytes.Equal(pkt, before) {
			t.Errorf("%s: packet was changed", name)
		}
	}
}

func FuzzClampMSSNeverPanics(f *testing.F) {
	f.Add(ipv4Packet(segment(tcpFlagSYN, mssOption(1460), nil)))
	f.Add(ipv6Packet(segment(tcpFlagSYN, mssOption(1440), nil)))
	f.Fuzz(func(t *testing.T, pkt []byte) {
		ClampMSS(pkt, 1280)
	})
}

// fakeDevice hands out one queued packet per Read and records every Write.
type fakeDevice struct {
	toRead  [][]byte
	written [][]byte
}

func (f *fakeDevice) File() *os.File           { return nil }
func (f *fakeDevice) MTU() (int, error)        { return 1380, nil }
func (f *fakeDevice) Name() (string, error)    { return "fake", nil }
func (f *fakeDevice) Events() <-chan tun.Event { return nil }
func (f *fakeDevice) Close() error             { return nil }
func (f *fakeDevice) BatchSize() int           { return 1 }

func (f *fakeDevice) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	sizes[0] = copy(bufs[0][offset:], f.toRead[0])
	f.toRead = f.toRead[1:]
	return 1, nil
}

func (f *fakeDevice) Write(bufs [][]byte, offset int) (int, error) {
	for _, b := range bufs {
		f.written = append(f.written, bytes.Clone(b[offset:]))
	}
	return len(bufs), nil
}

func TestDeviceClampsBothDirectionsOnlyWhileSet(t *testing.T) {
	const offset = 16
	syn := func() []byte { return ipv4Packet(segment(tcpFlagSYN, mssOption(1460), nil)) }
	inner := &fakeDevice{toRead: [][]byte{syn(), syn()}}
	dev := Wrap(inner)

	read := func() []byte {
		buf := make([]byte, offset+1500)
		sizes := make([]int, 1)
		if _, err := dev.Read([][]byte{buf}, sizes, offset); err != nil {
			t.Fatal(err)
		}
		return buf[offset : offset+sizes[0]]
	}
	write := func() []byte {
		buf := append(make([]byte, offset), syn()...)
		if _, err := dev.Write([][]byte{buf}, offset); err != nil {
			t.Fatal(err)
		}
		return inner.written[len(inner.written)-1]
	}

	if got := mssOf(t, read()[ipv4HeaderMin:]); got != 1460 {
		t.Fatalf("read with the clamp off: MSS = %d, want 1460", got)
	}
	dev.SetMTU(1280)
	if got := mssOf(t, read()[ipv4HeaderMin:]); got != 1240 {
		t.Fatalf("read with the clamp on: MSS = %d, want 1240", got)
	}
	if got := mssOf(t, write()[ipv4HeaderMin:]); got != 1240 {
		t.Fatalf("write with the clamp on: MSS = %d, want 1240", got)
	}
	dev.SetMTU(0)
	if got := mssOf(t, write()[ipv4HeaderMin:]); got != 1460 {
		t.Fatalf("write with the clamp off: MSS = %d, want 1460", got)
	}
}
