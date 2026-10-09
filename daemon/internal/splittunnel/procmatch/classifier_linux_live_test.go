//go:build linux

package procmatch

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func init() {
	extraHelpers["exec"] = runExecHelper
}

// runExecHelper reports itself, then execs the game helper in place, as wrapper scripts do.
func runExecHelper(dir string) error {
	if err := writeHelperInfo(dir, "wrapper", helperInfo{PID: os.Getpid()}); err != nil {
		return err
	}
	waitHelperSignal(dir, "wrapper")
	game := os.Getenv(helperGameEnv)
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, helperEnv+"=") {
			env = append(env, kv)
		}
	}
	return syscall.Exec(game, []string{game, "-test.run=^$"}, append(env, helperEnv+"=game"))
}

func hostLinuxEnv() *ruleEnv {
	return newRuleEnv("linux", hostSettings())
}

func newLiveClassifier(t *testing.T) *linuxClassifier {
	t.Helper()
	d := newNetlinkDiag()
	c, err := newLinuxClassifier(linuxConfig{root: "/proc", diag: d, env: hostLinuxEnv(), logf: t.Logf, minScan: minScanInterval, maxDirs: maxCandidateDirs})
	if err != nil {
		d.close()
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func sockInode(t *testing.T, c syscall.Conn) uint64 {
	t.Helper()
	rc, err := c.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var st unix.Stat_t
	var serr error
	if err := rc.Control(func(fd uintptr) { serr = unix.Fstat(int(fd), &st) }); err != nil || serr != nil {
		t.Fatal(err, serr)
	}
	return st.Ino
}

func TestLinuxLiveNetlinkDiag(t *testing.T) {
	d := newNetlinkDiag()
	defer d.close()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	conn, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	local, remote := addrPortOf(conn.LocalAddr()), addrPortOf(conn.RemoteAddr())
	rows, err := d.tcpLookup(local, remote)
	if err != nil || len(rows) != 1 {
		t.Fatalf("tcp lookup = %+v, %v", rows, err)
	}
	r := rows[0]
	if r.local != local || r.remote != remote || r.state != 1 || r.uid != uint32(os.Getuid()) || r.cookie == 0 || uint64(r.inode) != sockInode(t, conn.(*net.TCPConn)) {
		t.Fatalf("tcp row = %+v, want inode %d", r, sockInode(t, conn.(*net.TCPConn)))
	}
	if again, _ := d.tcpLookup(local, remote); len(again) != 1 || again[0].cookie != r.cookie {
		t.Fatalf("cookie not stable: %+v", again)
	}
	if rows, err := d.tcpLookup(netip.MustParseAddrPort("127.0.0.1:1"), remote); err != nil || len(rows) != 0 {
		t.Fatalf("missing socket = %+v, %v", rows, err)
	}
	if rows, err := d.tcpLookup(addrPortOf(ln.Addr()), netip.MustParseAddrPort("127.0.0.1:9")); err != nil || len(diagRowsFor(rows, protoTCP, addrPortOf(ln.Addr()), netip.MustParseAddrPort("127.0.0.1:9"))) != 0 {
		t.Fatalf("listener fallback = %+v, %v", rows, err)
	}

	pc4, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer pc4.Close()
	dual, err := net.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer dual.Close()
	v6only, err := net.ListenPacket("udp6", "[::]:0")
	if err != nil {
		t.Fatal(err)
	}
	defer v6only.Close()
	for _, tc := range []struct {
		pc       net.PacketConn
		family   uint8
		wildcard bool
		v6only   bool
	}{{pc4, afInet, false, false}, {dual, afInet6, true, false}, {v6only, afInet6, true, true}} {
		port := addrPortOf(tc.pc.LocalAddr()).Port()
		rows, err := d.dump(protoUDP, port, diagAllStates)
		if err != nil || len(rows) != 1 {
			t.Fatalf("udp dump for %v = %+v, %v", tc.pc.LocalAddr(), rows, err)
		}
		r := rows[0]
		if r.family != tc.family || r.local.Port() != port || r.local.Addr().IsUnspecified() != tc.wildcard || r.v6only != tc.v6only || uint64(r.inode) != sockInode(t, tc.pc.(*net.UDPConn)) {
			t.Fatalf("udp row for %v = %+v", tc.pc.LocalAddr(), r)
		}
	}
	all, err := d.dump(protoUDP, 0, diagAllStates)
	if err != nil || len(all) < 3 {
		t.Fatalf("full udp dump = %d rows, %v", len(all), err)
	}
	tcpAll, err := d.dump(protoTCP, addrPortOf(conn.LocalAddr()).Port(), diagLiveTCPStates)
	if err != nil || len(tcpAll) != 1 || tcpAll[0].cookie != r.cookie {
		t.Fatalf("tcp port dump = %+v, %v", tcpAll, err)
	}
}

func TestLinuxLiveSelfTest(t *testing.T) {
	c, err := NewClassifier(Options{SelfPID: os.Getpid(), Logf: t.Logf})
	if err != nil {
		t.Fatalf("NewClassifier: %v", err)
	}
	defer c.Close()
	s := openOwnSockets(t)
	exe, _ := os.Executable()
	res := c.Classify(compileIn(t, hostLinuxEnv(), exe), []FlowID{s.tcp, s.udp})
	for i, r := range res {
		if r.Verdict != VerdictTunnel || r.Owner.PID != 0 || r.Owner.SockID == 0 {
			t.Fatalf("flow %d: %+v; the classifier's own sockets must never bypass", i, r)
		}
	}
}

func TestLinuxLiveSelfTestRejectsWrongSelf(t *testing.T) {
	c, err := NewClassifier(Options{SelfPID: os.Getppid()})
	if err == nil {
		c.Close()
		t.Fatal("self-test passed for another process")
	}
	if !strings.Contains(err.Error(), "self-test") {
		t.Fatalf("error = %v", err)
	}
}

// dualStackTCP connects an AF_INET6 socket to a v4-mapped address, as dual-stack apps do.
func dualStackTCP(t *testing.T, to netip.AddrPort) FlowID {
	t.Helper()
	fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { unix.Close(fd) })
	sa := &unix.SockaddrInet6{Port: int(to.Port()), Addr: netip.AddrFrom16(to.Addr().As16()).As16()}
	if err := unix.Connect(fd, sa); err != nil {
		t.Fatal(err)
	}
	local, err := unix.Getsockname(fd)
	if err != nil {
		t.Fatal(err)
	}
	l6 := local.(*unix.SockaddrInet6)
	return FlowID{Proto: protoTCP, App: netip.AddrPortFrom(netip.AddrFrom16(l6.Addr).Unmap(), uint16(l6.Port)), Remote: to}
}

func TestLinuxLiveOwnSockets(t *testing.T) {
	c := newLiveClassifier(t)
	s := openOwnSockets(t)
	dual, err := net.ListenPacket("udp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer dual.Close()
	dualUDP := FlowID{Proto: protoUDP, App: netip.AddrPortFrom(netip.MustParseAddr("127.0.0.1"), addrPortOf(dual.LocalAddr()).Port()), Remote: netip.MustParseAddrPort("127.0.0.1:9")}
	dualTCP := dualStackTCP(t, s.tcp.Remote)
	exe, _ := os.Executable()
	env := hostLinuxEnv()
	match := compileIn(t, env, exe)
	flows := []FlowID{s.tcp, s.udp, dualUDP, dualTCP}
	res := c.Classify(match, flows)
	for i, r := range res {
		if r.Verdict != VerdictBypass || r.Owner.PID != os.Getpid() || len(r.Owner.Chain) == 0 || r.Owner.Chain[0] != exe || r.Owner.SockID == 0 {
			t.Fatalf("flow %d with own image rule: %+v", i, r)
		}
	}
	checks := []SocketCheck{
		{Proto: protoUDP, App: s.udp.App, Owner: res[1].Owner},
		{Proto: protoUDP, App: dualUDP.App, Owner: res[2].Owner},
		{Proto: protoTCP, App: s.tcp.App, Owner: res[0].Owner},
		{Proto: protoUDP, App: s.udp.App, Owner: Owner{PID: res[1].Owner.PID, Start: res[1].Owner.Start, SockID: res[1].Owner.SockID + 1}},
	}
	if got := c.Validate(checks); !slices.Equal(got, []bool{true, true, true, false}) {
		t.Fatalf("Validate = %v", got)
	}
	other := compileIn(t, env, "/nowhere/other")
	for i, r := range c.Classify(other, flows) {
		if r.Verdict != VerdictTunnel || r.Owner.PID != 0 || r.Owner.SockID == 0 {
			t.Fatalf("flow %d with another rule: %+v", i, r)
		}
	}
	if st := c.Stats(); st.Lookups != 8 || st.LookupFails != 0 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestLinuxLiveChildAncestry(t *testing.T) {
	h := startHelperTree(t, "")
	env := hostLinuxEnv()
	c := newLiveClassifier(t)
	launcherRule := compileIn(t, env, h.launcherPath)
	gameRule := compileIn(t, env, h.gamePath)
	dirRule := compileIn(t, env, filepath.Dir(h.gamePath)+"/")
	otherRule := compileIn(t, env, "/nowhere/other")

	expect := func(label string, cl Classifier, rules *Rules, want Verdict, wantChain []string) []Result {
		t.Helper()
		res := cl.Classify(rules, h.flows())
		for i, r := range res {
			if want == VerdictTunnel {
				if r.Verdict != VerdictTunnel || r.Owner.PID != 0 {
					t.Fatalf("%s flow %d: %+v, want tunnel", label, i, r)
				}
				continue
			}
			if r.Verdict != want || r.Owner.PID != h.game.PID || len(r.Owner.Chain) < len(wantChain) || !slices.Equal(r.Owner.Chain[:len(wantChain)], wantChain) {
				t.Fatalf("%s flow %d: %+v, want %v with chain prefix %q", label, i, r, want, wantChain)
			}
		}
		return res
	}
	res := expect("launcher rule", c, launcherRule, VerdictBypass, []string{h.gamePath, h.launcherPath})
	expect("game rule", c, gameRule, VerdictBypass, []string{h.gamePath, h.launcherPath})
	expect("dir rule", c, dirRule, VerdictBypass, []string{h.gamePath, h.launcherPath})
	expect("other rule", c, otherRule, VerdictTunnel, nil)

	checks := []SocketCheck{{Proto: protoTCP, App: h.flows()[0].App, Owner: res[0].Owner}, {Proto: protoUDP, App: h.flows()[1].App, Owner: res[1].Owner}}
	if got := c.Validate(checks); !slices.Equal(got, []bool{true, true}) {
		t.Fatalf("Validate = %v", got)
	}

	h.stopLauncher(t)
	c.mu.Lock()
	if err := c.refreshLocked(time.Now()); err != nil {
		t.Fatal(err)
	}
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
	expect("launcher rule after launcher exit", c, compileIn(t, env, h.launcherPath, "/nowhere/x"), VerdictBypass, []string{h.gamePath, h.launcherPath})

	late := newLiveClassifier(t)
	expect("seen only after its launcher exited", late, launcherRule, VerdictTunnel, nil)
}

func TestLinuxLiveExecWrapper(t *testing.T) {
	c := newLiveClassifier(t)
	dir := t.TempDir()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, conn)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, conn := range conns {
			conn.Close()
		}
		mu.Unlock()
	})
	wrapper := copyTestBinary(t, dir, "wrapper")
	game := copyTestBinary(t, dir, "game")
	cmd := exec.Command(wrapper, "-test.run=^$")
	cmd.Env = append(os.Environ(), helperEnv+"=exec", helperDirEnv+"="+dir, helperTCPEnv+"="+ln.Addr().String(), helperGameEnv+"="+game)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		os.WriteFile(filepath.Join(dir, "wrapper.exit"), nil, 0o600)
		os.WriteFile(filepath.Join(dir, "game.exit"), nil, 0o600)
		cmd.Process.Kill()
		cmd.Wait()
	})
	w := readHelperInfo(t, dir, "wrapper")
	c.mu.Lock()
	if err := c.refreshLocked(time.Now()); err != nil {
		t.Fatal(err)
	}
	e := c.procs[w.PID]
	c.mu.Unlock()
	if e == nil || e.path != wrapper {
		t.Fatalf("wrapper entry = %+v", e)
	}
	signalHelper(t, dir, "wrapper")
	g := readHelperInfo(t, dir, "game")
	if g.PID != w.PID {
		t.Fatalf("game pid %d, wrapper pid %d", g.PID, w.PID)
	}
	flows := []FlowID{
		{Proto: protoTCP, App: netip.MustParseAddrPort(g.TCPLocal), Remote: netip.MustParseAddrPort(g.TCPRemote)},
		{Proto: protoUDP, App: netip.MustParseAddrPort(g.UDPLocal), Remote: netip.MustParseAddrPort("127.0.0.1:9")},
	}
	for i, r := range c.Classify(compileIn(t, hostLinuxEnv(), game), flows) {
		if r.Verdict != VerdictBypass || r.Owner.PID != w.PID || len(r.Owner.Chain) == 0 || r.Owner.Chain[0] != game {
			t.Fatalf("flow %d after the wrapper exec'd the game: %+v", i, r)
		}
	}
}

