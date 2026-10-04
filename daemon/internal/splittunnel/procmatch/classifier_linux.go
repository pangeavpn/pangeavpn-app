//go:build linux

package procmatch

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
)

const (
	refreshInterval  = time.Second
	sweepPeriod      = 5 * time.Second
	youngWindow      = 5 * time.Second
	minScanInterval  = 10 * time.Millisecond
	scanReuseWindow  = 2 * time.Second
	bypassCookieTTL  = 30 * time.Second
	tunnelCookieTTL  = 2 * time.Minute
	maxCookies       = 16384
	maxCandidateDirs = 256
	fewPortsDump     = 2
)

// procEntry is one live process in the incremental /proc table.
type procEntry struct {
	key   procKey
	ppid  int
	path  string
	link  string
	seen  time.Time
	gen   uint64
	added uint64
	cand  bool
	fsuid uint32
	dirfd int
}

// cookieEntry caches a socket's owner by sock_diag cookie; a zero owner means no candidate holds it.
type cookieEntry struct {
	owner   procKey
	expires time.Time
}

type linuxConfig struct {
	root    string
	diag    sockSource
	env     *ruleEnv
	selfPID int
	never   []string
	logf    func(string, ...any)
	minScan time.Duration
	maxDirs int
}

// linuxClassifier keeps the processes whose lineage matches the rules (candidates) and finds a
// socket's owner by scanning only their descriptors.
type linuxClassifier struct {
	logf    func(string, ...any)
	env     *ruleEnv
	never   *Rules
	diag    sockSource
	minScan time.Duration
	maxDirs int
	limiter logLimiter

	mu         sync.Mutex
	fs         *procFS
	lin        *lineage
	self       procKey
	procs      map[int]*procEntry
	cands      map[int]*procEntry
	young      []*procEntry
	pids       []int
	gen        uint64
	dirfds     int
	rules      *Rules
	rulesReady bool
	cookies    map[uint64]cookieEntry
	scanned    map[uint32]procKey
	scanUIDs   map[uint32]bool
	scanAt     time.Time
	scanFrom   time.Time
	refreshAt  time.Time
	sweepPos   int
	sweepAll   bool
	closed     bool

	rulesOn   atomic.Bool
	lastRules atomic.Pointer[Rules]
	kick      chan struct{}

	lookups, lookupFails, fallbacks, pathFails, refreshes, scans atomic.Uint64

	started   atomic.Bool
	stop      chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

var (
	_ StatsSource   = (*linuxClassifier)(nil)
	_ RulesObserver = (*linuxClassifier)(nil)
)

func NewClassifier(opts Options) (Classifier, error) {
	if opts.SelfPID == 0 {
		opts.SelfPID = os.Getpid()
	}
	never := append([]string(nil), opts.NeverBypass...)
	if exe, err := os.Executable(); err == nil {
		never = append(never, exe)
	}
	diag := newNetlinkDiag()
	c, err := newLinuxClassifier(linuxConfig{
		root:    "/proc",
		diag:    diag,
		env:     hostEnv(),
		selfPID: opts.SelfPID,
		never:   never,
		logf:    opts.Logf,
		minScan: minScanInterval,
		maxDirs: maxCandidateDirs,
	})
	if err != nil {
		diag.close()
		return nil, err
	}
	if err := c.selfTest(); err != nil {
		c.Close()
		return nil, err
	}
	c.start()
	return c, nil
}

func newLinuxClassifier(cfg linuxConfig) (*linuxClassifier, error) {
	fs, err := openProcFS(cfg.root)
	if err != nil {
		return nil, fmt.Errorf("process table: %w", err)
	}
	c := &linuxClassifier{
		logf:    cfg.logf,
		env:     cfg.env,
		never:   compileImages(cfg.env, cfg.never),
		diag:    cfg.diag,
		minScan: cfg.minScan,
		maxDirs: cfg.maxDirs,
		fs:      fs,
		lin:     newLineage(),
		procs:   make(map[int]*procEntry),
		cands:   make(map[int]*procEntry),
		cookies: make(map[uint64]cookieEntry),
		scanned: make(map[uint32]procKey),
		kick:    make(chan struct{}, 1),
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
	c.rulesOn.Store(true)
	c.mu.Lock()
	defer c.mu.Unlock()
	if cfg.selfPID > 0 {
		_, start, err := fs.stat(-1, cfg.selfPID)
		if err != nil {
			fs.close()
			return nil, fmt.Errorf("self process: %w", err)
		}
		c.self = procKey{cfg.selfPID, start}
	}
	if err := c.refreshLocked(time.Now()); err != nil {
		fs.close()
		return nil, fmt.Errorf("process snapshot: %w", err)
	}
	// Processes already running are left to the sweep: only new ones are watched for exec.
	c.young = nil
	return c, nil
}

func (c *linuxClassifier) start() {
	c.started.Store(true)
	go c.run()
}

func (c *linuxClassifier) run() {
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
		c.mu.Lock()
		if c.closed {
			c.mu.Unlock()
			return
		}
		if !c.rulesOn.Load() {
			c.useRulesLocked(nil)
			c.mu.Unlock()
			continue
		}
		// Until the owner reports rules, keep the ones Classify last used.
		if r := c.lastRules.Load(); r != nil {
			c.useRulesLocked(r)
		}
		now := time.Now()
		if err := c.refreshLocked(now); err != nil {
			c.limiter.logf(c.logf, "refresh", "split tunnel: process listing failed: %v", err)
		}
		c.sweepLocked()
		c.mu.Unlock()
	}
}

func (c *linuxClassifier) Close() error {
	c.closeOnce.Do(func() {
		close(c.stop)
		if c.started.Load() {
			<-c.done
		}
		c.diag.close()
		c.mu.Lock()
		c.closed = true
		for _, e := range c.cands {
			if e.dirfd >= 0 {
				unix.Close(e.dirfd)
				e.dirfd = -1
			}
		}
		c.dirfds = 0
		c.fs.close()
		c.mu.Unlock()
	})
	return nil
}

func (c *linuxClassifier) ObserveRules(rules *Rules) {
	on := !rules.Empty()
	prev := c.lastRules.Swap(rules)
	c.rulesOn.Store(on)
	if prev != rules {
		select {
		case c.kick <- struct{}{}:
		default:
		}
	}
}

func (c *linuxClassifier) Stats() Stats {
	return Stats{
		Lookups:     c.lookups.Load(),
		LookupFails: c.lookupFails.Load(),
		Fallbacks:   c.fallbacks.Load(),
		PathFails:   c.pathFails.Load(),
		Refreshes:   c.refreshes.Load(),
	}
}

func (c *linuxClassifier) Classify(rules *Rules, flows []FlowID) []Result {
	out := make([]Result, len(flows))
	if rules.Empty() || len(flows) == 0 {
		return out
	}
	callStart := time.Now()
	rows, _ := c.flowRows(flows)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return out
	}
	c.useRulesLocked(rules)
	c.resolveLocked(callStart, rows)
	if c.closed {
		return out
	}
	never := []*Rules{c.never, rules.never}
	for i := range flows {
		c.lookups.Add(1)
		rs := rows[i]
		if len(rs) == 0 {
			c.lookupFails.Add(1)
			continue
		}
		key, ok := c.rowsOwnerLocked(rs)
		if !ok {
			out[i].Owner.SockID = rs[0].cookie
			continue
		}
		v, chain := verdictFor(rules, never, c.self, c.lin.view(key, c.env.isStop))
		out[i] = Result{Verdict: v, Owner: Owner{PID: key.pid, Start: key.start, SockID: rs[0].cookie, Chain: chain}}
	}
	return out
}

