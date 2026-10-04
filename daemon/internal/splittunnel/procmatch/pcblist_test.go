package procmatch

import (
	"encoding/binary"
	"net/netip"
	"slices"
	"testing"
)

type pcbSpec struct {
	local, remote netip.AddrPort
	vflag         byte
	state         int32
	uid           uint32
	last, epid    int32
	flags         uint32
	gencnt        uint64
	inpLen, soLen int
	extraKind     bool
	skipStats     bool
}

func pcbHeader() []byte {
	h := make([]byte, xinpgenLen)
	binary.NativeEndian.PutUint32(h, xinpgenLen)
	binary.NativeEndian.PutUint32(h[4:], 3)
	return h
}

// appendSub adds one xgen sub-struct padded to 8 bytes, as get_pcblist_n lays them out.
func appendSub(buf []byte, kind uint32, length int, fill func(b []byte)) []byte {
	b := make([]byte, roundup64(length))
	binary.NativeEndian.PutUint32(b, uint32(length))
	binary.NativeEndian.PutUint32(b[4:], kind)
	if fill != nil {
		fill(b[:length])
	}
	return append(buf, b...)
}

func appendPCB(buf []byte, tcp bool, s pcbSpec) []byte {
	ne := binary.NativeEndian
	inpLen, soLen := 104, 104
	if s.inpLen != 0 {
		inpLen = s.inpLen
	}
	if s.soLen != 0 {
		soLen = s.soLen
	}
	vflag := s.vflag
	if vflag == 0 {
		vflag = inpIPv4
	}
	if !s.remote.IsValid() {
		s.remote = netip.AddrPortFrom(netip.IPv4Unspecified(), 0)
		if vflag&inpIPv4 == 0 {
			s.remote = netip.AddrPortFrom(netip.IPv6Unspecified(), 0)
		}
	}
	buf = appendSub(buf, xsoInpcb, inpLen, func(b []byte) {
		if len(b) < xinpcbMinLen {
			return
		}
		binary.BigEndian.PutUint16(b[xinpFPort:], s.remote.Port())
		binary.BigEndian.PutUint16(b[xinpLPort:], s.local.Port())
		b[xinpVFlag] = vflag
		if vflag&inpIPv4 != 0 {
			r, l := s.remote.Addr().As4(), s.local.Addr().As4()
			copy(b[xinpFAddr+xinpAddr4:], r[:])
			copy(b[xinpLAddr+xinpAddr4:], l[:])
		} else {
			r, l := s.remote.Addr().As16(), s.local.Addr().As16()
			copy(b[xinpFAddr:], r[:])
			copy(b[xinpLAddr:], l[:])
		}
	})
	buf = appendSub(buf, xsoSocket, soLen, func(b []byte) {
		if len(b) < xsocketMinLen {
			return
		}
		ne.PutUint32(b[xsoUID:], s.uid)
		ne.PutUint32(b[xsoLastPID:], uint32(s.last))
		if len(b) >= xsoFlags+4 {
			ne.PutUint32(b[xsoEPID:], uint32(s.epid))
			ne.PutUint64(b[xsoGenCnt:], s.gencnt)
			ne.PutUint32(b[xsoFlags:], s.flags)
		}
	})
	buf = appendSub(buf, xsoRcvBuf, 32, nil)
	if s.extraKind {
		buf = appendSub(buf, 0x40, 44, nil)
	}
	buf = appendSub(buf, xsoSndBuf, 32, nil)
	if !s.skipStats {
		buf = appendSub(buf, xsoStats, 140, nil)
	}
	if tcp {
		buf = appendSub(buf, xsoTcpcb, 204, func(b []byte) {
			ne.PutUint32(b[xtcpState:], uint32(s.state))
		})
	}
	return buf
}

func buildPCBList(tcp bool, specs ...pcbSpec) []byte {
	buf := pcbHeader()
	for _, s := range specs {
		buf = appendPCB(buf, tcp, s)
	}
	return append(buf, pcbHeader()...)
}

var (
	pcbApp    = netip.MustParseAddrPort("10.64.0.2:50000")
	pcbRemote = netip.MustParseAddrPort("203.0.113.9:443")
)

func TestParsePCBListTCP(t *testing.T) {
	specs := []pcbSpec{
		{local: netip.MustParseAddrPort("10.64.0.2:40000"), remote: netip.MustParseAddrPort("198.51.100.1:80"), state: 4, uid: 501, last: 300, gencnt: 1},
		{local: netip.MustParseAddrPort("0.0.0.0:8080"), state: darwinListen, uid: 0, last: 1, extraKind: true},
		{local: pcbApp, remote: pcbRemote, state: 2, uid: 501, last: 410, epid: 420, flags: sofDelegated, gencnt: 0x1122334455},
	}
	recs := parsePCBList(buildPCBList(true, specs...), true)
	if len(recs) != 3 {
		t.Fatalf("parsed %d records, want 3: %+v", len(recs), recs)
	}
	got := recs[2]
	want := pcbRecord{local: pcbApp, remote: pcbRemote, state: 2, uid: 501, lastPID: 410, ePID: 420, flags: sofDelegated, gencnt: 0x1122334455}
	if got != want {
		t.Fatalf("third record = %+v, want %+v", got, want)
	}
	if recs[1].state != darwinListen || recs[1].local != netip.MustParseAddrPort("0.0.0.0:8080") {
		t.Fatalf("record after an unknown kind = %+v", recs[1])
	}
	if recs[0].lastPID != 300 || recs[0].remote.Port() != 80 {
		t.Fatalf("first record = %+v", recs[0])
	}
	if recs := parsePCBList(buildPCBList(false, specs...), true); len(recs) != 0 {
		t.Fatalf("records without a tcpcb emitted as tcp: %+v", recs)
	}
}

