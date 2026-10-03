//go:build darwin

package procmatch

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	refreshInterval = time.Second

	procInfoCallPIDInfo = 2
	procPIDPathInfo     = 11
	procPIDPathMaxSize  = 4 * 1024
)

type darwinClassifier struct {
	logf    func(string, ...any)
	env     *ruleEnv
	self    procKey
	never   *Rules
	limiter logLimiter

	mu  sync.Mutex
	lin *lineage

	refreshMu sync.Mutex

	rulesOn   atomic.Bool
	lastRules atomic.Pointer[Rules]
	kick      chan struct{}

	lookups, lookupFails, pathFails, refreshes atomic.Uint64

	started   atomic.Bool
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

var (
	_ StatsSource   = (*darwinClassifier)(nil)
	_ RulesObserver = (*darwinClassifier)(nil)
)

func NewClassifier(opts Options) (Classifier, error) {
	if opts.SelfPID == 0 {
		opts.SelfPID = os.Getpid()
	}
	never := append([]string(nil), opts.NeverBypass...)
	if exe, err := os.Executable(); err == nil {
		never = append(never, exe)
	}
	c, err := newDarwinClassifier(hostEnv(), opts.SelfPID, never, opts.Logf)
	if err != nil {
		return nil, err
	}
	if err := c.selfTest(); err != nil {
		c.Close()
		return nil, err
	}
	c.start()
	return c, nil
}

func newDarwinClassifier(env *ruleEnv, selfPID int, never []string, logf func(string, ...any)) (*darwinClassifier, error) {
	c := &darwinClassifier{
		logf:  logf,
		env:   env,
		never: compileImages(env, never),
		lin:   newLineage(),
		kick:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	if selfPID > 0 {
		_, start, err := kinfo(selfPID)
		if err != nil {
			return nil, fmt.Errorf("self process: %w", err)
		}
		c.self = procKey{selfPID, start}
	}
	c.rulesOn.Store(true)
	if err := c.refresh(); err != nil {
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	return c, nil
}

func (c *darwinClassifier) start() {
	c.started.Store(true)
	go c.run()
}

func (c *darwinClassifier) run() {
	defer close(c.done)
	t := time.NewTicker(refreshInterval)
	defer t.Stop()
	for {
		select {
		case <-c.stop:
			return
		case <-t.C:
		case <-c.kick:
		}
		if !c.rulesOn.Load() {
			continue
		}
		if err := c.refresh(); err != nil {
			c.limiter.logf(c.logf, "refresh", "split tunnel: process snapshot failed: %v", err)
		}
	}
}

func (c *darwinClassifier) Close() error {
	c.closeOnce.Do(func() {
		close(c.stop)
		if c.started.Load() {
			<-c.done
		}
	})
	return nil
}

func (c *darwinClassifier) ObserveRules(rules *Rules) {
	on := !rules.Empty()
	prev := c.lastRules.Swap(rules)
	c.rulesOn.Store(on)
	if on && prev != rules {
		select {
		case c.kick <- struct{}{}:
		default:
		}
	}
}

func (c *darwinClassifier) Stats() Stats {
	return Stats{
		Lookups:     c.lookups.Load(),
		LookupFails: c.lookupFails.Load(),
		PathFails:   c.pathFails.Load(),
		Refreshes:   c.refreshes.Load(),
	}
}

func (c *darwinClassifier) Classify(rules *Rules, flows []FlowID) []Result {
	out := make([]Result, len(flows))
	if rules.Empty() || len(flows) == 0 {
		return out
	}
	snap, at := c.snapshot(flows)
	never := []*Rules{c.never, rules.never}
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make(map[int]procKey)
	for i, f := range flows {
		c.lookups.Add(1)
		o, ok := snap.owner(f)
		if !ok {
			c.lookupFails.Add(1)
			continue
		}
		var ks []procKey
		var views []lineageView
		for _, pid := range o.pids() {
			k, ok := c.ownerKeyLocked(pid, at, keys)
			if !ok {
				ks = nil
				break
			}
			ks = append(ks, k)
			views = append(views, c.lin.view(k, c.env.isStop))
		}
		if len(ks) == 0 {
			c.lookupFails.Add(1)
			continue
		}
		idx, v, chain := verdictForCandidates(rules, never, c.self, views)
		out[i] = Result{Verdict: v, Owner: Owner{PID: ks[idx].pid, Start: ks[idx].start, Chain: chain}}
	}
	return out
}

func (c *darwinClassifier) Validate(checks []SocketCheck) []bool {
	out := make([]bool, len(checks))
	if len(checks) == 0 {
		return out
	}
	flows := make([]FlowID, len(checks))
	for i, ch := range checks {
		flows[i] = FlowID{Proto: ch.Proto, App: ch.App}
	}
	snap, _ := c.snapshot(flows)
	starts := make(map[int]int64)
	for i, ch := range checks {
		o, ok := snap.owner(flows[i])
		if !ok || !o.has(ch.Owner.PID) {
			continue
		}
		start, ok := starts[ch.Owner.PID]
		if !ok {
			start = -1
			if _, s, err := kinfo(ch.Owner.PID); err == nil {
				start = s
			}
			starts[ch.Owner.PID] = start
		}
		out[i] = start >= 0 && start == ch.Owner.Start
	}
	return out
}

// snapshot reads each needed pcblist once; a failed table leaves its flows without owners.
func (c *darwinClassifier) snapshot(flows []FlowID) (*pcbSnapshot, int64) {
	var needTCP, needUDP bool
	for _, f := range flows {
		needTCP = needTCP || f.Proto == protoTCP
		needUDP = needUDP || f.Proto == protoUDP
	}
	at := time.Now().UnixMicro()
	snap := &pcbSnapshot{}
	for _, t := range []struct {
		need  bool
		proto uint8
		name  string
	}{{needTCP, protoTCP, "net.inet.tcp.pcblist_n"}, {needUDP, protoUDP, "net.inet.udp.pcblist_n"}} {
		if !t.need {
			continue
		}
		buf, err := unix.SysctlRaw(t.name)
		if err != nil {
			c.limiter.logf(c.logf, t.name, "split tunnel: socket table failed: %v", err)
			continue
		}
		snap.add(t.proto, parsePCBList(buf, t.proto == protoTCP))
	}
	return snap, at
}

func (c *darwinClassifier) ownerKeyLocked(pid int, at int64, cache map[int]procKey) (procKey, bool) {
	if k, ok := cache[pid]; ok {
		return k, k.pid != 0
	}
	k, ok := c.observeLocked(pid, at, 0)
	if !ok {
		k = procKey{}
	}
	cache[pid] = k
	return k, ok
}

// observeLocked records pid's current process, started no later than notAfter, and its live
// ancestors; the image is re-read on every lookup because exec keeps pid and start time.
func (c *darwinClassifier) observeLocked(pid int, notAfter int64, depth int) (procKey, bool) {
	if pid <= 0 {
		return procKey{}, false
	}
	ppid, start, err := kinfo(pid)
	if err != nil || start > notAfter {
		return procKey{}, false
	}
	key := procKey{pid, start}
	path, perr := pidPath(pid)
	if _, again, err := kinfo(pid); err != nil || again != start {
		return procKey{}, false
	}
	if perr != nil {
		c.pathFails.Add(1)
		path = ""
	}
	if c.lin.known(key) {
		c.lin.setPath(key, path)
		c.lin.add(procInfo{pid: pid, start: start}, nil)
		return key, true
	}
	var parent *procKey
	if depth < maxChainDepth && ppid > 0 && ppid != pid {
		if pk, ok := c.observeLocked(ppid, start, depth+1); ok {
			parent = &pk
		}
	}
	c.lin.add(procInfo{pid: pid, ppid: ppid, start: start, path: path}, parent)
	return key, true
}

// refresh syncs the lineage with the full process list; images are read only for processes
// the lineage has not seen, outside the classifier lock.
func (c *darwinClassifier) refresh() error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.mu.Lock()
	since := c.lin.mark()
	c.mu.Unlock()
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return err
	}
	infos := make([]procInfo, 0, len(kps))
	var unknown []int
	c.mu.Lock()
	for i := range kps {
		pid := int(kps[i].Proc.P_pid)
		if pid <= 0 {
			continue
		}
		start := timevalMicros(kps[i].Proc.P_starttime)
		if !c.lin.has(procKey{pid, start}) {
			unknown = append(unknown, len(infos))
		}
		infos = append(infos, procInfo{pid: pid, ppid: int(kps[i].Eproc.Ppid), start: start})
	}
	c.mu.Unlock()
	for _, i := range unknown {
		p, err := pidPath(infos[i].pid)
		if err != nil {
			continue
		}
		if _, start, err := kinfo(infos[i].pid); err == nil && start == infos[i].start {
			infos[i].path = p
		}
	}
	c.mu.Lock()
	c.lin.sync(infos, since)
	c.mu.Unlock()
	c.refreshes.Add(1)
	return nil
}

func kinfo(pid int) (ppid int, start int64, err error) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return 0, 0, err
	}
	if int(kp.Proc.P_pid) != pid {
		return 0, 0, unix.ESRCH
	}
	return int(kp.Eproc.Ppid), timevalMicros(kp.Proc.P_starttime), nil
}