func TestLinuxLiveSharedUDPPortIsAmbiguous(t *testing.T) {
	lc := net.ListenConfig{Control: reuseAddrControl}
	mine, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer mine.Close()
	port := addrPortOf(mine.LocalAddr()).Port()
	h := startHelperTree(t, "", fmt.Sprintf("%s=%d", helperShareEnv, port))
	shared := netip.MustParseAddrPort(h.game.UDPShared)
	if shared.Port() != port {
		t.Fatalf("helper bound %v, want port %d", shared, port)
	}
	c := newLiveClassifier(t)
	rules := compileIn(t, hostLinuxEnv(), h.gamePath)
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

func TestLinuxLiveDeletedImage(t *testing.T) {
	h := startHelperTree(t, "")
	if err := os.Remove(h.gamePath); err != nil {
		t.Fatal(err)
	}
	if exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", h.game.PID)); err != nil || !strings.HasSuffix(exe, deletedSuffix) {
		t.Fatalf("exe link = %q, %v", exe, err)
	}
	c := newLiveClassifier(t)
	for i, r := range c.Classify(compileIn(t, hostLinuxEnv(), h.gamePath), h.flows()) {
		if r.Verdict != VerdictBypass || r.Owner.PID != h.game.PID || r.Owner.Chain[0] != h.gamePath {
			t.Fatalf("flow %d of a replaced binary: %+v", i, r)
		}
	}
}