func TestParsePCBListUDP(t *testing.T) {
	specs := []pcbSpec{
		{local: netip.MustParseAddrPort("127.0.0.1:5353"), last: 50},
		{local: netip.MustParseAddrPort("[::1]:5353"), vflag: inpIPv6, last: 51},
		{local: netip.MustParseAddrPort("0.0.0.0:3074"), vflag: inpIPv4 | inpIPv6, last: 52, epid: 52},
		{local: netip.MustParseAddrPort("[::ffff:10.64.0.2]:3075"), vflag: inpIPv6, last: 53},
	}
	recs := parsePCBList(buildPCBList(false, specs...), false)
	if len(recs) != 4 {
		t.Fatalf("parsed %d records", len(recs))
	}
	wantLocal := []string{"127.0.0.1:5353", "[::1]:5353", "0.0.0.0:3074", "10.64.0.2:3075"}
	for i, r := range recs {
		if r.local.String() != wantLocal[i] || r.state != -1 || int(r.lastPID) != 50+i {
			t.Errorf("record %d = %+v, want local %s", i, r, wantLocal[i])
		}
	}
}

func TestParsePCBListDamaged(t *testing.T) {
	a := pcbSpec{local: netip.MustParseAddrPort("10.64.0.2:1000"), last: 10}
	b := pcbSpec{local: netip.MustParseAddrPort("10.64.0.2:2000"), last: 20}
	c := pcbSpec{local: netip.MustParseAddrPort("10.64.0.2:3000"), last: 30}
	full := buildPCBList(false, a, b, c)
	lastStart := len(full) - xinpgenLen - (len(buildPCBList(false, c)) - 2*xinpgenLen)
	for _, cut := range []int{lastStart + 8, lastStart + 120, len(full) - xinpgenLen - 8} {
		if recs := parsePCBList(full[:cut], false); len(recs) != 2 || recs[1].lastPID != 20 {
			t.Errorf("cut at %d: %+v, want the two complete records", cut, recs)
		}
	}

	small := b
	small.inpLen = 64
	if recs := parsePCBList(buildPCBList(false, a, small, c), false); len(recs) != 2 || recs[0].lastPID != 10 || recs[1].lastPID != 30 {
		t.Fatalf("undersized xinpcb: %+v", recs)
	}
	small = b
	small.soLen = 68
	if recs := parsePCBList(buildPCBList(false, a, small, c), false); len(recs) != 2 || recs[1].lastPID != 30 {
		t.Fatalf("undersized xsocket: %+v", recs)
	}
	short := b
	short.soLen = 72
	recs := parsePCBList(buildPCBList(false, a, short, c), false)
	if len(recs) != 3 || recs[1].lastPID != 20 || recs[1].ePID != 0 || recs[1].flags != 0 {
		t.Fatalf("xsocket without e_pid: %+v", recs)
	}

	missing := b
	missing.skipStats = true
	if recs := parsePCBList(buildPCBList(false, a, missing, c), false); len(recs) != 2 || recs[0].lastPID != 10 || recs[1].lastPID != 30 {
		t.Fatalf("record missing a kind: %+v; the next record must resynchronise", recs)
	}

	odd := pcbHeader()
	binary.NativeEndian.PutUint32(odd, 20)
	odd = append(odd[:24], buildPCBList(false, a)[24:]...)
	if recs := parsePCBList(odd, false); len(recs) != 1 {
		t.Fatalf("header length rounded: %+v", recs)
	}
	if parsePCBList(nil, true) != nil || parsePCBList(make([]byte, 10), true) != nil {
		t.Fatal("short buffers parsed")
	}
	if recs := parsePCBList(pcbHeader(), true); len(recs) != 0 {
		t.Fatalf("empty list = %+v", recs)
	}
}

func TestPCBOwnerCandidates(t *testing.T) {
	cases := []struct {
		name string
		rec  pcbRecord
		want []int
	}{
		{"delegated", pcbRecord{lastPID: 100, ePID: 200, flags: sofDelegated}, []int{100, 200}},
		{"not delegated, no e_pid", pcbRecord{lastPID: 100}, []int{100}},
		{"e_pid equals last_pid", pcbRecord{lastPID: 100, ePID: 100}, []int{100}},
		{"other creator", pcbRecord{lastPID: 100, ePID: 300}, []int{100, 300}},
		{"kernel socket", pcbRecord{}, nil},
	}
	for _, tc := range cases {
		o := tc.rec.owner()
		if got := o.pids(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: pids = %v, want %v", tc.name, got, tc.want)
		}
		for _, p := range tc.want {
			if !o.has(p) {
				t.Errorf("%s: has(%d) = false", tc.name, p)
			}
		}
		if o.has(0) || o.has(999) {
			t.Errorf("%s: has() accepts a pid that is not a candidate", tc.name)
		}
	}
}

