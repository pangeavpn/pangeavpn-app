//go:build linux

package procmatch

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// fakeProcRoot is a directory laid out like /proc for the files the classifier reads.
type fakeProcRoot struct {
	t    testing.TB
	root string
}

type fakeProc struct {
	pid, ppid int
	start     int64
	exe       string
	uid       uint32
	sockets   []uint32
	pipes     int
	flatpak   string
}

func newFakeProcRoot(t testing.TB) *fakeProcRoot {
	return &fakeProcRoot{t: t, root: t.TempDir()}
}

func (f *fakeProcRoot) dir(pid int) string {
	return filepath.Join(f.root, strconv.Itoa(pid))
}

func (f *fakeProcRoot) must(err error) {
	f.t.Helper()
	if err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeProcRoot) add(p fakeProc) {
	f.t.Helper()
	dir := f.dir(p.pid)
	f.must(os.MkdirAll(filepath.Join(dir, "fd"), 0o755))
	comm := "kworker"
	if p.exe != "" {
		comm = filepath.Base(p.exe)
	}
	f.must(os.WriteFile(filepath.Join(dir, "stat"), []byte(procStatLine(p.pid, comm, p.ppid, p.start)), 0o644))
	status := fmt.Sprintf("Name:\t%s\nTgid:\t%d\nPid:\t%d\nPPid:\t%d\nUid:\t%d\t%d\t%d\t%d\nGid:\t0\t0\t0\t0\n", comm, p.pid, p.pid, p.ppid, p.uid, p.uid, p.uid, p.uid)
	f.must(os.WriteFile(filepath.Join(dir, "status"), []byte(status), 0o644))
	if p.exe != "" {
		f.must(os.Symlink(p.exe, filepath.Join(dir, "exe")))
	}
	fd := 0
	for i := 0; i < p.pipes; i++ {
		f.must(os.Symlink(fmt.Sprintf("pipe:[%d]", 900000+i), filepath.Join(dir, "fd", strconv.Itoa(fd))))
		fd++
	}
	for _, ino := range p.sockets {
		f.must(os.Symlink(fmt.Sprintf("socket:[%d]", ino), filepath.Join(dir, "fd", strconv.Itoa(fd))))
		fd++
	}
	if p.flatpak != "" {
		f.must(os.MkdirAll(filepath.Join(dir, "root"), 0o755))
		info := "[Application]\nname=com.example.App\n\n[Instance]\napp-path=" + p.flatpak + "\n"
		f.must(os.WriteFile(filepath.Join(dir, "root", ".flatpak-info"), []byte(info), 0o644))
	}
}

func (f *fakeProcRoot) remove(pid int) {
	f.t.Helper()
	f.must(os.RemoveAll(f.dir(pid)))
}

func (f *fakeProcRoot) setExe(pid int, exe string) {
	f.t.Helper()
	link := filepath.Join(f.dir(pid), "exe")
	f.must(os.Remove(link))
	f.must(os.Symlink(exe, link))
}

func (f *fakeProcRoot) addSocket(pid, fd int, ino uint32) {
	f.t.Helper()
	f.must(os.Symlink(fmt.Sprintf("socket:[%d]", ino), filepath.Join(f.dir(pid), "fd", strconv.Itoa(fd))))
}

// fakeDiag answers sock_diag lookups from a fixed socket list.
type fakeDiag struct {
	mu    sync.Mutex
	tcp   []diagRow
	udp   []diagRow
	bump  uint64
	err   error
	calls int
}

func (d *fakeDiag) set(proto uint8, rows ...diagRow) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if proto == protoTCP {
		d.tcp = rows
	} else {
		d.udp = rows
	}
}

func (d *fakeDiag) addRows(proto uint8, rows ...diagRow) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if proto == protoTCP {
		d.tcp = append(d.tcp, rows...)
	} else {
		d.udp = append(d.udp, rows...)
	}
}

func (d *fakeDiag) tcpLookup(app, remote netip.AddrPort) ([]diagRow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls++
	if d.err != nil {
		return nil, d.err
	}
	for _, r := range d.tcp {
		if r.local == app && r.remote == remote {
			r.cookie += d.bump
			return []diagRow{r}, nil
		}
	}
	return nil, nil
}

func (d *fakeDiag) dump(proto uint8, sport uint16, states uint32) ([]diagRow, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	rows := d.udp
	if proto == protoTCP {
		rows = d.tcp
	}
	var out []diagRow
	for _, r := range rows {
		if (sport == 0 || r.local.Port() == sport) && states&(1<<r.state) != 0 {
			r.cookie += d.bump
			out = append(out, r)
		}
	}
	return out, nil
}

func (d *fakeDiag) close() {}

func tcpSock(local, remote string, uid, inode uint32, cookie uint64) diagRow {
	return diagRow{family: afInet, state: 2, local: netip.MustParseAddrPort(local), remote: netip.MustParseAddrPort(remote), uid: uid, inode: inode, cookie: cookie}
}

func udpSock(local string, uid, inode uint32, cookie uint64) diagRow {
	r := diagRow{family: afInet, state: 7, local: netip.MustParseAddrPort(local), uid: uid, inode: inode, cookie: cookie}
	if r.local.Addr().Is6() {
		r.family = afInet6
	}
	return r
}