// An image on a mount whose server stopped answering (hung NFS, stalled FUSE) must not stall
// process tracking: the exec check reads only the exe link, never the image.
func TestLinuxLiveExeOnStalledFilesystem(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root for a private mount namespace")
	}
	fuse, err := unix.Open("/dev/fuse", unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("no FUSE: %v", err)
	}
	var once sync.Once
	abort := func() { once.Do(func() { unix.Close(fuse) }) }
	defer abort()
	f := newFakeProcRoot(t)
	stdTree(f)
	mnt := t.TempDir()
	f.add(fakeProc{pid: 1600, ppid: 1000, start: 700, exe: mnt + "/game", uid: 1000})
	var skip, fail error
	within(t, 3*time.Second, "process tracking with an image on a stalled mount", abort, func() {
		// Never unlocked: the thread, its mount namespace and the mount end with this goroutine.
		runtime.LockOSThread()
		if skip = unix.Unshare(unix.CLONE_NEWNS); skip != nil {
			return
		}
		if skip = unix.Mount("", "/", "", unix.MS_REC|unix.MS_PRIVATE, ""); skip != nil {
			return
		}
		// Nothing serves the connection, so every request on the mount waits until it is aborted.
		opts := fmt.Sprintf("fd=%d,rootmode=40000,user_id=0,group_id=0", fuse)
		if skip = unix.Mount("stalled", mnt, "fuse", unix.MS_NOSUID|unix.MS_NODEV, opts); skip != nil {
			return
		}
		c, err := newLinuxClassifier(linuxConfig{root: f.root, diag: stdDiag(), env: linuxEnv(), maxDirs: maxCandidateDirs})
		if err != nil {
			fail = err
			return
		}
		defer c.Close()
		c.mu.Lock()
		defer c.mu.Unlock()
		if e := c.procs[1600]; e == nil || e.path != mnt+"/game" {
			fail = fmt.Errorf("process on the stalled mount = %+v", e)
			return
		}
		c.sweepAll = true
		c.sweepLocked()
		link := filepath.Join(f.dir(1600), "exe")
		if fail = os.Remove(link); fail == nil {
			fail = os.Symlink(mnt+"/other", link)
		}
		c.sweepAll = true
		c.sweepLocked()
		if e := c.procs[1600]; fail == nil && (e == nil || e.path != mnt+"/other") {
			fail = fmt.Errorf("process after an exec onto the stalled mount = %+v", e)
		}
	})
	if skip != nil {
		t.Skipf("private FUSE mount unavailable: %v", skip)
	}
	if fail != nil {
		t.Fatal(fail)
	}
}