func TestPCBSnapshotOwner(t *testing.T) {
	var s pcbSnapshot
	s.add(protoTCP, []pcbRecord{
		{local: pcbApp, state: darwinListen, lastPID: 9},
		{local: pcbApp, remote: pcbRemote, state: darwinTimeWait, lastPID: 8},
		{local: pcbApp, remote: pcbRemote, state: 2, lastPID: 410, ePID: 420, flags: sofDelegated},
		{local: netip.MustParseAddrPort("10.64.0.2:50001"), remote: pcbRemote, state: 4},
	})
	wild := netip.MustParseAddrPort("0.0.0.0:3074")
	exact := netip.MustParseAddrPort("10.64.0.2:3074")
	s.add(protoUDP, []pcbRecord{
		{local: wild, lastPID: 500},
		{local: exact, lastPID: 500},
		{local: netip.MustParseAddrPort("0.0.0.0:3075"), lastPID: 500},
		{local: netip.MustParseAddrPort("10.64.0.2:3075"), lastPID: 600},
		{local: netip.MustParseAddrPort("[::]:3076"), lastPID: 700, ePID: 701},
	})
	if o, ok := s.owner(FlowID{Proto: protoTCP, App: pcbApp, Remote: pcbRemote}); !ok || o != (pcbOwner{410, 420}) {
		t.Fatalf("tcp owner = %+v %v", o, ok)
	}
	if _, ok := s.owner(FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.64.0.2:50001"), Remote: pcbRemote}); ok {
		t.Fatal("socket without a process got an owner")
	}
	if o, ok := s.owner(FlowID{Proto: protoUDP, App: exact}); !ok || o != (pcbOwner{last: 500}) {
		t.Fatalf("udp exact + wildcard, one owner = %+v %v", o, ok)
	}
	if _, ok := s.owner(FlowID{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:3075")}); ok {
		t.Fatal("udp port shared by two processes got an owner")
	}
	if o, ok := s.owner(FlowID{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:3076")}); !ok || o != (pcbOwner{700, 701}) {
		t.Fatalf("dual-stack wildcard = %+v %v", o, ok)
	}
	if darwinTCPState(darwinListen) != tcpStateListen || darwinTCPState(darwinTimeWait) != tcpStateTimeWait || darwinTCPState(2) == tcpStateListen {
		t.Fatal("tcp state mapping")
	}
}

func TestVerdictForCandidates(t *testing.T) {
	env := darwinEnv()
	safari := "/Applications/Safari.app/Contents/MacOS/Safari"
	webkit := "/System/Library/Frameworks/WebKit.framework/Versions/A/XPCServices/com.apple.WebKit.Networking.xpc/Contents/MacOS/com.apple.WebKit.Networking"
	l := newLineage()
	refresh(l,
		proc(1, 0, 0, "/sbin/launchd"),
		proc(300, 1, 5, safari),
		proc(310, 1, 6, webkit),
		proc(400, 1, 7, "/Applications/PangeaVPN.app/Contents/MacOS/PangeaVPN"),
	)
	view := func(pid int, start int64) lineageView { return l.view(procKey{pid, start}, env.isStop) }
	rules := compileIn(t, env, "/Applications/Safari.app")
	never := []*Rules{compileImages(env, []string{"/Applications/PangeaVPN.app/Contents/MacOS/PangeaVPN"}), nil}

	idx, v, chain := verdictForCandidates(rules, never, procKey{}, []lineageView{view(310, 6), view(300, 5)})
	if v != VerdictBypass || idx != 1 || !slices.Equal(chain, []string{safari}) {
		t.Fatalf("delegated flow = %d %v %q, want bypass via the delegating app", idx, v, chain)
	}
	idx, v, chain = verdictForCandidates(rules, never, procKey{}, []lineageView{view(310, 6)})
	if v != VerdictTunnel || idx != 0 || !slices.Equal(chain, []string{webkit}) {
		t.Fatalf("networking process alone = %d %v %q", idx, v, chain)
	}
	if _, v, chain := verdictForCandidates(rules, never, procKey{}, []lineageView{view(300, 5), view(400, 7)}); v != VerdictTunnel || chain != nil {
		t.Fatalf("candidate under a never-bypass image = %v %q", v, chain)
	}
	if _, v, _ := verdictForCandidates(rules, never, procKey{310, 6}, []lineageView{view(310, 6), view(300, 5)}); v != VerdictTunnel {
		t.Fatal("classifier's own process bypassed")
	}
	if _, v, chain := verdictForCandidates(rules, never, procKey{}, nil); v != VerdictTunnel || chain != nil {
		t.Fatal("no candidates must tunnel")
	}
}
