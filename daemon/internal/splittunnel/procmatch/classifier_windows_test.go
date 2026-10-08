//go:build windows

package procmatch

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/sagernet/sing/common/winiphlpapi"
	"golang.org/x/sys/windows"
)

func putRow(buf []byte, off int, words ...[4]byte) {
	for i, w := range words {
		copy(buf[off+4*i:], w[:])
	}
}

func le32(v uint32) [4]byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	return b
}

func TestDecodeOwnerTables(t *testing.T) {
	const tcpRowSize = 24
	tcpBuf := make([]byte, 4+3*tcpRowSize)
	binary.LittleEndian.PutUint32(tcpBuf, 3)
	putRow(tcpBuf, 4, le32(3), [4]byte{10, 64, 0, 2}, [4]byte{0xC3, 0x50, 0xAA, 0xBB}, [4]byte{203, 0, 113, 9}, [4]byte{0x01, 0xBB, 0x12, 0x34}, le32(4321))
	putRow(tcpBuf, 4+tcpRowSize, le32(11), [4]byte{10, 64, 0, 2}, [4]byte{0xC3, 0x50}, [4]byte{203, 0, 113, 9}, [4]byte{0x01, 0xBB}, le32(0))
	putRow(tcpBuf, 4+2*tcpRowSize, le32(5), [4]byte{10, 64, 0, 2}, [4]byte{0xC3, 0x50}, [4]byte{198, 51, 100, 7}, [4]byte{0x00, 0x50}, le32(99))
	if unsafe.Sizeof(winiphlpapi.MibTcpRowOwnerPid{}) != tcpRowSize || unsafe.Offsetof(winiphlpapi.MibTcpTableOwnerPid{}.Table) != 4 {
		t.Fatal("MIB_TCPROW_OWNER_PID layout changed")
	}
	n := int(binary.LittleEndian.Uint32(tcpBuf))
	raw := unsafe.Slice((*winiphlpapi.MibTcpRowOwnerPid)(unsafe.Pointer(&tcpBuf[4])), n)
	var tcp []tcpRow
	for _, r := range raw {
		tcp = append(tcp, tcpRowFrom(r))
	}
	want := tcpRow{state: 3, local: netip.MustParseAddrPort("10.64.0.2:50000"), remote: netip.MustParseAddrPort("203.0.113.9:443"), pid: 4321}
	if tcp[0] != want {
		t.Fatalf("row 0 = %+v, want %+v", tcp[0], want)
	}
	if tcp[2].remote != netip.MustParseAddrPort("198.51.100.7:80") || tcp[2].pid != 99 {
		t.Fatalf("row 2 = %+v", tcp[2])
	}
	if pid, ok := tcpOwner(tcp, want.local, want.remote); !ok || pid != 4321 {
		t.Fatalf("owner = %d %v; the TIME_WAIT duplicate must be ignored", pid, ok)
	}

	const udpRowSize = 12
	udpBuf := make([]byte, 4+2*udpRowSize)
	binary.LittleEndian.PutUint32(udpBuf, 2)
	putRow(udpBuf, 4, [4]byte{0, 0, 0, 0}, [4]byte{0x0D, 0x05, 0xFF, 0xFF}, le32(77))
	putRow(udpBuf, 4+udpRowSize, [4]byte{10, 64, 0, 2}, [4]byte{0x0D, 0x05}, le32(78))
	if unsafe.Sizeof(winiphlpapi.MibUdpRowOwnerPid{}) != udpRowSize || unsafe.Offsetof(winiphlpapi.MibUdpTableOwnerPid{}.Table) != 4 {
		t.Fatal("MIB_UDPROW_OWNER_PID layout changed")
	}
	rawUDP := unsafe.Slice((*winiphlpapi.MibUdpRowOwnerPid)(unsafe.Pointer(&udpBuf[4])), 2)
	udp := []udpRow{udpRowFrom(rawUDP[0]), udpRowFrom(rawUDP[1])}
	if udp[0] != (udpRow{netip.MustParseAddrPort("0.0.0.0:3333"), 77}) || udp[1] != (udpRow{netip.MustParseAddrPort("10.64.0.2:3333"), 78}) {
		t.Fatalf("udp rows = %+v", udp)
	}
	if _, ok := udpOwner(udp, netip.MustParseAddrPort("10.64.0.2:3333")); ok {
		t.Fatal("wildcard and exact rows of different processes must be ambiguous")
	}
}

