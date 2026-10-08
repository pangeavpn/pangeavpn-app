package procmatch

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

// NETLINK_SOCK_DIAG wire layout (linux/netlink.h, linux/inet_diag.h).
const (
	nlmsgHdrLen      = 16
	nlmsgError       = 2
	nlmsgDone        = 3
	nlmFRequest      = 0x1
	nlmFDump         = 0x300
	sockDiagByFamily = 20

	diagReqLen       = nlmsgHdrLen + 56
	diagMsgLen       = 72
	diagAttrV6Only   = 11
	nlaTypeMask      = 0x3fff
	afInet           = 2
	afInet6          = 10
	diagAllStates    = 0xffffffff
	linuxTCPTimeWait = 6
	linuxTCPListen   = 10
)

// diagLiveTCPStates leaves out TIME_WAIT and LISTEN sockets.
const diagLiveTCPStates = diagAllStates &^ (1<<linuxTCPTimeWait | 1<<linuxTCPListen)

var (
	errDiagNotFound    = errors.New("socket not found")
	errDiagUnsupported = errors.New("socket diagnostics unavailable for this protocol")
	errDiagMalformed   = errors.New("malformed netlink message")
)

// diagRow is one inet_diag_msg; addresses are unmapped so dual-stack sockets compare as IPv4.
type diagRow struct {
	family uint8
	state  uint8
	local  netip.AddrPort
	remote netip.AddrPort
	uid    uint32
	inode  uint32
	cookie uint64
	v6only bool
}

// packDiagRequest builds an inet_diag_req_v2; an exact lookup names the socket by src (local) and dst.
func packDiagRequest(seq uint32, family, proto uint8, states uint32, src, dst netip.AddrPort, dump bool) []byte {
	b := make([]byte, diagReqLen)
	ne := binary.NativeEndian
	ne.PutUint32(b[0:], diagReqLen)
	ne.PutUint16(b[4:], sockDiagByFamily)
	flags := uint16(nlmFRequest)
	if dump {
		flags |= nlmFDump
	}
	ne.PutUint16(b[6:], flags)
	ne.PutUint32(b[8:], seq)
	b[16] = family
	b[17] = proto
	ne.PutUint32(b[20:], states)
	binary.BigEndian.PutUint16(b[24:], src.Port())
	binary.BigEndian.PutUint16(b[26:], dst.Port())
	putDiagAddr(b[28:44], family, src.Addr())
	putDiagAddr(b[44:60], family, dst.Addr())
	ne.PutUint64(b[64:], ^uint64(0))
	return b
}

func putDiagAddr(dst []byte, family uint8, a netip.Addr) {
	if !a.IsValid() {
		return
	}
	if family == afInet6 {
		s := a.As16()
		copy(dst, s[:])
		return
	}
	if a = a.Unmap(); a.Is4() {
		s := a.As4()
		copy(dst, s[:])
	}
}

// walkNetlink calls fn for each message in a datagram until fn returns false.
func walkNetlink(b []byte, fn func(typ uint16, seq uint32, data []byte) bool) error {
	ne := binary.NativeEndian
	for len(b) >= nlmsgHdrLen {
		l := int(ne.Uint32(b))
		if l < nlmsgHdrLen || l > len(b) {
			return errDiagMalformed
		}
		if !fn(ne.Uint16(b[4:]), ne.Uint32(b[8:]), b[nlmsgHdrLen:l]) {
			return nil
		}
		l = (l + 3) &^ 3
		if l >= len(b) {
			return nil
		}
		b = b[l:]
	}
	return nil
}

// netlinkErrno returns the positive errno of an NLMSG_ERROR payload (0 is an ack).
func netlinkErrno(data []byte) int {
	if len(data) < 4 {
		return -1
	}
	e := int32(binary.NativeEndian.Uint32(data))
	if e < 0 {
		e = -e
	}
	return int(e)
}

func parseDiagMsg(data []byte) (diagRow, bool) {
	if len(data) < diagMsgLen {
		return diagRow{}, false
	}
	ne := binary.NativeEndian
	r := diagRow{family: data[0], state: data[1]}
	src, ok := diagAddr(r.family, data[8:24])
	if !ok {
		return diagRow{}, false
	}
	dst, _ := diagAddr(r.family, data[24:40])
	r.local = netip.AddrPortFrom(src, binary.BigEndian.Uint16(data[4:]))
	r.remote = netip.AddrPortFrom(dst, binary.BigEndian.Uint16(data[6:]))
	r.cookie = uint64(ne.Uint32(data[44:])) | uint64(ne.Uint32(data[48:]))<<32
	r.uid = ne.Uint32(data[64:])
	r.inode = ne.Uint32(data[68:])
	attrs := data[diagMsgLen:]
	for len(attrs) >= 4 {
		l := int(ne.Uint16(attrs))
		if l < 4 || l > len(attrs) {
			break
		}
		attr := attrs[:l]
		if ne.Uint16(attr[2:])&nlaTypeMask == diagAttrV6Only && len(attr) > 4 {
			r.v6only = attr[4] != 0
		}
		l = (l + 3) &^ 3
		if l >= len(attrs) {
			break
		}
		attrs = attrs[l:]
	}
	return r, true
}

func diagAddr(family uint8, b []byte) (netip.Addr, bool) {
	switch family {
	case afInet:
		return netip.AddrFrom4([4]byte(b[:4])), true
	case afInet6:
		return netip.AddrFrom16([16]byte(b[:16])).Unmap(), true
	}
	return netip.Addr{}, false
}

// tcpDiagMatch keeps live sockets with the flow's exact endpoints; TIME_WAIT and request
// sockets carry no inode and so no owner.
func tcpDiagMatch(r diagRow, app, remote netip.AddrPort) bool {
	if r.state == linuxTCPListen || r.state == linuxTCPTimeWait || r.inode == 0 {
		return false
	}
	if r.local != app {
		return false
	}
	return !remote.IsValid() || r.remote == remote
}

// udpDiagMatch keeps sockets that can send from app: bound to its address, the IPv4 wildcard,
// or a dual-stack IPv6 wildcard.
func udpDiagMatch(r diagRow, app netip.AddrPort) bool {
	if r.local.Port() != app.Port() || r.inode == 0 {
		return false
	}
	a := r.local.Addr()
	switch {
	case a == app.Addr():
		return true
	case a.IsUnspecified() && a.Is4():
		return true
	case a.IsUnspecified() && a.Is6():
		return !r.v6only
	}
	return false
}

// diagRowsFor selects the rows that may own the flow; an exact-address UDP row sorts first.
func diagRowsFor(rows []diagRow, proto uint8, app, remote netip.AddrPort) []diagRow {
	app = netip.AddrPortFrom(app.Addr().Unmap(), app.Port())
	if remote.IsValid() {
		remote = netip.AddrPortFrom(remote.Addr().Unmap(), remote.Port())
	}
	var out []diagRow
	for _, r := range rows {
		switch proto {
		case protoTCP:
			if tcpDiagMatch(r, app, remote) {
				out = append(out, r)
			}
		case protoUDP:
			if udpDiagMatch(r, app) {
				if r.local.Addr() == app.Addr() {
					out = append([]diagRow{r}, out...)
				} else {
					out = append(out, r)
				}
			}
		}
	}
	return out
}
