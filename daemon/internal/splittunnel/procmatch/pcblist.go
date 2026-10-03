package procmatch

import (
	"encoding/binary"
	"net/netip"
)

// net.inet.{tcp,udp}.pcblist_n layout (xnu bsd/netinet/in_pcb.h, bsd/sys/socketvar.h).
const (
	xinpgenLen = 24

	xsoSocket = 0x01
	xsoRcvBuf = 0x02
	xsoSndBuf = 0x04
	xsoStats  = 0x08
	xsoInpcb  = 0x10
	xsoTcpcb  = 0x20

	pcbKindsUDP = xsoSocket | xsoRcvBuf | xsoSndBuf | xsoStats | xsoInpcb
	pcbKindsTCP = pcbKindsUDP | xsoTcpcb

	xinpcbMinLen   = 80
	xsocketMinLen  = 72
	xinpFPort      = 16
	xinpLPort      = 18
	xinpVFlag      = 44
	xinpFAddr      = 48
	xinpLAddr      = 64
	xinpAddr4      = 12
	xsoUID         = 64
	xsoLastPID     = 68
	xsoEPID        = 72
	xsoGenCnt      = 76
	xsoFlags       = 84
	xtcpState      = 36
	inpIPv4        = 0x1
	inpIPv6        = 0x2
	sofDelegated   = 0x04000000
	darwinListen   = 1
	darwinTimeWait = 10
)

type pcbRecord struct {
	local   netip.AddrPort
	remote  netip.AddrPort
	state   int32
	uid     uint32
	lastPID int32
	ePID    int32
	flags   uint32
	gencnt  uint64
}

// pcbOwner is a socket's owner candidates: the last user and, when it differs, the
// effective (delegating) process.
type pcbOwner struct {
	last int
	eff  int
}

func (r pcbRecord) owner() pcbOwner {
	o := pcbOwner{last: int(r.lastPID)}
	if r.ePID > 0 && r.ePID != r.lastPID {
		o.eff = int(r.ePID)
	}
	return o
}

func (o pcbOwner) pids() []int {
	var out []int
	if o.last > 0 {
		out = append(out, o.last)
	}
	if o.eff > 0 {
		out = append(out, o.eff)
	}
	return out
}

func (o pcbOwner) has(pid int) bool {
	return pid > 0 && (o.last == pid || o.eff == pid)
}

func roundup64(n int) int {
	return (n + 7) &^ 7
}

// parsePCBList walks a pcblist_n buffer by each sub-struct's length and kind, emitting a
// record once every kind of one socket has been seen.
func parsePCBList(buf []byte, tcp bool) []pcbRecord {
	if len(buf) < xinpgenLen {
		return nil
	}
	ne := binary.NativeEndian
	want := pcbKindsUDP
	if tcp {
		want = pcbKindsTCP
	}
	var out []pcbRecord
	var inp, so, tp []byte
	mask := 0
	pos := roundup64(int(ne.Uint32(buf)))
	for pos >= 0 && pos+8 <= len(buf) {
		l := int(ne.Uint32(buf[pos:]))
		kind := int(ne.Uint32(buf[pos+4:]))
		if l <= xinpgenLen || pos+l > len(buf) {
			break
		}
		sub := buf[pos : pos+l]
		pos += roundup64(l)
		if kind&want == 0 || kind&(kind-1) != 0 {
			continue
		}
		if mask&kind != 0 {
			mask, inp, so, tp = 0, nil, nil, nil
		}
		mask |= kind
		switch kind {
		case xsoInpcb:
			inp = sub
		case xsoSocket:
			so = sub
		case xsoTcpcb:
			tp = sub
		}
		if mask != want {
			continue
		}
		if r, ok := pcbRecordFrom(inp, so, tp, tcp); ok {
			out = append(out, r)
		}
		mask, inp, so, tp = 0, nil, nil, nil
	}
	return out
}