// Validate confirms the port's sockets are still the classified socket, plus sockets the
// cache attributes to the same process (reuseport groups).
func (c *linuxClassifier) Validate(checks []SocketCheck) []bool {
	out := make([]bool, len(checks))
	if len(checks) == 0 {
		return out
	}
	flows := make([]FlowID, len(checks))
	for i, ch := range checks {
		flows[i] = FlowID{Proto: ch.Proto, App: ch.App}
	}
	rows, _ := c.flowRows(flows)
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	for i, ch := range checks {
		if ch.Owner.SockID == 0 || len(rows[i]) == 0 {
			continue
		}
		owner := procKey{ch.Owner.PID, ch.Owner.Start}
		hit, ok := false, true
		for _, r := range rows[i] {
			if r.cookie == ch.Owner.SockID {
				hit = true
				continue
			}
			ce, found := c.cookies[r.cookie]
			if !found || ce.owner != owner || owner.pid == 0 || now.After(ce.expires) {
				ok = false
				break
			}
		}
		out[i] = hit && ok
	}
	return out
}

// flowRows looks up every flow's candidate sockets: TCP by exact 4-tuple, UDP (and TCP without
// a remote) by a port-filtered dump of both families.
func (c *linuxClassifier) flowRows(flows []FlowID) ([][]diagRow, []error) {
	rows := make([][]diagRow, len(flows))
	errs := make([]error, len(flows))
	var udpPorts, tcpPorts []uint16
	var tcpErr error
	for i, f := range flows {
		switch f.Proto {
		case protoTCP:
			if !f.Remote.IsValid() {
				tcpPorts = appendPort(tcpPorts, f.App.Port())
				continue
			}
			if tcpErr != nil {
				errs[i] = tcpErr
				continue
			}
			rs, err := c.diag.tcpLookup(f.App, f.Remote)
			if err != nil {
				errs[i], tcpErr = err, err
				continue
			}
			rows[i] = diagRowsFor(rs, protoTCP, f.App, f.Remote)
		case protoUDP:
			udpPorts = appendPort(udpPorts, f.App.Port())
		}
	}
	fill := func(proto uint8, ports []uint16, states uint32) {
		if len(ports) == 0 {
			return
		}
		all, err := c.dumpPorts(proto, ports, states)
		for i, f := range flows {
			if f.Proto != proto || (proto == protoTCP && f.Remote.IsValid()) {
				continue
			}
			if err != nil {
				errs[i] = err
				continue
			}
			rows[i] = diagRowsFor(all, proto, f.App, f.Remote)
		}
	}
	fill(protoUDP, udpPorts, diagAllStates)
	fill(protoTCP, tcpPorts, diagLiveTCPStates)
	for _, err := range errs {
		if err != nil {
			c.limiter.logf(c.logf, "diag", "split tunnel: socket lookup failed: %v", err)
			break
		}
	}
	return rows, errs
}

