package procmatch

import (
	"bytes"
	"encoding/binary"
	"errors"
	"net/netip"
	"slices"
	"testing"
)

func TestPackDiagRequestTCPExact(t *testing.T) {
	b := packDiagRequest(7, afInet, protoTCP, 0, app, remote, false)
	ne := binary.NativeEndian
	if len(b) != 72 || ne.Uint32(b) != 72 || ne.Uint16(b[4:]) != 20 || ne.Uint16(b[6:]) != nlmFRequest || ne.Uint32(b[8:]) != 7 {
		t.Fatalf("header = % x", b[:16])
	}
	if b[16] != afInet || b[17] != protoTCP || ne.Uint32(b[20:]) != 0 {
		t.Fatalf("family/proto/states = % x", b[16:24])
	}
	if binary.BigEndian.Uint16(b[24:]) != 50000 || binary.BigEndian.Uint16(b[26:]) != 443 {
		t.Fatalf("ports = % x", b[24:28])
	}
	if !bytes.Equal(b[28:44], append([]byte{10, 64, 0, 2}, make([]byte, 12)...)) {
		t.Fatalf("src = % x", b[28:44])
	}
	if !bytes.Equal(b[44:60], append([]byte{203, 0, 113, 9}, make([]byte, 12)...)) {
		t.Fatalf("dst = % x", b[44:60])
	}
	if ne.Uint32(b[60:]) != 0 || !bytes.Equal(b[64:72], bytes.Repeat([]byte{0xff}, 8)) {
		t.Fatalf("ifindex/cookie = % x", b[60:72])
	}
}

func TestPackDiagRequestDump(t *testing.T) {
	src := netip.AddrPortFrom(netip.Addr{}, 3074)
	b := packDiagRequest(9, afInet6, protoUDP, diagAllStates, src, netip.AddrPort{}, true)
	ne := binary.NativeEndian
	if ne.Uint16(b[6:]) != nlmFRequest|nlmFDump || b[16] != afInet6 || b[17] != protoUDP || ne.Uint32(b[20:]) != diagAllStates {
		t.Fatalf("dump header = % x", b[:24])
	}
	if binary.BigEndian.Uint16(b[24:]) != 3074 || binary.BigEndian.Uint16(b[26:]) != 0 || !bytes.Equal(b[28:60], make([]byte, 32)) {
		t.Fatalf("dump filter = % x", b[24:60])
	}
	mapped := packDiagRequest(1, afInet6, protoTCP, 0, app, remote, false)
	want := netip.MustParseAddr("::ffff:10.64.0.2").As16()
	if !bytes.Equal(mapped[28:44], want[:]) {
		t.Fatalf("v4 source in an AF_INET6 request = % x", mapped[28:44])
	}
	if diagLiveTCPStates&(1<<linuxTCPListen) != 0 || diagLiveTCPStates&(1<<linuxTCPTimeWait) != 0 || diagLiveTCPStates&(1<<2) == 0 {
		t.Fatalf("live state mask %#x", diagLiveTCPStates)
	}
}

type diagMsgSpec struct {
	family, state uint8
	local, remote netip.AddrPort
	cookie        uint64
	uid, inode    uint32
	attrs         [][]byte
}