func pcbRecordFrom(inp, so, tp []byte, tcp bool) (pcbRecord, bool) {
	if len(inp) < xinpcbMinLen || len(so) < xsocketMinLen {
		return pcbRecord{}, false
	}
	ne := binary.NativeEndian
	r := pcbRecord{
		uid:     ne.Uint32(so[xsoUID:]),
		lastPID: int32(ne.Uint32(so[xsoLastPID:])),
		state:   -1,
	}
	if len(so) >= xsoEPID+4 {
		r.ePID = int32(ne.Uint32(so[xsoEPID:]))
	}
	if len(so) >= xsoGenCnt+8 {
		r.gencnt = ne.Uint64(so[xsoGenCnt:])
	}
	if len(so) >= xsoFlags+4 {
		r.flags = ne.Uint32(so[xsoFlags:])
	}
	if tcp && len(tp) >= xtcpState+4 {
		r.state = int32(ne.Uint32(tp[xtcpState:]))
	}
	fport := binary.BigEndian.Uint16(inp[xinpFPort:])
	lport := binary.BigEndian.Uint16(inp[xinpLPort:])
	var laddr, faddr netip.Addr
	switch vflag := inp[xinpVFlag]; {
	case vflag&inpIPv4 != 0:
		faddr = netip.AddrFrom4([4]byte(inp[xinpFAddr+xinpAddr4:]))
		laddr = netip.AddrFrom4([4]byte(inp[xinpLAddr+xinpAddr4:]))
	case vflag&inpIPv6 != 0:
		faddr = netip.AddrFrom16([16]byte(inp[xinpFAddr:])).Unmap()
		laddr = netip.AddrFrom16([16]byte(inp[xinpLAddr:])).Unmap()
	default:
		return pcbRecord{}, false
	}
	r.local = netip.AddrPortFrom(laddr, lport)
	r.remote = netip.AddrPortFrom(faddr, fport)
	return r, true
}

// darwinTCPState maps BSD TCP states onto the owner-table numbering (LISTEN, TIME_WAIT).
func darwinTCPState(s int32) uint32 {
	switch s {
	case darwinListen:
		return tcpStateListen
	case darwinTimeWait:
		return tcpStateTimeWait
	}
	return 0x100 + uint32(s)
}

// pcbSnapshot indexes pcblist records as owner-table rows; row pid n names owners[n-1].
type pcbSnapshot struct {
	tcp    []tcpRow
	udp    []udpRow
	owners []pcbOwner
	ids    map[pcbOwner]int
}

func (s *pcbSnapshot) idOf(o pcbOwner) int {
	if o.last <= 0 && o.eff <= 0 {
		return 0
	}
	if id, ok := s.ids[o]; ok {
		return id
	}
	if s.ids == nil {
		s.ids = make(map[pcbOwner]int)
	}
	s.owners = append(s.owners, o)
	s.ids[o] = len(s.owners)
	return len(s.owners)
}

func (s *pcbSnapshot) add(proto uint8, recs []pcbRecord) {
	for _, r := range recs {
		id := s.idOf(r.owner())
		switch proto {
		case protoTCP:
			s.tcp = append(s.tcp, tcpRow{state: darwinTCPState(r.state), local: r.local, remote: r.remote, pid: id})
		case protoUDP:
			s.udp = append(s.udp, udpRow{local: r.local, pid: id})
		}
	}
}

// owner applies the shared ambiguity rules; a socket without a known process has no owner.
func (s *pcbSnapshot) owner(f FlowID) (pcbOwner, bool) {
	id, ok := flowOwner(s.tcp, s.udp, f)
	if !ok || id <= 0 || id > len(s.owners) {
		return pcbOwner{}, false
	}
	return s.owners[id-1], true
}

// verdictForCandidates bypasses when any candidate's chain matches and none is protected;
// it returns the index of the candidate the owner should name.
func verdictForCandidates(rules *Rules, never []*Rules, self procKey, views []lineageView) (int, Verdict, []string) {
	for _, v := range views {
		if v.protected(never, self) {
			return 0, VerdictTunnel, nil
		}
	}
	for i, v := range views {
		if rules.MatchChain(v.chain) {
			return i, VerdictBypass, v.chain
		}
	}
	if len(views) == 0 {
		return 0, VerdictTunnel, nil
	}
	return 0, VerdictTunnel, views[0].chain
}