func appendPort(ports []uint16, p uint16) []uint16 {
	if slices.Contains(ports, p) {
		return ports
	}
	return append(ports, p)
}

// dumpPorts filters in the kernel for a few ports and dumps everything for larger batches.
func (c *linuxClassifier) dumpPorts(proto uint8, ports []uint16, states uint32) ([]diagRow, error) {
	if len(ports) > fewPortsDump {
		return c.diag.dump(proto, 0, states)
	}
	var all []diagRow
	for _, p := range ports {
		rs, err := c.diag.dump(proto, p, states)
		if err != nil {
			return nil, err
		}
		all = append(all, rs...)
	}
	return all, nil
}

// rowsOwnerLocked requires every row to be held by the same live candidate.
func (c *linuxClassifier) rowsOwnerLocked(rs []diagRow) (procKey, bool) {
	var key procKey
	for i, r := range rs {
		ce, ok := c.cookies[r.cookie]
		if !ok || ce.owner.pid == 0 || !c.liveCandLocked(ce.owner) {
			return procKey{}, false
		}
		if i > 0 && ce.owner != key {
			return procKey{}, false
		}
		key = ce.owner
	}
	return key, true
}

func (c *linuxClassifier) liveCandLocked(k procKey) bool {
	e := c.procs[k.pid]
	return e != nil && e.key == k && e.cand
}