func nlAttr(typ uint16, payload ...byte) []byte {
	b := make([]byte, 4+len(payload))
	binary.NativeEndian.PutUint16(b, uint16(len(b)))
	binary.NativeEndian.PutUint16(b[2:], typ)
	copy(b[4:], payload)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func diagMsgBytes(s diagMsgSpec) []byte {
	b := make([]byte, diagMsgLen)
	ne := binary.NativeEndian
	b[0], b[1] = s.family, s.state
	binary.BigEndian.PutUint16(b[4:], s.local.Port())
	binary.BigEndian.PutUint16(b[6:], s.remote.Port())
	putDiagAddr(b[8:24], s.family, s.local.Addr())
	putDiagAddr(b[24:40], s.family, s.remote.Addr())
	ne.PutUint32(b[44:], uint32(s.cookie))
	ne.PutUint32(b[48:], uint32(s.cookie>>32))
	ne.PutUint32(b[64:], s.uid)
	ne.PutUint32(b[68:], s.inode)
	for _, a := range s.attrs {
		b = append(b, a...)
	}
	return b
}

func nlMessage(typ uint16, seq uint32, payload []byte) []byte {
	b := make([]byte, nlmsgHdrLen+len(payload))
	binary.NativeEndian.PutUint32(b, uint32(len(b)))
	binary.NativeEndian.PutUint16(b[4:], typ)
	binary.NativeEndian.PutUint32(b[8:], seq)
	copy(b[nlmsgHdrLen:], payload)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func TestParseDiagMsg(t *testing.T) {
	v4 := diagMsgBytes(diagMsgSpec{family: afInet, state: 2, local: app, remote: remote, cookie: 0x0000_0012_8000_0001, uid: 1000, inode: 4242})
	r, ok := parseDiagMsg(v4)
	want := diagRow{family: afInet, state: 2, local: app, remote: remote, uid: 1000, inode: 4242, cookie: 0x0000_0012_8000_0001}
	if !ok || r != want {
		t.Fatalf("v4 = %+v %v, want %+v", r, ok, want)
	}

	mapped := diagMsgBytes(diagMsgSpec{family: afInet6, state: 1, local: netip.MustParseAddrPort("[::ffff:10.64.0.2]:50000"), remote: netip.MustParseAddrPort("[::ffff:203.0.113.9]:443"), inode: 7})
	if r, ok := parseDiagMsg(mapped); !ok || r.local != app || r.remote != remote || r.family != afInet6 {
		t.Fatalf("v4-mapped = %+v %v", r, ok)
	}

	anyV6 := netip.MustParseAddrPort("[::]:3074")
	shutdown := nlAttr(8, 0)
	v6only := diagMsgBytes(diagMsgSpec{family: afInet6, state: 7, local: anyV6, inode: 8, attrs: [][]byte{shutdown, nlAttr(diagAttrV6Only, 1)}})
	if r, ok := parseDiagMsg(v6only); !ok || !r.v6only || r.local != anyV6 {
		t.Fatalf("v6only wildcard = %+v %v", r, ok)
	}
	dual := diagMsgBytes(diagMsgSpec{family: afInet6, state: 7, local: anyV6, inode: 9, attrs: [][]byte{nlAttr(diagAttrV6Only, 0)}})
	if r, ok := parseDiagMsg(dual); !ok || r.v6only {
		t.Fatalf("dual-stack wildcard = %+v %v", r, ok)
	}
	broken := append(diagMsgBytes(diagMsgSpec{family: afInet6, local: anyV6, inode: 9}), 0xff, 0x00, 11, 0)
	if r, ok := parseDiagMsg(broken); !ok || r.v6only {
		t.Fatalf("oversized attribute = %+v %v", r, ok)
	}

	if _, ok := parseDiagMsg(v4[:diagMsgLen-1]); ok {
		t.Fatal("short message parsed")
	}
	bad := slices.Clone(v4)
	bad[0] = 1
	if _, ok := parseDiagMsg(bad); ok {
		t.Fatal("unknown family parsed")
	}
}

func TestWalkNetlink(t *testing.T) {
	msg := diagMsgBytes(diagMsgSpec{family: afInet, local: app, remote: remote, inode: 1})
	errPayload := make([]byte, 4+nlmsgHdrLen)
	binary.NativeEndian.PutUint32(errPayload, uint32(0xfffffffe))
	buf := slices.Concat(
		nlMessage(sockDiagByFamily, 4, msg),
		nlMessage(sockDiagByFamily, 5, msg),
		nlMessage(nlmsgError, 5, errPayload),
		nlMessage(nlmsgDone, 5, make([]byte, 4)),
	)
	var seen []uint16
	var errno int
	err := walkNetlink(buf, func(typ uint16, seq uint32, data []byte) bool {
		if seq != 5 {
			return true
		}
		seen = append(seen, typ)
		if typ == nlmsgError {
			errno = netlinkErrno(data)
			return false
		}
		if typ == sockDiagByFamily {
			if r, ok := parseDiagMsg(data); !ok || r.local != app {
				t.Fatalf("message payload = %+v %v", r, ok)
			}
		}
		return true
	})
	if err != nil || !slices.Equal(seen, []uint16{sockDiagByFamily, nlmsgError}) || errno != 2 {
		t.Fatalf("walk = %v %v errno %d", seen, err, errno)
	}

	short := nlMessage(nlmsgDone, 1, nil)
	binary.NativeEndian.PutUint32(short, 8)
	if err := walkNetlink(short, func(uint16, uint32, []byte) bool { return true }); !errors.Is(err, errDiagMalformed) {
		t.Fatalf("short length: %v", err)
	}
	long := nlMessage(nlmsgDone, 1, make([]byte, 4))
	binary.NativeEndian.PutUint32(long, 64)
	if err := walkNetlink(long, func(uint16, uint32, []byte) bool { return true }); !errors.Is(err, errDiagMalformed) {
		t.Fatalf("overlong length: %v", err)
	}
	if netlinkErrno(nil) != -1 || netlinkErrno(make([]byte, 4)) != 0 {
		t.Fatal("errno decoding")
	}
}

func TestDiagRowsFor(t *testing.T) {
	wild4 := netip.AddrPortFrom(anyIP, app.Port())
	wild6 := netip.AddrPortFrom(netip.IPv6Unspecified(), app.Port())
	tcp := []diagRow{
		{state: linuxTCPListen, local: app, inode: 1},
		{state: linuxTCPTimeWait, local: app, remote: remote},
		{state: 2, local: app, remote: remote2, inode: 3},
		{state: 2, local: netip.AddrPortFrom(physIP, app.Port()), remote: remote, inode: 4},
		{state: 2, local: app, remote: remote, inode: 5},
		{state: 1, local: app, remote: remote, inode: 0},
	}
	inodes := func(rows []diagRow) []uint32 {
		var out []uint32
		for _, r := range rows {
			out = append(out, r.inode)
		}
		return out
	}
	if got := inodes(diagRowsFor(tcp, protoTCP, app, remote)); !slices.Equal(got, []uint32{5}) {
		t.Fatalf("tcp exact = %v", got)
	}
	if got := inodes(diagRowsFor(tcp, protoTCP, app, netip.AddrPort{})); !slices.Equal(got, []uint32{3, 5}) {
		t.Fatalf("tcp any remote = %v", got)
	}
	mappedApp := netip.MustParseAddrPort("[::ffff:10.64.0.2]:50000")
	if got := inodes(diagRowsFor(tcp, protoTCP, mappedApp, remote)); !slices.Equal(got, []uint32{5}) {
		t.Fatalf("mapped flow = %v", got)
	}

	udp := []diagRow{
		{local: wild4, inode: 10},
		{local: netip.AddrPortFrom(physIP, app.Port()), inode: 11},
		{local: wild6, inode: 12},
		{local: wild6, v6only: true, inode: 13},
		{local: netip.MustParseAddrPort("[2001:db8::1]:50000"), inode: 14},
		{local: netip.AddrPortFrom(tunIP, 50001), inode: 15},
		{local: app, inode: 16},
		{local: app, inode: 0},
	}
	if got := inodes(diagRowsFor(udp, protoUDP, app, remote)); !slices.Equal(got, []uint32{16, 10, 12}) {
		t.Fatalf("udp rows = %v, want exact first then wildcards", got)
	}
}