func TestParseSPISynthetic(t *testing.T) {
	size := int(unsafe.Sizeof(windows.SYSTEM_PROCESS_INFORMATION{}))
	entry := (size + 64 + 7) &^ 7
	procs := []spiProc{{0, 0, 0}, {4, 0, 100}, {1234, 4, 132000000000000000}, {5678, 1234, 132000000000000001}}
	buf := make([]byte, entry*len(procs))
	for i, p := range procs {
		e := (*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&buf[i*entry]))
		if i < len(procs)-1 {
			e.NextEntryOffset = uint32(entry)
		}
		e.UniqueProcessID = uintptr(p.pid)
		e.InheritedFromUniqueProcessID = uintptr(p.ppid)
		e.CreateTime = p.start
	}
	if got := parseSPI(buf); !slices.Equal(got, procs) {
		t.Fatalf("parsed %+v, want %+v", got, procs)
	}
	if got := parseSPI(buf[:entry+size-1]); !slices.Equal(got, procs[:1]) {
		t.Fatalf("truncated buffer parsed %+v", got)
	}
	if got := parseSPI(buf[:size-1]); len(got) != 0 {
		t.Fatalf("short buffer parsed %+v", got)
	}
	bad := slices.Clone(buf)
	(*windows.SYSTEM_PROCESS_INFORMATION)(unsafe.Pointer(&bad[0])).NextEntryOffset = uint32(len(bad) + entry)
	if got := parseSPI(bad); !slices.Equal(got, procs[:1]) {
		t.Fatalf("out-of-range next offset parsed %+v", got)
	}
}

func TestSystemInformationLayout(t *testing.T) {
	if windows.SystemProcessIdInformation != 0x58 || windows.SystemProcessInformation != 5 {
		t.Fatalf("information classes: %#x %#x", windows.SystemProcessIdInformation, windows.SystemProcessInformation)
	}
	if unsafe.Sizeof(systemProcessIDInformation{}) != 3*unsafe.Sizeof(uintptr(0)) {
		t.Fatalf("SYSTEM_PROCESS_ID_INFORMATION size %d", unsafe.Sizeof(systemProcessIDInformation{}))
	}
}

func ownImage(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return normalizeProcessPath(exe)
}