// resolveLocked attributes every uncached row; a scan answers flows read before it and its process
// listing started, and scans are at least minScan apart so a burst of new flows shares one.
func (c *linuxClassifier) resolveLocked(callStart time.Time, rows [][]diagRow) {
	now := time.Now()
	var pending []*diagRow
	uids := make(map[uint32]bool)
	for i := range rows {
		for j := range rows[i] {
			r := &rows[i][j]
			if c.cachedLocked(r.cookie, now) {
				continue
			}
			pending = append(pending, r)
			uids[r.uid] = true
		}
	}
	if len(pending) == 0 {
		return
	}
	if now.Sub(c.scanAt) < scanReuseWindow {
		if pending = c.assignLocked(pending, now, false); len(pending) == 0 {
			return
		}
	}
	for {
		if !c.scanAt.Before(callStart) && !c.scanFrom.Before(callStart) && coversUIDs(c.scanUIDs, uids) {
			break
		}
		if wait := c.minScan - time.Since(c.scanAt); wait > 0 {
			c.mu.Unlock()
			time.Sleep(wait)
			c.mu.Lock()
			if c.closed {
				return
			}
			continue
		}
		if c.refreshAt.Before(callStart) {
			if err := c.refreshLocked(time.Now()); err != nil {
				c.limiter.logf(c.logf, "refresh", "split tunnel: process listing failed: %v", err)
			}
		}
		c.scanLocked(uids)
		break
	}
	c.assignLocked(pending, time.Now(), true)
}

func coversUIDs(have, want map[uint32]bool) bool {
	for u := range want {
		if !have[u] {
			return false
		}
	}
	return true
}

// assignLocked caches the rows the last scan found in a candidate; with final set the rest are
// cached as held by no candidate.
func (c *linuxClassifier) assignLocked(pending []*diagRow, now time.Time, final bool) []*diagRow {
	rest := pending[:0]
	for _, r := range pending {
		if key, ok := c.scanned[r.inode]; ok && c.liveCandLocked(key) {
			c.putCookieLocked(r.cookie, cookieEntry{key, now.Add(bypassCookieTTL)})
			continue
		}
		if final {
			c.putCookieLocked(r.cookie, cookieEntry{procKey{}, now.Add(tunnelCookieTTL)})
			continue
		}
		rest = append(rest, r)
	}
	return rest
}

func (c *linuxClassifier) cachedLocked(cookie uint64, now time.Time) bool {
	ce, ok := c.cookies[cookie]
	if !ok {
		return false
	}
	if now.After(ce.expires) || (ce.owner.pid != 0 && !c.liveCandLocked(ce.owner)) {
		delete(c.cookies, cookie)
		return false
	}
	return true
}

func (c *linuxClassifier) putCookieLocked(cookie uint64, ce cookieEntry) {
	if _, ok := c.cookies[cookie]; !ok && len(c.cookies) >= maxCookies {
		now := time.Now()
		for k, v := range c.cookies {
			if now.After(v.expires) {
				delete(c.cookies, k)
			}
		}
		if len(c.cookies) >= maxCookies {
			clear(c.cookies)
		}
	}
	c.cookies[cookie] = ce
}

// purgeTunnelCookiesLocked forgets "no candidate" answers once an existing process became one.
func (c *linuxClassifier) purgeTunnelCookiesLocked() {
	for k, v := range c.cookies {
		if v.owner.pid == 0 {
			delete(c.cookies, k)
		}
	}
	c.scanAt = time.Time{}
}

// scanLocked maps the socket inodes held by candidates whose fsuid owns one of the sockets.
func (c *linuxClassifier) scanLocked(uids map[uint32]bool) {
	c.scans.Add(1)
	c.scanAt = time.Now()
	c.scanFrom = c.refreshAt
	c.scanUIDs = uids
	clear(c.scanned)
	for _, e := range c.cands {
		if !uids[e.fsuid] {
			continue
		}
		fd := e.dirfd
		if fd < 0 {
			c.fallbacks.Add(1)
			var err error
			if fd, err = c.fs.openPID(e.key.pid, e.key.start); err != nil {
				if goneErr(err) {
					c.removeLocked(e)
				}
				continue
			}
		}
		key := e.key
		err := c.fs.socketInodes(fd, func(ino uint32) {
			if prev, ok := c.scanned[ino]; !ok || key.start < prev.start {
				c.scanned[ino] = key
			}
		})
		if e.dirfd < 0 {
			unix.Close(fd)
		}
		if err != nil {
			if goneErr(err) {
				c.removeLocked(e)
			} else {
				c.limiter.logf(c.logf, "scan", "split tunnel: reading process descriptors failed: %v", err)
			}
		}
	}
}

