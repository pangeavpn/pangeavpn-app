//go:build windows

package procmatch

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/sing/common/winiphlpapi"
	"golang.org/x/sys/windows"
)

const refreshInterval = time.Second

type winClassifier struct {
	logf    func(string, ...any)
	env     *ruleEnv
	self    procKey
	never   *Rules
	limiter logLimiter
	devices dosDevices

	mu          sync.Mutex
	lin         *lineage
	spi         map[int]spiProc
	spiAt       int64
	fallbackBuf []byte

	refreshMu  sync.Mutex
	refreshBuf []byte

	rulesOn   atomic.Bool
	lastRules atomic.Pointer[Rules]
	kick      chan struct{}

	lookups, lookupFails, fallbacks, pathFails, refreshes atomic.Uint64

	started   atomic.Bool
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

var (
	_ StatsSource   = (*winClassifier)(nil)
	_ RulesObserver = (*winClassifier)(nil)
)

func NewClassifier(opts Options) (Classifier, error) {
	if err := winiphlpapi.LoadExtendedTable(); err != nil {
		return nil, fmt.Errorf("socket owner tables unavailable: %w", err)
	}
	if opts.SelfPID == 0 {
		opts.SelfPID = os.Getpid()
	}
	never := append([]string(nil), opts.NeverBypass...)
	if exe, err := os.Executable(); err == nil {
		never = append(never, exe)
	}
	c, err := newWinClassifier(hostEnv(), opts.SelfPID, never, opts.Logf)
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

func newWinClassifier(env *ruleEnv, selfPID int, never []string, logf func(string, ...any)) (*winClassifier, error) {
	c := &winClassifier{
		logf:  logf,
		env:   env,
		never: compileImages(env, never),
		lin:   newLineage(),
		kick:  make(chan struct{}, 1),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	if selfPID > 0 {
		start, err := processStart(selfPID)
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

func (c *winClassifier) start() {
	c.started.Store(true)
	go c.run()
}

func (c *winClassifier) run() {
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

func (c *winClassifier) Close() error {
	c.closeOnce.Do(func() {
		close(c.stop)
		if c.started.Load() {
			<-c.done
		}
	})
	return nil
}

func (c *winClassifier) ObserveRules(rules *Rules) {
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

func (c *winClassifier) Stats() Stats {
	return Stats{
		Lookups:     c.lookups.Load(),
		LookupFails: c.lookupFails.Load(),
		Fallbacks:   c.fallbacks.Load(),
		PathFails:   c.pathFails.Load(),
		Refreshes:   c.refreshes.Load(),
	}
}

func (c *winClassifier) Classify(rules *Rules, flows []FlowID) []Result {
	out := make([]Result, len(flows))
	if rules.Empty() || len(flows) == 0 {
		return out
	}
	pids, at := c.owners(flows)
	never := []*Rules{c.never, rules.never}
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make(map[int]procKey)
	for i := range flows {
		c.lookups.Add(1)
		key, ok := c.ownerKeyLocked(pids[i], at, keys)
		if !ok {
			c.lookupFails.Add(1)
			continue
		}
		v, chain := verdictFor(rules, never, c.self, c.lin.view(key, c.env.isStop))
		out[i] = Result{Verdict: v, Owner: Owner{PID: key.pid, Start: key.start, Chain: chain}}
	}
	return out
}

func (c *winClassifier) Validate(checks []SocketCheck) []bool {
	out := make([]bool, len(checks))
	if len(checks) == 0 {
		return out
	}
	flows := make([]FlowID, len(checks))
	for i, ch := range checks {
		flows[i] = FlowID{Proto: ch.Proto, App: ch.App}
	}
	pids, at := c.owners(flows)
	c.mu.Lock()
	defer c.mu.Unlock()
	starts := make(map[int]int64)
	for i, ch := range checks {
		pid := pids[i]
		if pid <= 0 || pid != ch.Owner.PID {
			continue
		}
		start, ok := starts[pid]
		if !ok {
			start, ok = c.startLocked(pid, at)
			if !ok {
				start = -1
			}
			starts[pid] = start
		}
		out[i] = start >= 0 && start == ch.Owner.Start
	}
	return out
}

// owners reads each needed socket table once; pid -1 means no single owner.
func (c *winClassifier) owners(flows []FlowID) ([]int, int64) {
	var needTCP, needUDP bool
	for _, f := range flows {
		needTCP = needTCP || f.Proto == protoTCP
		needUDP = needUDP || f.Proto == protoUDP
	}
	at := filetimeNow()
	var tcp []tcpRow
	var udp []udpRow
	var tcpErr, udpErr error
	if needTCP {
		if tcp, tcpErr = tcpTable(); tcpErr != nil {
			c.limiter.logf(c.logf, "tcptable", "split tunnel: tcp owner table failed: %v", tcpErr)
		}
	}
	if needUDP {
		if udp, udpErr = udpTable(); udpErr != nil {
			c.limiter.logf(c.logf, "udptable", "split tunnel: udp owner table failed: %v", udpErr)
		}
	}
	pids := make([]int, len(flows))
	for i, f := range flows {
		pids[i] = -1
		if (f.Proto == protoTCP && tcpErr != nil) || (f.Proto == protoUDP && udpErr != nil) {
			continue
		}
		if pid, ok := flowOwner(tcp, udp, f); ok {
			pids[i] = pid
		}
	}
	return pids, at
}

func tcpTable() ([]tcpRow, error) {
	rows, err := winiphlpapi.GetExtendedTcpTable()
	if err != nil {
		return nil, err
	}
	out := make([]tcpRow, len(rows))
	for i, r := range rows {
		out[i] = tcpRowFrom(r)
	}
	return out, nil
}

func udpTable() ([]udpRow, error) {
	rows, err := winiphlpapi.GetExtendedUdpTable()
	if err != nil {
		return nil, err
	}
	out := make([]udpRow, len(rows))
	for i, r := range rows {
		out[i] = udpRowFrom(r)
	}
	return out, nil
}

func tcpRowFrom(r winiphlpapi.MibTcpRowOwnerPid) tcpRow {
	return tcpRow{
		state:  r.DwState,
		local:  netip.AddrPortFrom(winiphlpapi.DwordToAddr(r.DwLocalAddr), winiphlpapi.DwordToPort(r.DwLocalPort)),
		remote: netip.AddrPortFrom(winiphlpapi.DwordToAddr(r.DwRemoteAddr), winiphlpapi.DwordToPort(r.DwRemotePort)),
		pid:    int(r.DwOwningPid),
	}
}

func udpRowFrom(r winiphlpapi.MibUdpRowOwnerPid) udpRow {
	return udpRow{
		local: netip.AddrPortFrom(winiphlpapi.DwordToAddr(r.DwLocalAddr), winiphlpapi.DwordToPort(r.DwLocalPort)),
		pid:   int(r.DwOwningPid),
	}
}

func (c *winClassifier) ownerKeyLocked(pid int, at int64, cache map[int]procKey) (procKey, bool) {
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

// observeLocked puts pid's current process, started no later than notAfter, into the lineage
// together with every live ancestor it can verify.
func (c *winClassifier) observeLocked(pid int, notAfter int64, depth int) (procKey, bool) {
	if pid <= 0 || pid == 4 {
		return procKey{}, false
	}
	start, ok := c.startLocked(pid, notAfter)
	if !ok || start > notAfter {
		return procKey{}, false
	}
	key := procKey{pid, start}
	if c.lin.known(key) {
		c.lin.add(procInfo{pid: pid, start: start}, nil)
		return key, true
	}
	info, ok := c.infoLocked(pid, start)
	if !ok {
		return procKey{}, false
	}
	var parent *procKey
	if depth < maxChainDepth && info.ppid > 0 && info.ppid != pid {
		if pk, ok := c.observeLocked(info.ppid, start, depth+1); ok {
			parent = &pk
		}
	}
	c.lin.add(procInfo{pid: pid, ppid: info.ppid, start: start, path: info.path}, parent)
	return key, true
}

// startLocked returns the creation time of the process holding pid now; the fallback
// snapshot must postdate notBefore so it cannot describe an earlier holder of the pid.
func (c *winClassifier) startLocked(pid int, notBefore int64) (int64, bool) {
	if start, err := processStart(pid); err == nil {
		return start, true
	}
	c.fallbacks.Add(1)
	sp, ok := c.spiLocked(pid, notBefore)
	return sp.start, ok
}

func (c *winClassifier) infoLocked(pid int, start int64) (winProcess, bool) {
	info, err := processInfo(pid)
	if err == nil {
		return info, info.start == start
	}
	if info.start != 0 && info.start != start {
		return winProcess{}, false
	}
	c.fallbacks.Add(1)
	if info.start == 0 {
		sp, ok := c.spiLocked(pid, start)
		if !ok || sp.start != start {
			return winProcess{}, false
		}
		info = winProcess{start: start, ppid: sp.ppid}
	}
	if p, ok := c.ntPath(pid); ok {
		info.path = p
	} else {
		c.pathFails.Add(1)
	}
	return info, true
}

func (c *winClassifier) spiLocked(pid int, notBefore int64) (spiProc, bool) {
	if c.spi == nil || c.spiAt < notBefore {
		at := filetimeNow()
		procs, err := systemProcesses(&c.fallbackBuf)
		if err != nil {
			c.limiter.logf(c.logf, "spi", "split tunnel: process snapshot failed: %v", err)
			return spiProc{}, false
		}
		c.setSPILocked(procs, at)
	}
	sp, ok := c.spi[pid]
	return sp, ok
}

func (c *winClassifier) setSPILocked(procs []spiProc, at int64) {
	if at < c.spiAt {
		return
	}
	m := make(map[int]spiProc, len(procs))
	for _, p := range procs {
		m[p.pid] = p
	}
	c.spi, c.spiAt = m, at
}

func (c *winClassifier) ntPath(pid int) (string, bool) {
	nt, err := ntImagePath(pid)
	if err != nil || nt == "" {
		return "", false
	}
	dos, ok := c.devices.toDos(nt)
	if !ok {
		return "", false
	}
	return normalizeProcessPath(dos), true
}

// refresh syncs the lineage with a full process snapshot; image paths are read only for
// processes the lineage has not seen, outside the classifier lock.
func (c *winClassifier) refresh() error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.mu.Lock()
	since := c.lin.mark()
	c.mu.Unlock()
	at := filetimeNow()
	procs, err := systemProcesses(&c.refreshBuf)
	if err != nil {
		return err
	}
	infos := make([]procInfo, 0, len(procs))
	var unknown []int
	c.mu.Lock()
	c.setSPILocked(procs, at)
	for _, p := range procs {
		if p.pid == 0 || p.pid == 4 {
			continue
		}
		if !c.lin.has(procKey{p.pid, p.start}) {
			unknown = append(unknown, len(infos))
		}
		infos = append(infos, procInfo{pid: p.pid, ppid: p.ppid, start: p.start})
	}
	c.mu.Unlock()
	for _, i := range unknown {
		infos[i].path = c.pathFor(infos[i].pid, infos[i].start)
	}
	c.mu.Lock()
	c.lin.sync(infos, since)
	c.mu.Unlock()
	c.refreshes.Add(1)
	return nil
}

func (c *winClassifier) pathFor(pid int, start int64) string {
	if h, err := openProcess(pid); err == nil {
		defer windows.CloseHandle(h)
		s, err := handleStart(h)
		if err == nil && s != start {
			return ""
		}
		if err == nil {
			if image, err := handleImage(h); err == nil {
				return normalizeProcessPath(image)
			}
		}
	}
	c.fallbacks.Add(1)
	if p, ok := c.ntPath(pid); ok {
		return p
	}
	c.pathFails.Add(1)
	return ""
}

// selfTest resolves the classifier's own loopback TCP and UDP sockets to its process and image.
func (c *winClassifier) selfTest() error {
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
	images := ownImages()
	pids, at := c.owners(flows)
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, proto := range []string{"tcp", "udp"} {
		if pids[i] != c.self.pid {
			return fmt.Errorf("self-test: %s socket owner not found", proto)
		}
		key, ok := c.observeLocked(pids[i], at, 0)
		if !ok || key != c.self {
			return fmt.Errorf("self-test: %s socket owner identity not resolved", proto)
		}
		if n := c.lin.nodes[key]; n == nil || (len(images) > 0 && !containsString(images, n.path)) {
			return fmt.Errorf("self-test: %s socket owner image not resolved", proto)
		}
	}
	return nil
}

func ownImages() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	out := []string{normalizeProcessPath(exe)}
	for _, f := range resolveWindows(exe, RuleFile) {
		out = append(out, normalizeWinImage(f))
	}
	return out
}