func TestLiveProcessSources(t *testing.T) {
	pid := os.Getpid()
	start, err := processStart(pid)
	if err != nil {
		t.Fatal(err)
	}
	info, err := processInfo(pid)
	if err != nil {
		t.Fatal(err)
	}
	if info.start != start || info.ppid != os.Getppid() || info.path != ownImage(t) {
		t.Fatalf("processInfo = %+v, want start %d ppid %d path %q", info, start, os.Getppid(), ownImage(t))
	}
	var buf []byte
	procs, err := systemProcesses(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if len(procs) < 10 {
		t.Fatalf("snapshot has only %d processes", len(procs))
	}
	i := slices.IndexFunc(procs, func(p spiProc) bool { return p.pid == pid })
	if i < 0 {
		t.Fatal("snapshot misses the test process")
	}
	if procs[i].ppid != os.Getppid() || procs[i].start != start {
		t.Fatalf("snapshot entry for self = %+v, want ppid %d start %d", procs[i], os.Getppid(), start)
	}
	nt, err := ntImagePath(pid)
	if err != nil || !strings.HasPrefix(strings.ToLower(nt), `\device\`) {
		t.Fatalf("nt image path = %q, %v", nt, err)
	}
	var d dosDevices
	dos, ok := d.toDos(nt)
	if !ok || normalizeProcessPath(dos) != ownImage(t) {
		t.Fatalf("nt path %q mapped to %q %v, want %q", nt, dos, ok, ownImage(t))
	}
	if f := filetimeNow(); f < start {
		t.Fatalf("precise now %d is before own start %d", f, start)
	}
}

func hostWinEnv() *ruleEnv {
	return newRuleEnv("windows", hostSettings())
}

func TestLiveSelfClassification(t *testing.T) {
	c, err := NewClassifier(Options{SelfPID: os.Getpid(), Logf: t.Logf})
	if err != nil {
		t.Fatalf("NewClassifier: %v", err)
	}
	defer c.Close()
	s := openOwnSockets(t)
	start, err := processStart(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	exe, _ := os.Executable()
	rules := compileIn(t, hostWinEnv(), exe)
	res := c.Classify(rules, []FlowID{s.tcp, s.udp})
	for i, r := range res {
		if r.Verdict != VerdictTunnel || r.Owner.PID != os.Getpid() || r.Owner.Start != start || r.Owner.Chain != nil {
			t.Fatalf("flow %d: %+v; the classifier's own process must resolve and never bypass", i, r)
		}
	}

	checks := []SocketCheck{
		{Proto: protoTCP, App: s.tcp.App, Owner: res[0].Owner},
		{Proto: protoUDP, App: s.udp.App, Owner: res[1].Owner},
		{Proto: protoUDP, App: s.udp.App, Owner: Owner{PID: os.Getpid(), Start: start + 1}},
		{Proto: protoUDP, App: s.udp.App, Owner: Owner{PID: os.Getppid(), Start: start}},
		{Proto: protoUDP, App: netip.MustParseAddrPort("127.0.0.1:1"), Owner: res[1].Owner},
	}
	if got := c.Validate(checks); !slices.Equal(got, []bool{true, true, false, false, false}) {
		t.Fatalf("Validate = %v", got)
	}
	if st := c.(StatsSource).Stats(); st.Lookups < 2 || st.Refreshes < 1 {
		t.Fatalf("stats = %+v", st)
	}
	if got := c.Classify(nil, []FlowID{s.tcp}); len(got) != 1 || got[0].Verdict != VerdictTunnel {
		t.Fatalf("empty rules = %+v", got)
	}
}

func TestLiveOwnImageChain(t *testing.T) {
	c, err := newWinClassifier(hostWinEnv(), 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	s := openOwnSockets(t)
	exe, _ := os.Executable()
	env := hostWinEnv()
	match := compileIn(t, env, exe)
	other := compileIn(t, env, `C:\Nowhere\other.exe`)
	for _, f := range []FlowID{s.tcp, s.udp} {
		r := c.Classify(match, []FlowID{f})[0]
		if r.Verdict != VerdictBypass || r.Owner.PID != os.Getpid() || len(r.Owner.Chain) == 0 || r.Owner.Chain[0] != ownImage(t) {
			t.Fatalf("proto %d with own image rule: %+v", f.Proto, r)
		}
		r = c.Classify(other, []FlowID{f})[0]
		if r.Verdict != VerdictTunnel || r.Owner.PID != os.Getpid() || len(r.Owner.Chain) == 0 || r.Owner.Chain[0] != ownImage(t) {
			t.Fatalf("proto %d with another rule: %+v", f.Proto, r)
		}
	}
	unknown := FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.255.255.1:1"), Remote: netip.MustParseAddrPort("10.255.255.2:2")}
	if r := c.Classify(match, []FlowID{unknown})[0]; r.Verdict != VerdictTunnel || r.Owner.PID != 0 {
		t.Fatalf("flow without a socket: %+v", r)
	}
}

func TestLiveChildAncestry(t *testing.T) {
	h := startHelperTree(t, ".exe")
	env := hostWinEnv()
	c, err := newWinClassifier(env, 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	launcherRule := compileIn(t, env, h.launcherPath)
	gameRule := compileIn(t, env, h.gamePath)
	dirRule := compileIn(t, env, filepath.Dir(h.gamePath)+`\`)
	otherRule := compileIn(t, env, `C:\Nowhere\other.exe`)
	game, launcher := normalizeWinImage(h.gamePath), normalizeWinImage(h.launcherPath)

	expect := func(label string, rules *Rules, want Verdict, wantChain []string) []Result {
		t.Helper()
		res := c.Classify(rules, h.flows())
		for i, r := range res {
			if r.Verdict != want || r.Owner.PID != h.game.PID || len(r.Owner.Chain) < len(wantChain) || !slices.Equal(r.Owner.Chain[:len(wantChain)], wantChain) {
				t.Fatalf("%s flow %d: %+v, want %v with chain prefix %q", label, i, r, want, wantChain)
			}
		}
		return res
	}
	res := expect("launcher rule", launcherRule, VerdictBypass, []string{game, launcher})
	expect("game rule", gameRule, VerdictBypass, []string{game, launcher})
	expect("dir rule", dirRule, VerdictBypass, []string{game, launcher})
	expect("other rule", otherRule, VerdictTunnel, []string{game, launcher})

	checks := []SocketCheck{{Proto: protoTCP, App: h.flows()[0].App, Owner: res[0].Owner}, {Proto: protoUDP, App: h.flows()[1].App, Owner: res[1].Owner}}
	if got := c.Validate(checks); !slices.Equal(got, []bool{true, true}) {
		t.Fatalf("Validate = %v", got)
	}

	if err := c.refresh(); err != nil {
		t.Fatal(err)
	}
	h.stopLauncher(t)
	if err := c.refresh(); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	var retained *lineageNode
	for k, n := range c.lin.nodes {
		if k.pid == h.launcherPID {
			retained = n
		}
	}
	c.mu.Unlock()
	if retained == nil || retained.alive || retained.refs == 0 {
		t.Fatalf("launcher lineage node = %+v, want retained after exit", retained)
	}
	expect("launcher rule after launcher exit", launcherRule, VerdictBypass, []string{game, launcher})

	late, err := newWinClassifier(env, 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer late.Close()
	for i, r := range late.Classify(launcherRule, h.flows()) {
		if r.Verdict != VerdictTunnel || r.Owner.PID != h.game.PID || !slices.Equal(r.Owner.Chain, []string{game}) {
			t.Fatalf("flow %d seen only after its launcher exited: %+v", i, r)
		}
	}
}

func TestLiveProtectedTrees(t *testing.T) {
	h := startHelperTree(t, ".exe")
	env := hostWinEnv()
	gameRule := compileIn(t, env, h.gamePath)

	self, err := NewClassifier(Options{SelfPID: os.Getpid(), Logf: t.Logf})
	if err != nil {
		t.Fatal(err)
	}
	defer self.Close()
	for i, r := range self.Classify(gameRule, h.flows()) {
		if r.Verdict != VerdictTunnel || r.Owner.PID != h.game.PID || r.Owner.Chain != nil {
			t.Fatalf("flow %d of the classifier's own descendant: %+v", i, r)
		}
	}

	never, err := newWinClassifier(env, 0, []string{strings.ToUpper(h.launcherPath)}, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer never.Close()
	for i, r := range never.Classify(gameRule, h.flows()) {
		if r.Verdict != VerdictTunnel || r.Owner.PID != h.game.PID {
			t.Fatalf("flow %d below a never-bypass image: %+v", i, r)
		}
	}

	protected, errs := compileRules(env, []string{h.gamePath}, []string{h.launcherPath})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	plain, err := newWinClassifier(env, 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer plain.Close()
	for i, r := range plain.Classify(protected, h.flows()) {
		if r.Verdict != VerdictTunnel {
			t.Fatalf("flow %d below an image protected by the rule set: %+v", i, r)
		}
	}
}

func TestSelfTestRejectsWrongSelf(t *testing.T) {
	c, err := NewClassifier(Options{SelfPID: os.Getppid()})
	if err == nil {
		c.Close()
		t.Fatal("self-test passed for another process")
	}
	if !strings.Contains(err.Error(), "self-test") {
		t.Fatalf("error = %v", err)
	}
}

func TestRefreshFollowsRules(t *testing.T) {
	c, err := newWinClassifier(hostWinEnv(), 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.start()
	defer c.Close()
	c.ObserveRules(nil)
	time.Sleep(100 * time.Millisecond)
	before := c.Stats().Refreshes
	time.Sleep(1300 * time.Millisecond)
	if after := c.Stats().Refreshes; after != before {
		t.Fatalf("refreshed %d times with no rules", after-before)
	}
	c.ObserveRules(compileIn(t, winEnv(), `C:\Games\`))
	deadline := time.Now().Add(time.Second)
	for c.Stats().Refreshes == before {
		if time.Now().After(deadline) {
			t.Fatal("rules change did not trigger a refresh")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	c.Close()
	if d := time.Since(start); d > time.Second {
		t.Fatalf("Close took %v", d)
	}
}

func TestStaleClassifyKeepsRefreshOff(t *testing.T) {
	c, err := newWinClassifier(hostWinEnv(), 0, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.start()
	defer c.Close()
	c.ObserveRules(nil)
	// An engine still holding the previous rules classifies after they were emptied.
	flow := FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("127.0.0.1:1"), Remote: netip.MustParseAddrPort("127.0.0.1:2")}
	c.Classify(compileIn(t, winEnv(), `C:\Games\`), []FlowID{flow})
	before := c.Stats().Refreshes
	time.Sleep(1300 * time.Millisecond)
	if n := c.Stats().Refreshes - before; n != 0 {
		t.Fatalf("refreshed %d times after the rules were emptied", n)
	}
}

func TestLongPathNormalisation(t *testing.T) {
	dir := filepath.Join(longTempDir(t), "A Rather Long Directory Name")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	long := filepath.Join(dir, "LongExecutableName.exe")
	if err := os.WriteFile(long, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p, _ := windows.UTF16PtrFromString(long)
	buf := make([]uint16, 1024)
	n, err := windows.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Fatal(err)
	}
	short := windows.UTF16ToString(buf[:n])
	if !strings.Contains(short, "~") {
		t.Skip("8.3 names are disabled on this volume")
	}
	if got := normalizeProcessPath(short); got != strings.ToLower(long) {
		t.Fatalf("normalizeProcessPath(%q) = %q, want %q", short, got, strings.ToLower(long))
	}
	rules := compileIn(t, hostWinEnv(), long)
	if !rules.MatchPath(normalizeProcessPath(short)) {
		t.Fatal("8.3 launch does not match the long-name rule")
	}
}

func TestResolveJunction(t *testing.T) {
	base := longTempDir(t)
	target := filepath.Join(base, "Library", "Game")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "game.exe"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "Linked")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, filepath.Join(base, "Library")).CombinedOutput(); err != nil {
		t.Skipf("mklink /J: %v %s", err, out)
	}
	env := hostWinEnv()
	file := compileIn(t, env, filepath.Join(link, "Game", "game.exe"))
	dir := compileIn(t, env, filepath.Join(link, "Game")+`\`)
	realPath := normalizeWinImage(filepath.Join(target, "game.exe"))
	if !file.MatchPath(realPath) || !dir.MatchPath(realPath) {
		t.Fatalf("rules through a junction miss the real image %q", realPath)
	}
	if !file.MatchPath(normalizeWinImage(filepath.Join(link, "Game", "game.exe"))) {
		t.Fatal("raw form lost")
	}
	if dir.MatchPath(normalizeWinImage(filepath.Join(base, "Library", "Game2", "game.exe"))) {
		t.Fatal("resolved dir form lost its separator boundary")
	}
	missing := compileIn(t, env, filepath.Join(base, "Missing", "x.exe"))
	if !missing.MatchPath(normalizeWinImage(filepath.Join(base, "Missing", "x.exe"))) {
		t.Fatal("rule for a missing file must keep its raw form")
	}
}

func BenchmarkClassifyKnownOwner(b *testing.B) {
	c, err := newWinClassifier(hostWinEnv(), 0, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer pc.Close()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		b.Fatal(err)
	}
	defer ln.Close()
	conn, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	flows := make([]FlowID, 0, 64)
	for i := 0; i < 32; i++ {
		flows = append(flows,
			FlowID{Proto: protoTCP, App: addrPortOf(conn.LocalAddr()), Remote: addrPortOf(conn.RemoteAddr())},
			FlowID{Proto: protoUDP, App: addrPortOf(pc.LocalAddr())})
	}
	rules, _ := compileRules(winEnv(), []string{`C:\Games\`}, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Classify(rules, flows)
	}
}

func TestLiveSharedUDPPortIsAmbiguous(t *testing.T) {
	lc := net.ListenConfig{Control: reuseAddrControl}
	mine, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Close()
	port := addrPortOf(mine.LocalAddr()).Port()
	h := startHelperTree(t, ".exe", fmt.Sprintf("%s=%d", helperShareEnv, port))
	shared := netip.MustParseAddrPort(h.game.UDPShared)
	if shared.Port() != port {
		t.Fatalf("helper bound %v, want port %d", shared, port)
	}
	env := hostWinEnv()
	c, err := newWinClassifier(env, 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rules := compileIn(t, env, h.gamePath)
	flows := []FlowID{{Proto: protoUDP, App: shared, Remote: netip.MustParseAddrPort("127.0.0.1:9")}, h.flows()[1]}
	res := c.Classify(rules, flows)
	if res[0].Verdict != VerdictTunnel || res[0].Owner.PID != 0 {
		t.Fatalf("port held by two processes: %+v", res[0])
	}
	if res[1].Verdict != VerdictBypass || res[1].Owner.PID != h.game.PID {
		t.Fatalf("helper's exclusive port: %+v", res[1])
	}
	checks := []SocketCheck{
		{Proto: protoUDP, App: shared, Owner: res[1].Owner},
		{Proto: protoUDP, App: flows[1].App, Owner: res[1].Owner},
	}
	if got := c.Validate(checks); !slices.Equal(got, []bool{false, true}) {
		t.Fatalf("Validate = %v", got)
	}
}

func BenchmarkRefresh(b *testing.B) {
	c, err := newWinClassifier(hostWinEnv(), 0, nil, nil)
	if err != nil {
		b.Fatal(err)
	}
	defer c.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := c.refresh(); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	c.mu.Lock()
	b.ReportMetric(float64(len(c.lin.nodes)), "nodes")
	c.mu.Unlock()
	b.ReportMetric(float64(c.Stats().PathFails), "pathfails")
}

// A path read without a handle only counts while the pid still has the start time it was read for.
func TestStillStartedAtTellsAReusedPid(t *testing.T) {
	var buf []byte
	procs, err := systemProcesses(&buf)
	if err != nil {
		t.Fatal(err)
	}
	self, start := os.Getpid(), int64(0)
	for _, p := range procs {
		if p.pid == self {
			start = p.start
		}
	}
	if start == 0 {
		t.Fatal("own process missing from the snapshot")
	}
	if !stillStartedAt(self, start) {
		t.Error("the live process was taken for a reused pid")
	}
	if stillStartedAt(self, start+1) {
		t.Error("a different start time was accepted")
	}
	if stillStartedAt(1<<30, start) {
		t.Error("a pid with no process was accepted")
	}
}