// useRulesLocked recomputes the candidate set when the rules change or finish resolving.
func (c *linuxClassifier) useRulesLocked(rules *Rules) {
	if rules.Empty() {
		rules = nil
	}
	ready := rules.resolveDone() && c.never.resolveDone()
	if c.rules == rules {
		if c.rulesReady || !ready {
			return
		}
	} else if !c.rules.Equal(rules) {
		clear(c.cookies)
		c.scanAt = time.Time{}
		c.sweepAll = true
	}
	c.rules, c.rulesReady = rules, ready
	admitted := false
	for _, e := range c.procs {
		if c.setCandLocked(e, c.wantCandLocked(e)) {
			admitted = true
		}
	}
	if admitted {
		c.purgeTunnelCookiesLocked()
	}
}

func (c *linuxClassifier) wantCandLocked(e *procEntry) bool {
	if c.rules == nil {
		return false
	}
	v, _ := verdictFor(c.rules, []*Rules{c.never, c.rules.never}, c.self, c.lin.view(e.key, c.env.isStop))
	return v == VerdictBypass
}

// setCandLocked admits or drops a candidate and reports a new admission. A candidate keeps a
// /proc/<pid> descriptor verified against its start time, so a reused pid never reads as it.
func (c *linuxClassifier) setCandLocked(e *procEntry, cand bool) bool {
	if e.cand == cand {
		return false
	}
	if !cand {
		c.dropCandLocked(e)
		return false
	}
	fd, err := c.fs.openPID(e.key.pid, e.key.start)
	if err != nil {
		if goneErr(err) {
			c.removeLocked(e)
		}
		return false
	}
	uid, err := c.fs.fsuid(fd, e.key.pid)
	if err != nil {
		unix.Close(fd)
		if goneErr(err) {
			c.removeLocked(e)
		}
		return false
	}
	e.fsuid = uid
	if c.dirfds < c.maxDirs {
		e.dirfd = fd
		c.dirfds++
	} else {
		unix.Close(fd)
	}
	e.cand = true
	c.cands[e.key.pid] = e
	return true
}

func (c *linuxClassifier) dropCandLocked(e *procEntry) {
	if e.dirfd >= 0 {
		unix.Close(e.dirfd)
		e.dirfd = -1
		c.dirfds--
	}
	e.cand = false
	if c.cands[e.key.pid] == e {
		delete(c.cands, e.key.pid)
	}
}

func (c *linuxClassifier) removeLocked(e *procEntry) {
	if e.cand {
		c.dropCandLocked(e)
	}
	if c.procs[e.key.pid] == e {
		delete(c.procs, e.key.pid)
	}
	c.lin.retire(e.key)
}

// newEntryLocked reads a process seen for the first time; the second stat read proves the
// image belongs to the same process as the start time.
func (c *linuxClassifier) newEntryLocked(pid int) *procEntry {
	ppid, start, err := c.fs.stat(-1, pid)
	if err != nil {
		return nil
	}
	e := &procEntry{key: procKey{pid, start}, ppid: ppid, dirfd: -1, added: c.gen}
	if link, err := c.fs.exeLink(-1, pid); err == nil {
		e.link, e.path = link, c.fs.matchPath(-1, pid, link)
	} else if !goneErr(err) {
		c.pathFails.Add(1)
	}
	if _, again, err := c.fs.stat(-1, pid); err != nil || again != start {
		return nil
	}
	return e
}