func timevalMicros(tv unix.Timeval) int64 {
	return tv.Sec*1e6 + int64(tv.Usec)
}

// pidPath is proc_pidpath: proc_info(PROC_PIDPATHINFO) reports the kernel vnode path.
func pidPath(pid int) (string, error) {
	buf := make([]byte, procPIDPathMaxSize)
	_, _, errno := unix.Syscall6(unix.SYS_PROC_INFO, procInfoCallPIDInfo, uintptr(pid), procPIDPathInfo, 0,
		uintptr(unsafe.Pointer(&buf[0])), procPIDPathMaxSize)
	if errno != 0 {
		return "", errno
	}
	p := unix.ByteSliceToString(buf)
	if p == "" {
		return "", unix.ESRCH
	}
	return p, nil
}

// selfTest resolves the classifier's own loopback TCP and UDP sockets to its process and image.
func (c *darwinClassifier) selfTest() error {
	pc, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("self-test: %w", err)
	}
	defer pc.Close()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("self-test: %w", err)
	}
	defer ln.Close()
	conn, err := net.DialTimeout("tcp4", ln.Addr().String(), 2*time.Second)
	if err != nil {
		return fmt.Errorf("self-test: %w", err)
	}
	defer conn.Close()
	flows := []FlowID{
		{Proto: protoTCP, App: addrPortOf(conn.LocalAddr()), Remote: addrPortOf(conn.RemoteAddr())},
		{Proto: protoUDP, App: addrPortOf(pc.LocalAddr()), Remote: netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 0, 0, 1}), 9)},
	}
	snap, at := c.snapshot(flows)
	images := ownImagesDarwin()
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, proto := range []string{"tcp", "udp"} {
		o, ok := snap.owner(flows[i])
		if !ok || !o.has(c.self.pid) {
			return fmt.Errorf("self-test: %s socket owner not found", proto)
		}
		key, ok := c.observeLocked(c.self.pid, at, 0)
		if !ok || key != c.self {
			return fmt.Errorf("self-test: %s socket owner identity not resolved", proto)
		}
		if n := c.lin.nodes[key]; n == nil || (len(images) > 0 && !containsString(images, n.path)) {
			return fmt.Errorf("self-test: %s socket owner image not resolved", proto)
		}
	}
	return nil
}

func ownImagesDarwin() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	out := []string{exe}
	if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
		out = append(out, real)
	}
	if v, ok := vnodePath(exe); ok && !containsString(out, v) {
		out = append(out, v)
	}
	return out
}