func newFakeLinux(t testing.TB, root string, d sockSource, mut ...func(*linuxConfig)) *linuxClassifier {
	t.Helper()
	cfg := linuxConfig{root: root, diag: d, env: linuxEnv(), logf: t.Logf, maxDirs: maxCandidateDirs}
	for _, m := range mut {
		m(&cfg)
	}
	c, err := newLinuxClassifier(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

const (
	exeLauncher = "/opt/games/launcher/launcher"
	exeGame     = "/opt/games/game/game"
	exeFirefox  = "/usr/lib/firefox/firefox"
	exeDaemon   = "/opt/pangeavpn/pangea-daemon"
)

// stdTree is a desktop session: gnome-shell -> bash -> launcher -> game, firefox beside it,
// and the VPN daemon (root) with a child of its own.
func stdTree(f *fakeProcRoot) {
	for _, p := range []fakeProc{
		{pid: 1, ppid: 0, start: 1, exe: "/sbin/init"},
		{pid: 2, ppid: 0, start: 1},
		{pid: 900, ppid: 1, start: 100, exe: "/usr/lib/systemd/systemd", uid: 1000},
		{pid: 1000, ppid: 900, start: 200, exe: "/usr/bin/gnome-shell", uid: 1000},
		{pid: 1100, ppid: 1000, start: 300, exe: "/usr/bin/bash", uid: 1000, pipes: 3},
		{pid: 1200, ppid: 1100, start: 400, exe: exeLauncher, uid: 1000, sockets: []uint32{5001}, pipes: 2},
		{pid: 1201, ppid: 1200, start: 410, exe: exeGame, uid: 1000, sockets: []uint32{5002, 5003, 5004, 5005}},
		{pid: 1300, ppid: 1000, start: 500, exe: exeFirefox, uid: 1000, sockets: []uint32{6001, 6005, 6006}, pipes: 5},
		{pid: 1400, ppid: 1, start: 50, exe: exeDaemon, sockets: []uint32{7001}},
		{pid: 1401, ppid: 1400, start: 60, exe: exeGame, sockets: []uint32{7002}},
	} {
		f.add(p)
	}
}

var (
	flowGame     = FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.64.0.2:40001"), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
	flowFirefox  = FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.64.0.2:40002"), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
	flowLauncher = FlowID{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:3074"), Remote: netip.MustParseAddrPort("198.51.100.4:3074")}
	flowGameUDP  = FlowID{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:3075"), Remote: netip.MustParseAddrPort("198.51.100.4:3074")}
	flowDaemon   = FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.64.0.2:40003"), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
	flowNone     = FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.64.0.2:40009"), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
)

func stdDiag() *fakeDiag {
	d := &fakeDiag{}
	d.set(protoTCP,
		tcpSock("10.64.0.2:40001", "203.0.113.9:443", 1000, 5002, 0xa002),
		tcpSock("10.64.0.2:40002", "203.0.113.9:443", 1000, 6001, 0xb001),
		tcpSock("10.64.0.2:40003", "203.0.113.9:443", 0, 7002, 0xc002),
	)
	d.set(protoUDP,
		udpSock("0.0.0.0:3074", 1000, 5001, 0xa001),
		udpSock("10.64.0.2:3075", 1000, 5003, 0xa003),
	)
	return d
}

func withNever(paths ...string) func(*linuxConfig) {
	return func(cfg *linuxConfig) { cfg.never = paths }
}

func checkResult(t *testing.T, label string, got Result, v Verdict, pid int, chain []string) {
	t.Helper()
	if got.Verdict != v || got.Owner.PID != pid || !slices.Equal(got.Owner.Chain, chain) {
		t.Fatalf("%s = %+v, want %v pid %d chain %q", label, got, v, pid, chain)
	}
}

func TestLinuxFakeClassify(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	d := stdDiag()
	c := newFakeLinux(t, f.root, d, withNever(exeDaemon))
	rules := compileIn(t, linuxEnv(), exeLauncher, exeGame)
	res := c.Classify(rules, []FlowID{flowGame, flowFirefox, flowLauncher, flowGameUDP, flowDaemon, flowNone})
	checkResult(t, "game", res[0], VerdictBypass, 1201, []string{exeGame, exeLauncher, "/usr/bin/bash"})
	if res[0].Owner.Start != 410 || res[0].Owner.SockID != 0xa002 {
		t.Fatalf("game owner identity = %+v", res[0].Owner)
	}
	checkResult(t, "firefox", res[1], VerdictTunnel, 0, nil)
	if res[1].Owner.SockID != 0xb001 {
		t.Fatalf("firefox socket id = %#x", res[1].Owner.SockID)
	}
	checkResult(t, "launcher udp", res[2], VerdictBypass, 1200, []string{exeLauncher, "/usr/bin/bash"})
	checkResult(t, "game udp", res[3], VerdictBypass, 1201, []string{exeGame, exeLauncher, "/usr/bin/bash"})
	checkResult(t, "never-bypass tree", res[4], VerdictTunnel, 0, nil)
	if r := res[5]; r.Verdict != VerdictTunnel || r.Owner.PID != 0 || r.Owner.SockID != 0 || r.Owner.Chain != nil {
		t.Fatalf("flow without a socket = %+v", r)
	}
	if st := c.Stats(); st.Lookups != 6 || st.LookupFails != 1 {
		t.Fatalf("stats = %+v", st)
	}
	if r := c.Classify(nil, []FlowID{flowGame})[0]; r.Verdict != VerdictTunnel || r.Owner.PID != 0 {
		t.Fatalf("empty rules = %+v", r)
	}

	checks := []SocketCheck{
		{Proto: protoUDP, App: flowGameUDP.App, Owner: res[3].Owner},
		{Proto: protoUDP, App: flowLauncher.App, Owner: res[2].Owner},
		{Proto: protoUDP, App: flowGameUDP.App, Owner: Owner{PID: 1201, Start: 410, SockID: 0xdead}},
		{Proto: protoTCP, App: flowGame.App, Owner: res[0].Owner},
		{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:9"), Owner: res[3].Owner},
	}
	if got := c.Validate(checks); !slices.Equal(got, []bool{true, true, false, true, false}) {
		t.Fatalf("Validate = %v", got)
	}
	d.set(protoUDP, udpSock("0.0.0.0:3074", 1000, 5001, 0xa001), udpSock("10.64.0.2:3075", 1000, 5009, 0xa009))
	if got := c.Validate(checks[:1]); got[0] {
		t.Fatal("a replaced socket still validates")
	}
}

func TestLinuxFakeLineageRetained(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	d := stdDiag()
	c := newFakeLinux(t, f.root, d)
	rules := compileIn(t, linuxEnv(), exeLauncher)
	checkResult(t, "game", c.Classify(rules, []FlowID{flowGame})[0], VerdictBypass, 1201, []string{exeGame, exeLauncher, "/usr/bin/bash"})

	f.remove(1200)
	c.mu.Lock()
	if err := c.refreshLocked(time.Now()); err != nil {
		t.Fatal(err)
	}
	c.mu.Unlock()
	d.bump = 1
	checkResult(t, "game after launcher exit", c.Classify(rules, []FlowID{flowGame})[0], VerdictBypass, 1201, []string{exeGame, exeLauncher, "/usr/bin/bash"})

	late := newFakeLinux(t, f.root, d)
	checkResult(t, "game seen only after its launcher exited", late.Classify(rules, []FlowID{flowGame})[0], VerdictTunnel, 0, nil)
}

func TestLinuxFakePidReuseNeverBypasses(t *testing.T) {
	for _, maxDirs := range []int{maxCandidateDirs, 0} {
		t.Run(fmt.Sprintf("dirfds=%d", maxDirs), func(t *testing.T) {
			f := newFakeProcRoot(t)
			stdTree(f)
			d := stdDiag()
			c := newFakeLinux(t, f.root, d, func(cfg *linuxConfig) { cfg.maxDirs = maxDirs })
			rules := compileIn(t, linuxEnv(), exeLauncher)
			if r := c.Classify(rules, []FlowID{flowGame})[0]; r.Verdict != VerdictBypass {
				t.Fatalf("game before reuse = %+v", r)
			}

			f.remove(1201)
			f.add(fakeProc{pid: 1201, ppid: 1100, start: 900, exe: "/usr/bin/other", uid: 1000, sockets: []uint32{5099}})
			d.set(protoTCP, tcpSock("10.64.0.2:40001", "203.0.113.9:443", 1000, 5099, 0xa099))
			if r := c.Classify(rules, []FlowID{flowGame})[0]; r.Verdict != VerdictTunnel || r.Owner.PID != 0 {
				t.Fatalf("socket of the process that reused the game's pid = %+v", r)
			}

			f.remove(1200)
			f.add(fakeProc{pid: 1200, ppid: 1100, start: 950, exe: "/usr/bin/other", uid: 1000})
			f.add(fakeProc{pid: 1250, ppid: 1200, start: 960, exe: "/usr/bin/tool", uid: 1000, sockets: []uint32{5050}})
			d.addRows(protoTCP, tcpSock("10.64.0.2:40050", "203.0.113.9:443", 1000, 5050, 0xa050))
			flow := FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.64.0.2:40050"), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
			if r := c.Classify(rules, []FlowID{flow})[0]; r.Verdict != VerdictTunnel {
				t.Fatalf("child of the process that reused the launcher's pid = %+v", r)
			}
			c.mu.Lock()
			e := c.procs[1250]
			c.mu.Unlock()
			if e == nil {
				t.Fatal("new child not listed")
			}
			if v := c.lin.view(e.key, c.env.isStop); !slices.Equal(v.chain, []string{"/usr/bin/tool", "/usr/bin/other", "/usr/bin/bash"}) {
				t.Fatalf("child chain = %q", v.chain)
			}
		})
	}
}

func TestLinuxFakeDeletedAndFlatpak(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	appPath := "/var/lib/flatpak/app/com.spotify.Client/x86_64/stable/abc123/files"
	f.add(fakeProc{pid: 1500, ppid: 1100, start: 600, exe: exeGame + " (deleted)", uid: 1000, sockets: []uint32{5500}})
	f.add(fakeProc{pid: 1600, ppid: 1000, start: 700, exe: "/app/bin/spotify", uid: 1000, sockets: []uint32{5600}, flatpak: appPath})
	f.add(fakeProc{pid: 1601, ppid: 1000, start: 710, exe: "/app/bin/spotify", uid: 1000, sockets: []uint32{5601}})
	d := &fakeDiag{}
	d.set(protoTCP,
		tcpSock("10.64.0.2:41500", "203.0.113.9:443", 1000, 5500, 1),
		tcpSock("10.64.0.2:41600", "203.0.113.9:443", 1000, 5600, 2),
		tcpSock("10.64.0.2:41601", "203.0.113.9:443", 1000, 5601, 3),
	)
	flow := func(port uint16) FlowID {
		return FlowID{Proto: protoTCP, App: netip.AddrPortFrom(tunIP, port), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
	}
	c := newFakeLinux(t, f.root, d)
	rules := compileIn(t, linuxEnv(), exeGame, "/var/lib/flatpak/app/com.spotify.Client/")
	res := c.Classify(rules, []FlowID{flow(41500), flow(41600), flow(41601)})
	checkResult(t, "upgraded binary", res[0], VerdictBypass, 1500, []string{exeGame, "/usr/bin/bash"})
	checkResult(t, "flatpak", res[1], VerdictBypass, 1600, []string{appPath + "/bin/spotify"})
	checkResult(t, "/app without flatpak-info", res[2], VerdictTunnel, 0, nil)
}

func TestLinuxFakeExecDetected(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	f.add(fakeProc{pid: 1702, ppid: 1100, start: 790, exe: "/usr/bin/bash", uid: 1000})
	d := stdDiag()
	c := newFakeLinux(t, f.root, d)
	rules := compileIn(t, linuxEnv(), exeGame)
	flow := func(port uint16) FlowID {
		return FlowID{Proto: protoTCP, App: netip.AddrPortFrom(tunIP, port), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
	}
	f.add(fakeProc{pid: 1700, ppid: 1100, start: 800, exe: "/usr/bin/bash", uid: 1000})
	f.add(fakeProc{pid: 1701, ppid: 1100, start: 801, exe: "/usr/bin/bash", uid: 1000})
	c.mu.Lock()
	if err := c.refreshLocked(time.Now()); err != nil {
		t.Fatal(err)
	}
	if len(c.young) != 2 {
		t.Fatalf("young = %d, want only the processes started after the classifier", len(c.young))
	}
	c.procs[1701].seen = time.Now().Add(-2 * youngWindow)
	c.mu.Unlock()

	f.setExe(1700, exeGame)
	f.addSocket(1700, 3, 5700)
	f.setExe(1701, exeGame)
	f.addSocket(1701, 3, 5701)
	d.addRows(protoTCP, tcpSock("10.64.0.2:41700", "203.0.113.9:443", 1000, 5700, 0x1700), tcpSock("10.64.0.2:41701", "203.0.113.9:443", 1000, 5701, 0x1701))
	res := c.Classify(rules, []FlowID{flow(41700), flow(41701)})
	checkResult(t, "young wrapper after exec", res[0], VerdictBypass, 1700, []string{exeGame, "/usr/bin/bash"})
	checkResult(t, "old process after exec, before the sweep", res[1], VerdictTunnel, 0, nil)

	c.mu.Lock()
	c.sweepAll = true
	c.sweepLocked()
	c.mu.Unlock()
	checkResult(t, "old process after the sweep", c.Classify(rules, []FlowID{flow(41701)})[0], VerdictBypass, 1701, []string{exeGame, "/usr/bin/bash"})
}

func TestLinuxFakeScanTouchesOnlyCandidates(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	for i := 0; i < 300; i++ {
		var socks []uint32
		for j := 0; j < 20; j++ {
			socks = append(socks, uint32(100000+i*20+j))
		}
		f.add(fakeProc{pid: 3000 + i, ppid: 1000, start: 1000 + int64(i), exe: fmt.Sprintf("/usr/bin/app%d", i), uid: 1000, sockets: socks})
	}
	d := stdDiag()
	d.addRows(protoTCP, tcpSock("10.64.0.2:42000", "203.0.113.9:443", 1000, 100005, 0x2000), tcpSock("10.64.0.2:42001", "203.0.113.9:443", 0, 100006, 0x2001))
	c := newFakeLinux(t, f.root, d, withNever(exeDaemon))
	rules := compileIn(t, linuxEnv(), exeLauncher)
	flow := func(port uint16) FlowID {
		return FlowID{Proto: protoTCP, App: netip.AddrPortFrom(tunIP, port), Remote: netip.MustParseAddrPort("203.0.113.9:443")}
	}
	c.Classify(rules, nil)
	reads := func() uint64 {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.fs.fdReads
	}

	before, scans := reads(), c.scans.Load()
	if r := c.Classify(rules, []FlowID{flow(42000)})[0]; r.Verdict != VerdictTunnel || r.Owner.SockID != 0x2000 {
		t.Fatalf("other app's flow = %+v", r)
	}
	if got := reads() - before; got != 7 || c.scans.Load() != scans+1 {
		t.Fatalf("miss read %d descriptors in %d scans, want the candidates' 7 in one", got, c.scans.Load()-scans)
	}

	before = reads()
	c.Classify(rules, []FlowID{flow(42001)})
	if got := reads() - before; got != 0 {
		t.Fatalf("socket of a uid no candidate runs as read %d descriptors", got)
	}

	before, scans = reads(), c.scans.Load()
	c.Classify(rules, []FlowID{flow(42000), flow(42001)})
	if reads() != before || c.scans.Load() != scans {
		t.Fatal("cached sockets were scanned again")
	}
}

func TestLinuxFakeUDPAmbiguity(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	d := &fakeDiag{}
	d.set(protoUDP,
		udpSock("10.64.0.2:3074", 1000, 5003, 1),
		udpSock("0.0.0.0:3074", 1000, 6005, 2),
		udpSock("10.64.0.2:3076", 1000, 5004, 3),
		udpSock("[::]:3076", 1000, 5005, 4),
		udpSock("[::]:3076", 1000, 6006, 5),
	)
	d.udp[4].v6only = true
	c := newFakeLinux(t, f.root, d)
	rules := compileIn(t, linuxEnv(), exeGame)
	shared := FlowID{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:3074")}
	own := FlowID{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:3076")}
	res := c.Classify(rules, []FlowID{shared, own})
	checkResult(t, "port shared with another app", res[0], VerdictTunnel, 0, nil)
	checkResult(t, "two sockets of one process", res[1], VerdictBypass, 1201, []string{exeGame, exeLauncher, "/usr/bin/bash"})
	if res[1].Owner.SockID != 3 {
		t.Fatalf("socket id = %d, want the exact-address row's cookie", res[1].Owner.SockID)
	}
	check := []SocketCheck{{Proto: protoUDP, App: own.App, Owner: res[1].Owner}}
	if got := c.Validate(check); !got[0] {
		t.Fatal("reuseport group of the same process fails validation")
	}
	d.udp[3] = udpSock("[::]:3076", 1000, 6007, 6)
	if got := c.Validate(check); got[0] {
		t.Fatal("port joined by another socket still validates")
	}
}

func TestLinuxFakeScanInterval(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	d := stdDiag()
	d.addRows(protoTCP, tcpSock("10.64.0.2:42002", "203.0.113.9:443", 1000, 6005, 0x3001))
	c := newFakeLinux(t, f.root, d, func(cfg *linuxConfig) { cfg.minScan = 80 * time.Millisecond })
	rules := compileIn(t, linuxEnv(), exeLauncher)
	other := FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("10.64.0.2:42002"), Remote: netip.MustParseAddrPort("203.0.113.9:443")}

	c.Classify(rules, []FlowID{flowFirefox})
	scans := c.scans.Load()
	start := time.Now()
	if r := c.Classify(rules, []FlowID{flowGame})[0]; r.Verdict != VerdictBypass {
		t.Fatalf("game = %+v", r)
	}
	if c.scans.Load() != scans || time.Since(start) > 40*time.Millisecond {
		t.Fatalf("socket a recent scan found in a candidate waited %v and scanned %d times", time.Since(start), c.scans.Load()-scans)
	}

	start = time.Now()
	if r := c.Classify(rules, []FlowID{other})[0]; r.Verdict != VerdictTunnel {
		t.Fatalf("other = %+v", r)
	}
	if c.scans.Load() != scans+1 || time.Since(start) < 30*time.Millisecond {
		t.Fatalf("second miss took %v and %d scans, want one scan after the interval", time.Since(start), c.scans.Load()-scans)
	}
}

func TestLinuxFakeRulesResolveLate(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	gate := make(chan struct{})
	env := newRuleEnv("linux", envSettings{resolve: func(raw string, kind RuleKind) []string {
		<-gate
		if raw == "/usr/bin/firefox" {
			return []string{exeFirefox}
		}
		return nil
	}})
	d := stdDiag()
	c := newFakeLinux(t, f.root, d, func(cfg *linuxConfig) { cfg.env = env })
	rules, errs := compileRules(env, []string{"/usr/bin/firefox"}, nil)
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	if r := c.Classify(rules, []FlowID{flowFirefox})[0]; r.Verdict != VerdictTunnel {
		t.Fatalf("before the symlink resolved = %+v", r)
	}
	close(gate)
	rules.waitResolved()
	checkResult(t, "after the symlink resolved", c.Classify(rules, []FlowID{flowFirefox})[0], VerdictBypass, 1300, []string{exeFirefox})
}

func TestLinuxFakeDescriptorCapAndRelease(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	d := stdDiag()
	c := newFakeLinux(t, f.root, d, func(cfg *linuxConfig) { cfg.maxDirs = 1 })
	rules := compileIn(t, linuxEnv(), "/opt/games/")
	res := c.Classify(rules, []FlowID{flowGame, flowLauncher, flowDaemon})
	for i, r := range res {
		if r.Verdict != VerdictBypass {
			t.Fatalf("flow %d = %+v", i, r)
		}
	}
	c.mu.Lock()
	dirfds, cands := c.dirfds, len(c.cands)
	c.mu.Unlock()
	if dirfds != 1 || cands != 3 || c.Stats().Fallbacks == 0 {
		t.Fatalf("dirfds %d candidates %d fallbacks %d", dirfds, cands, c.Stats().Fallbacks)
	}
	c.mu.Lock()
	c.useRulesLocked(nil)
	dirfds, cands = c.dirfds, len(c.cands)
	c.mu.Unlock()
	if dirfds != 0 || cands != 0 {
		t.Fatalf("after rules emptied: dirfds %d candidates %d", dirfds, cands)
	}
	c.Close()
	if r := c.Classify(rules, []FlowID{flowGame})[0]; r.Verdict != VerdictTunnel {
		t.Fatalf("after Close = %+v", r)
	}
}

func TestLinuxFakeLookupErrorsTunnelQuietly(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	d := stdDiag()
	var mu sync.Mutex
	var lines []string
	c := newFakeLinux(t, f.root, d, func(cfg *linuxConfig) {
		cfg.logf = func(format string, args ...any) {
			mu.Lock()
			lines = append(lines, fmt.Sprintf(format, args...))
			mu.Unlock()
		}
	})
	rules := compileIn(t, linuxEnv(), exeLauncher)
	d.err = errors.New("netlink timeout")
	flows := []FlowID{flowGame, flowFirefox, flowDaemon, flowLauncher}
	for i := 0; i < 20; i++ {
		for j, r := range c.Classify(rules, flows) {
			if r.Verdict != VerdictTunnel || r.Owner.PID != 0 {
				t.Fatalf("flow %d with a failing lookup = %+v", j, r)
			}
		}
	}
	if d.calls != 20 {
		t.Fatalf("%d tcp lookups for 20 batches, want one per batch after the first failure", d.calls)
	}
	d.err = nil
	if r := c.Classify(rules, []FlowID{flowGame})[0]; r.Verdict != VerdictBypass {
		t.Fatalf("after recovery = %+v", r)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(lines) != 1 {
		t.Fatalf("logged %d lines: %q", len(lines), lines)
	}
	for _, leak := range []string{"10.64.0.2", "203.0.113", "40001", "3074", "/opt/", "/usr/", "1201"} {
		if strings.Contains(lines[0], leak) {
			t.Fatalf("log line %q leaks %q", lines[0], leak)
		}
	}
}

func TestLinuxFakeSelfTree(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	d := stdDiag()
	c := newFakeLinux(t, f.root, d, func(cfg *linuxConfig) { cfg.selfPID = 1400 })
	rules := compileIn(t, linuxEnv(), exeGame)
	res := c.Classify(rules, []FlowID{flowDaemon, flowGame})
	checkResult(t, "classifier's own child", res[0], VerdictTunnel, 0, nil)
	if res[1].Verdict != VerdictBypass {
		t.Fatalf("unrelated game = %+v", res[1])
	}
}

func TestLinuxFakeRefreshFollowsRules(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	c := newFakeLinux(t, f.root, stdDiag())
	c.start()
	c.ObserveRules(compileIn(t, linuxEnv(), exeLauncher))
	deadline := time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		n := len(c.cands)
		c.mu.Unlock()
		if n == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("candidates = %d after rules were observed", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.add(fakeProc{pid: 1210, ppid: 1200, start: 420, exe: "/opt/games/launcher/updater", uid: 1000})
	time.Sleep(1300 * time.Millisecond)
	c.mu.Lock()
	e := c.procs[1210]
	c.mu.Unlock()
	if e == nil || !e.cand {
		t.Fatalf("launcher child started after rules = %+v", e)
	}
	c.ObserveRules(nil)
	deadline = time.Now().Add(2 * time.Second)
	for {
		c.mu.Lock()
		n := c.dirfds
		c.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("dirfds = %d after rules were emptied", n)
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	c.Close()
	if time.Since(start) > time.Second {
		t.Fatalf("Close took %v", time.Since(start))
	}
}

func TestLinuxFakeStaleClassifyKeepsRefreshOff(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	c := newFakeLinux(t, f.root, stdDiag())
	c.start()
	c.ObserveRules(nil)
	// An engine still holding the previous rules classifies after they were emptied.
	c.Classify(compileIn(t, linuxEnv(), exeLauncher), []FlowID{flowLauncher})
	before := c.Stats().Refreshes
	time.Sleep(1300 * time.Millisecond)
	if n := c.Stats().Refreshes - before; n != 0 {
		t.Fatalf("refreshed %d times after the rules were emptied", n)
	}
	c.mu.Lock()
	n := c.dirfds
	c.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d candidate descriptors held for the stale rules", n)
	}
}

// gatedDiag holds the dump of one UDP port until released, so another caller can scan meanwhile.
type gatedDiag struct {
	*fakeDiag
	port    atomic.Uint32
	entered chan struct{}
	release chan struct{}
}

func (g *gatedDiag) dump(proto uint8, sport uint16, states uint32) ([]diagRow, error) {
	if sport != 0 && g.port.CompareAndSwap(uint32(sport), 0) {
		close(g.entered)
		<-g.release
	}
	return g.fakeDiag.dump(proto, sport, states)
}

// A scan another caller ran over candidates listed before this flow was read must not cache the
// flow's socket as held by no candidate.
func TestLinuxFakeSharedScanNeedsFreshCandidates(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	g := &gatedDiag{fakeDiag: stdDiag(), entered: make(chan struct{}), release: make(chan struct{})}
	c := newFakeLinux(t, f.root, g, func(cfg *linuxConfig) { cfg.minScan = 300 * time.Millisecond })
	rules := compileIn(t, linuxEnv(), exeLauncher)
	c.ObserveRules(rules)
	tick := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.useRulesLocked(c.lastRules.Load())
		if err := c.refreshLocked(time.Now()); err != nil {
			t.Error(err)
		}
		c.sweepLocked()
	}
	c.Classify(rules, []FlowID{flowLauncher})

	other := make(chan struct{})
	go func() {
		defer close(other)
		c.Classify(rules, []FlowID{flowFirefox})
	}()
	time.Sleep(30 * time.Millisecond)
	tick()
	time.Sleep(5 * time.Millisecond)
	f.add(fakeProc{pid: 1250, ppid: 1200, start: 450, exe: exeGame, uid: 1000, sockets: []uint32{5050}})
	g.addRows(protoUDP, udpSock("10.64.0.2:3090", 1000, 5050, 0xa050))
	flow := FlowID{Proto: protoUDP, App: netip.MustParseAddrPort("10.64.0.2:3090"), Remote: netip.MustParseAddrPort("198.51.100.4:3074")}
	g.port.Store(3090)
	var first Result
	done := make(chan struct{})
	go func() {
		defer close(done)
		first = c.Classify(rules, []FlowID{flow})[0]
	}()
	<-g.entered
	<-other
	close(g.release)
	<-done
	chain := []string{exeGame, exeLauncher, "/usr/bin/bash"}
	checkResult(t, "new child of the excluded launcher", first, VerdictBypass, 1250, chain)
	tick()
	flow.Remote = netip.MustParseAddrPort("198.51.100.7:3074")
	checkResult(t, "same socket after a refresh", c.Classify(rules, []FlowID{flow})[0], VerdictBypass, 1250, chain)
}

// within fails the test when fn still runs after d; release must unblock it.
func within(t *testing.T, d time.Duration, what string, release func(), fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
		return
	case <-time.After(d):
	}
	release()
	select {
	case <-done:
	case <-time.After(d):
	}
	t.Fatalf("%s still blocked after %v", what, d)
}

// fifoAt puts a FIFO at path; the returned func plays the writer a reader blocked in open waits for.
func fifoAt(t *testing.T, path string) func() {
	t.Helper()
	os.Remove(path)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	return func() {
		if fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0); err == nil {
			unix.Close(fd)
		}
	}
}

// A process controls its own /.flatpak-info: a FIFO there must neither block nor be opened.
func TestLinuxFakeFlatpakInfoNotRegular(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	f.add(fakeProc{pid: 1600, ppid: 1000, start: 700, exe: "/app/bin/spotify", uid: 1000})
	info := filepath.Join(f.dir(1600), "root", ".flatpak-info")
	release := fifoAt(t, info)
	var c *linuxClassifier
	var err error
	within(t, 3*time.Second, "process snapshot over a FIFO", release, func() {
		c, err = newLinuxClassifier(linuxConfig{root: f.root, diag: stdDiag(), env: linuxEnv(), logf: t.Logf, maxDirs: maxCandidateDirs})
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	if e := c.procs[1600]; e == nil || e.path != "/app/bin/spotify" {
		t.Fatalf("sandboxed process = %+v", e)
	}

	opened := make(chan struct{})
	go func() {
		if fd, err := unix.Open(info, unix.O_WRONLY|unix.O_CLOEXEC, 0); err == nil {
			unix.Close(fd)
		}
		close(opened)
	}()
	t.Cleanup(func() {
		if fd, err := unix.Open(info, unix.O_RDONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0); err == nil {
			<-opened
			unix.Close(fd)
		}
	})
	time.Sleep(50 * time.Millisecond)
	within(t, 3*time.Second, "image lookup with a writer waiting", release, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.fs.exePath(-1, 1600)
	})
	select {
	case <-opened:
		t.Fatal("the classifier opened a FIFO the process controls")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestReadRegularRefusesSpecialFiles(t *testing.T) {
	dir := t.TempDir()
	want := "[Instance]\napp-path=/x\n"
	for _, err := range []error{
		os.WriteFile(filepath.Join(dir, "plain"), []byte(want), 0o644),
		unix.Mkfifo(filepath.Join(dir, "fifo"), 0o600),
		os.Symlink(filepath.Join(dir, "plain"), filepath.Join(dir, "link")),
		os.Mkdir(filepath.Join(dir, "sub"), 0o755),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	p, err := openProcFS(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if b, err := p.readRegular(p.rootfd, "plain", 64<<10); err != nil || string(b) != want {
		t.Fatalf("regular file = %q, %v", b, err)
	}
	for _, name := range []string{"fifo", "link", "sub", "missing"} {
		if b, err := p.readRegular(p.rootfd, name, 64<<10); err == nil {
			t.Errorf("%s read as %q", name, b)
		}
	}
	for _, path := range []string{"/proc/version", "/sys/kernel/uevent_seqnum", "/dev/null"} {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		d, err := unix.Open(filepath.Dir(path), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		if err != nil {
			t.Fatal(err)
		}
		b, err := p.readRegular(d, filepath.Base(path), 64<<10)
		unix.Close(d)
		if err == nil {
			t.Errorf("%s read as %q", path, b)
		}
	}
}

// The exec check re-reads /.flatpak-info only when the exe link changes.
func TestLinuxFakeExecCheckKeepsSandboxInfo(t *testing.T) {
	f := newFakeProcRoot(t)
	stdTree(f)
	appPath := "/var/lib/flatpak/app/com.spotify.Client/x86_64/stable/abc123/files"
	f.add(fakeProc{pid: 1600, ppid: 1000, start: 700, exe: "/app/bin/spotify", uid: 1000, flatpak: appPath})
	c := newFakeLinux(t, f.root, &fakeDiag{})
	info := filepath.Join(f.dir(1600), "root", ".flatpak-info")
	release := fifoAt(t, info)
	sweep := func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.sweepAll = true
		c.sweepLocked()
	}
	path := func() string {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.procs[1600].path
	}
	within(t, 3*time.Second, "sweep of an unchanged sandboxed process", release, sweep)
	if p := path(); p != appPath+"/bin/spotify" {
		t.Fatalf("path after the sweep = %q", p)
	}

	other := "/var/lib/flatpak/app/org.example.Other/x86_64/stable/def456/files"
	f.must(os.Remove(info))
	f.must(os.WriteFile(info, []byte("[Instance]\napp-path="+other+"\n"), 0o644))
	f.setExe(1600, "/app/bin/other")
	within(t, 3*time.Second, "sweep after an exec", func() {}, sweep)
	if p := path(); p != other+"/bin/other" {
		t.Fatalf("path after an exec = %q", p)
	}
}

// benchTree builds a /proc of n processes: an excluded launcher tree of cands processes with
// fds sockets each, the rest unrelated with a few pipes, plus 64 unrelated TCP sockets.
func benchTree(tb testing.TB, n, cands, fds int) (*fakeProcRoot, *fakeDiag, []FlowID) {
	f := newFakeProcRoot(tb)
	f.add(fakeProc{pid: 1, start: 1, exe: "/sbin/init"})
	f.add(fakeProc{pid: 10, ppid: 1, start: 2, exe: "/usr/bin/bash", uid: 1000})
	ino := uint32(10000)
	for i := 0; i < cands; i++ {
		p := fakeProc{pid: 100 + i, ppid: 100, start: int64(3 + i), exe: exeGame, uid: 1000}
		if i == 0 {
			p.ppid, p.exe = 10, exeLauncher
		}
		for j := 0; j < fds; j++ {
			p.sockets = append(p.sockets, ino)
			ino++
		}
		f.add(p)
	}
	d := &fakeDiag{}
	var flows []FlowID
	for i := 0; len(flows) < 64 || i < n-cands-2; i++ {
		p := fakeProc{pid: 1000 + i, ppid: 10, start: int64(1000 + i), exe: "/usr/bin/app" + strconv.Itoa(i%50), uid: 1000, pipes: 3}
		if len(flows) < 64 {
			p.sockets = []uint32{uint32(500000 + i)}
			app := netip.AddrPortFrom(tunIP, uint16(30000+i))
			d.addRows(protoTCP, tcpSock(app.String(), "203.0.113.9:443", 1000, uint32(500000+i), uint64(i+1)))
			flows = append(flows, FlowID{Proto: protoTCP, App: app, Remote: netip.MustParseAddrPort("203.0.113.9:443")})
		}
		if i < n-cands-2 {
			f.add(p)
		}
	}
	return f, d, flows
}

func benchClassifier(tb testing.TB, f *fakeProcRoot, d *fakeDiag, flows []FlowID, cands int) (*linuxClassifier, *Rules) {
	tb.Helper()
	c := newFakeLinux(tb, f.root, d, func(cfg *linuxConfig) { cfg.logf = nil })
	rules, _ := compileRules(linuxEnv(), []string{exeLauncher}, nil)
	rules.waitResolved()
	c.Classify(rules, flows)
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.cands) != cands {
		tb.Fatalf("%d candidates, want %d", len(c.cands), cands)
	}
	return c, rules
}

func BenchmarkLinuxClassify(b *testing.B) {
	for _, tc := range []struct {
		name          string
		n, cands, fds int
		miss          bool
	}{
		{"cached/2000", 2000, 30, 8, false},
		{"miss/2000", 2000, 30, 8, true},
		{"miss/8000", 8000, 30, 8, true},
		{"miss/800-fat-tree", 800, 30, 170, true},
	} {
		f, d, flows := benchTree(b, tc.n, tc.cands, tc.fds)
		b.Run(tc.name, func(b *testing.B) {
			c, rules := benchClassifier(b, f, d, flows, tc.cands)
			c.mu.Lock()
			reads := c.fs.fdReads
			c.mu.Unlock()
			scans := c.scans.Load()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if tc.miss {
					d.bump = uint64(i+1) << 32
				}
				c.Classify(rules, flows)
			}
			b.StopTimer()
			d.bump = 0
			c.mu.Lock()
			b.ReportMetric(float64(c.fs.fdReads-reads)/float64(b.N), "fdreads/op")
			c.mu.Unlock()
			b.ReportMetric(float64(c.scans.Load()-scans)/float64(b.N), "scans/op")
		})
	}
}

func BenchmarkLinuxRefresh(b *testing.B) {
	f, d, _ := benchTree(b, 2000, 30, 8)
	c := newFakeLinux(b, f.root, d, func(cfg *linuxConfig) { cfg.logf = nil })
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.mu.Lock()
		if err := c.refreshLocked(time.Now()); err != nil {
			b.Fatal(err)
		}
		c.mu.Unlock()
	}
}

// TestLinuxFakeMissBudget checks the PM-6 target loosely: a 64-flow batch of new sockets from
// unrelated apps over 2000 processes stays well inside the classifier's 50 ms budget.
func TestLinuxFakeMissBudget(t *testing.T) {
	f, d, flows := benchTree(t, 2000, 30, 8)
	c, rules := benchClassifier(t, f, d, flows, 30)
	var times []time.Duration
	for i := 0; i < 31; i++ {
		d.bump = uint64(i+1) << 32
		start := time.Now()
		res := c.Classify(rules, flows)
		times = append(times, time.Since(start))
		for _, r := range res {
			if r.Verdict != VerdictTunnel || r.Owner.SockID == 0 {
				t.Fatalf("unrelated flow = %+v", r)
			}
		}
	}
	slices.Sort(times)
	t.Logf("64-flow miss batch over 2000 processes: median %v, max %v", times[len(times)/2], times[len(times)-1])
	if times[len(times)/2] > 10*time.Millisecond {
		t.Fatalf("median miss batch %v", times[len(times)/2])
	}
}

func (f *fakeProcRoot) setMountNS(pid int, ns string) {
	f.t.Helper()
	dir := filepath.Join(f.root, "self")
	if pid > 0 {
		dir = f.dir(pid)
	}
	f.must(os.MkdirAll(filepath.Join(dir, "ns"), 0o755))
	f.must(os.Symlink(ns, filepath.Join(dir, "ns", "mnt")))
}

// Another mount namespace can show any binary at an excluded app's path (unshare plus a bind
// mount, or a container), so there the exe path only counts when it is that very file.
func TestLinuxForeignMountNamespaceNeedsTheSameImage(t *testing.T) {
	f := newFakeProcRoot(t)
	host := t.TempDir()
	firefox, evil := filepath.Join(host, "firefox"), filepath.Join(host, "evil")
	f.must(os.WriteFile(firefox, []byte("real"), 0o755))
	f.must(os.WriteFile(evil, []byte("evil"), 0o755))
	f.setMountNS(0, "mnt:[4026531840]")
	f.add(fakeProc{pid: 100, ppid: 1, start: 10, exe: firefox})
	f.setMountNS(100, "mnt:[4026531840]")
	f.add(fakeProc{pid: 200, ppid: 1, start: 20, exe: firefox})
	f.setMountNS(200, "mnt:[4026532999]")
	f.add(fakeProc{pid: 300, ppid: 1, start: 30, exe: filepath.Join(host, "gone")})
	f.setMountNS(300, "mnt:[4026532999]")
	f.add(fakeProc{pid: 400, ppid: 1, start: 40, exe: filepath.Join(host, "gone")})
	f.setMountNS(400, "mnt:[4026531840]")
	f.add(fakeProc{pid: 500, ppid: 1, start: 50, exe: firefox})
	f.add(fakeProc{pid: 501, ppid: 1, start: 51, exe: filepath.Join(host, "gone")})

	p, err := openProcFS(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if p.mntNS != "mnt:[4026531840]" {
		t.Fatalf("own mount namespace = %q", p.mntNS)
	}
	for _, tc := range []struct {
		pid  int
		want string
		why  string
	}{
		{100, firefox, "same namespace"},
		{200, firefox, "foreign namespace, the very file"},
		{300, "", "foreign namespace, a path that is not this file"},
		{400, filepath.Join(host, "gone"), "same namespace keeps the link as before"},
		{500, firefox, "an unreadable namespace counts as foreign, and this is the very file"},
		{501, "", "an unreadable namespace counts as foreign, and this path is not the file"},
	} {
		if got, err := p.exePath(-1, tc.pid); err != nil || got != tc.want {
			t.Errorf("pid %d (%s): exePath = %q, %v; want %q", tc.pid, tc.why, got, err, tc.want)
		}
	}
	if !p.sameImage(-1, 200, firefox) || p.sameImage(-1, 200, evil) || p.sameImage(-1, 200, "") {
		t.Error("sameImage does not tell the image's own file from another one")
	}
}

// A sandbox's /.flatpak-info on FUSE or a network mount could block the read under the
// classifier lock, so it is not read at all and the /app path matches nothing.
func TestLinuxFlatpakInfoOnABlockingMountIsNotRead(t *testing.T) {
	f := newFakeProcRoot(t)
	appPath := "/var/lib/flatpak/app/com.example.App/x86_64/stable/abc"
	f.add(fakeProc{pid: 600, ppid: 1, start: 60, exe: "/app/bin/app", flatpak: appPath})
	f.add(fakeProc{pid: 700, ppid: 1, start: 70, exe: "/app/bin/app", flatpak: appPath})
	local := "22 1 8:1 / / rw - ext4 /dev/sda1 rw\n42 22 0:53 / /.flatpak-info ro - tmpfs tmpfs ro\n"
	fuse := "22 1 8:1 / / rw - ext4 /dev/sda1 rw\n43 22 0:54 / /.flatpak-info ro - fuse.hang hang ro\n"
	f.must(os.WriteFile(filepath.Join(f.dir(600), "mountinfo"), []byte(local), 0o644))
	f.must(os.WriteFile(filepath.Join(f.dir(700), "mountinfo"), []byte(fuse), 0o644))

	p, err := openProcFS(f.root)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	if got, _ := p.exePath(-1, 600); got != appPath+"/bin/app" {
		t.Errorf("local /.flatpak-info: exePath = %q, want the host deployment", got)
	}
	if got, _ := p.exePath(-1, 700); got != "/app/bin/app" {
		t.Errorf("FUSE /.flatpak-info: exePath = %q, want the untranslated /app path", got)
	}
}