// refreshLocked lists /proc, reads only processes not seen before, links them to their verified
// parents and re-checks the image of recently started processes (exec).
func (c *linuxClassifier) refreshLocked(now time.Time) error {
	pids, err := c.fs.listPIDs(c.pids[:0])
	c.pids = pids
	if err != nil {
		return err
	}
	c.gen++
	gen := c.gen
	var fresh []*procEntry
	for _, pid := range pids {
		e := c.procs[pid]
		if e == nil {
			if e = c.newEntryLocked(pid); e == nil {
				continue
			}
			c.procs[pid] = e
			fresh = append(fresh, e)
		}
		e.gen = gen
	}
	for _, e := range c.procs {
		if e.gen != gen {
			c.removeLocked(e)
		}
	}
	var verified map[int]bool
	for i := 0; i < len(fresh); i++ {
		p := c.procs[fresh[i].ppid]
		if p == nil || p.added == gen || verified[p.key.pid] {
			continue
		}
		if verified == nil {
			verified = make(map[int]bool)
		}
		verified[p.key.pid] = true
		if _, start, err := c.fs.stat(-1, p.key.pid); err == nil && start == p.key.start {
			continue
		}
		c.removeLocked(p)
		if ne := c.newEntryLocked(p.key.pid); ne != nil {
			ne.gen = gen
			c.procs[ne.key.pid] = ne
			fresh = append(fresh, ne)
		}
	}
	slices.SortFunc(fresh, func(a, b *procEntry) int {
		if a.key.start != b.key.start {
			if a.key.start < b.key.start {
				return -1
			}
			return 1
		}
		return a.key.pid - b.key.pid
	})
	for pass := 0; pass < 2; pass++ {
		for _, e := range fresh {
			n := c.lin.nodes[e.key]
			if pass == 1 && (n == nil || n.hasParent) {
				continue
			}
			var parent *procKey
			if p := c.procs[e.ppid]; p != nil && p != e && c.lin.has(p.key) {
				k := p.key
				parent = &k
			}
			if pass == 1 && parent == nil {
				continue
			}
			c.lin.add(procInfo{pid: e.key.pid, ppid: e.ppid, start: e.key.start, path: e.path}, parent)
		}
	}
	for _, e := range fresh {
		e.seen = now
	}
	changed := c.checkYoungLocked(now, gen)
	c.young = append(c.young, fresh...)
	for _, e := range fresh {
		if c.procs[e.key.pid] == e {
			c.setCandLocked(e, c.wantCandLocked(e))
		}
	}
	if len(changed) > 0 {
		c.reevalLocked(changed)
	}
	c.refreshAt = now
	c.refreshes.Add(1)
	return nil
}

// checkYoungLocked re-reads the image of processes seen in the last few seconds: a wrapper
// that execs the excluded program keeps its pid and start time.
func (c *linuxClassifier) checkYoungLocked(now time.Time, gen uint64) map[procKey]bool {
	var changed map[procKey]bool
	keep := c.young[:0]
	for _, e := range c.young {
		if c.procs[e.key.pid] != e || now.Sub(e.seen) > youngWindow {
			continue
		}
		keep = append(keep, e)
		if e.added == gen || !c.checkExecLocked(e) {
			continue
		}
		if changed == nil {
			changed = make(map[procKey]bool)
		}
		changed[e.key] = true
	}
	clear(c.young[len(keep):])
	c.young = keep
	return changed
}

// checkExecLocked reports whether the process now runs another image; an unchanged exe link makes
// the common case one readlink, and the sandbox's files are read again only after an exec.
func (c *linuxClassifier) checkExecLocked(e *procEntry) bool {
	link, err := c.fs.exeLink(e.dirfd, e.key.pid)
	if err != nil {
		if goneErr(err) && e.dirfd >= 0 {
			c.removeLocked(e)
		}
		return false
	}
	if link == e.link {
		return false
	}
	p := c.fs.matchPath(e.dirfd, e.key.pid, link)
	if e.dirfd < 0 {
		if _, start, err := c.fs.stat(-1, e.key.pid); err != nil || start != e.key.start {
			c.removeLocked(e)
			return false
		}
	}
	e.link = link
	if p == e.path {
		return false
	}
	e.path = p
	c.lin.setPath(e.key, p)
	return true
}

// reevalLocked recomputes candidacy for processes whose image changed and their descendants.
func (c *linuxClassifier) reevalLocked(changed map[procKey]bool) {
	withKids := false
	for k := range changed {
		if n := c.lin.nodes[k]; n != nil && n.refs > 0 {
			withKids = true
			break
		}
	}
	admitted := false
	eval := func(e *procEntry) {
		if e != nil && c.setCandLocked(e, c.wantCandLocked(e)) {
			admitted = true
		}
	}
	if withKids {
		for _, e := range c.procs {
			if changed[e.key] || c.lin.hasAncestor(e.key, changed) {
				eval(e)
			}
		}
	} else {
		for k := range changed {
			if e := c.procs[k.pid]; e != nil && e.key == k {
				eval(e)
			}
		}
	}
	if admitted {
		c.purgeTunnelCookiesLocked()
	}
}

// sweepLocked re-reads the start time and image of a rotating slice of processes so every pid
// is re-verified within sweepPeriod (all of them right after a rules change).
func (c *linuxClassifier) sweepLocked() {
	n := len(c.pids)
	if n == 0 {
		return
	}
	count := n
	if !c.sweepAll {
		per := int(sweepPeriod / refreshInterval)
		count = (n + per - 1) / per
	}
	c.sweepAll = false
	var changed map[procKey]bool
	for i := 0; i < count; i++ {
		if c.sweepPos >= n {
			c.sweepPos = 0
		}
		pid := c.pids[c.sweepPos]
		c.sweepPos++
		e := c.procs[pid]
		if e == nil {
			continue
		}
		_, start, err := c.fs.stat(-1, pid)
		if err != nil || start != e.key.start {
			if err == nil || goneErr(err) {
				c.removeLocked(e)
			}
			continue
		}
		if c.checkExecLocked(e) {
			if changed == nil {
				changed = make(map[procKey]bool)
			}
			changed[e.key] = true
		}
	}
	if len(changed) > 0 {
		c.reevalLocked(changed)
	}
}

// selfTest resolves the classifier's own loopback TCP and UDP sockets through sock_diag and
// its own /proc descriptors; a kernel without udp_diag fails here instead of tunnelling silently.
func (c *linuxClassifier) selfTest() error {
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
	protos := []string{"tcp", "udp"}
	rows, errs := c.flowRows(flows)
	for i, proto := range protos {
		if errors.Is(errs[i], errDiagUnsupported) {
			return fmt.Errorf("self-test: %s socket diagnostics unavailable (%s_diag)", proto, proto)
		}
		if errs[i] != nil {
			return fmt.Errorf("self-test: %s socket lookup: %w", proto, errs[i])
		}
		if len(rows[i]) == 0 {
			return fmt.Errorf("self-test: %s socket not found", proto)
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	fd, err := c.fs.openPID(c.self.pid, c.self.start)
	if err != nil {
		return fmt.Errorf("self-test: /proc access: %w", err)
	}
	defer unix.Close(fd)
	held := make(map[uint32]bool)
	if err := c.fs.socketInodes(fd, func(ino uint32) { held[ino] = true }); err != nil {
		return fmt.Errorf("self-test: /proc descriptor access: %w", err)
	}
	for i, proto := range protos {
		if !slices.ContainsFunc(rows[i], func(r diagRow) bool { return held[r.inode] }) {
			return fmt.Errorf("self-test: %s socket owner not found", proto)
		}
	}
	images := ownImagesLinux()
	if p, err := c.fs.exePath(fd, c.self.pid); err != nil || (len(images) > 0 && !containsString(images, p)) {
		return fmt.Errorf("self-test: own image not resolved")
	}
	return nil
}

func ownImagesLinux() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	out := []string{exe}
	if real, err := filepath.EvalSymlinks(exe); err == nil && real != exe {
		out = append(out, real)
	}
	return out
}
