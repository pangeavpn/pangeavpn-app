package splittunnel

import (
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/udp"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
)

type limits struct {
	outQueue       int
	pendingFlows   int
	pendingBytes   int
	tcpQueue       int
	bypassTCP      int
	cacheEntries   int
	batch          int
	batchWait      time.Duration
	pendingTimeout time.Duration
	tunnelTTL      time.Duration
	linger         time.Duration
	lingerCap      int
	dialFailLinger time.Duration
	endedTTL       time.Duration
	embryonicTTL   time.Duration
	dialTimeout    time.Duration
	sweepEvery     time.Duration
	scanEvery      time.Duration
	timeWait       time.Duration
	finLinger      time.Duration

	udpQueueBytes    int
	udpSessions      int
	udpCreating      int
	udpPeers         int
	udpSendQ         int
	udpFresh         time.Duration
	udpRevalidate    time.Duration
	udpIdle          time.Duration
	validateEvery    time.Duration
	validateStale    time.Duration
	failSafeBase     time.Duration
	failSafeMax      time.Duration
	egressBackoff    time.Duration
	listenTimeout    time.Duration
	unreachableEvery time.Duration
	fragTTL          time.Duration
	fragMax          int
	fragBytes        int
}

var defaultLimits = limits{
	outQueue:       1024,
	pendingFlows:   1024,
	pendingBytes:   4 << 20,
	tcpQueue:       32,
	bypassTCP:      2048,
	cacheEntries:   65536,
	batch:          64,
	batchWait:      time.Millisecond,
	pendingTimeout: time.Second,
	tunnelTTL:      30 * time.Second,
	linger:         41 * time.Second,
	lingerCap:      8192,
	dialFailLinger: 10 * time.Second,
	endedTTL:       2 * time.Minute,
	embryonicTTL:   30 * time.Second,
	dialTimeout:    20 * time.Second,
	sweepEvery:     100 * time.Millisecond,
	scanEvery:      time.Second,
	timeWait:       time.Second,
	finLinger:      10 * time.Second,

	// One 65507-byte datagram at MTU 1280 is 53 fragments, each held in a 2 KiB buffer.
	udpQueueBytes:    112 << 10,
	udpSessions:      1024, // one per app socket, not per peer; each pins a 64 KiB read buffer
	udpCreating:      64,
	udpPeers:         4096,
	udpSendQ:         64,
	udpFresh:         time.Second,
	udpRevalidate:    30 * time.Second,
	udpIdle:          2 * time.Minute,
	validateEvery:    time.Second,
	validateStale:    5 * time.Second,
	failSafeBase:     5 * time.Second,
	failSafeMax:      60 * time.Second,
	egressBackoff:    5 * time.Second,
	listenTimeout:    3 * time.Second,
	unreachableEvery: 2 * time.Second,
	fragTTL:          2 * time.Second,
	fragMax:          256,
	fragBytes:        4 << 20,
}

type action uint8

const (
	actTunnel action = iota
	actDrop
	actStack
	actHeld
)

type flowState uint8

const (
	statePending flowState = iota
	stateTunnel
	stateBypass
	stateLinger
)

// flow is one unit the classifier decides on: a TCP connection or a UDP source port.
// Callbacks capture the pointer and only touch the table while it is still the entry for its key.
type flow struct {
	proto     uint8
	key       flowKey
	isn       uint32
	state     flowState
	draining  bool
	submitted bool
	deadline  time.Time
	queue     []*packetBuf
	queueCost int
	owner     procmatch.Owner
	prev      []abortTarget

	awaitingDial bool
	live         bool
	aborted      bool
	endedAt      time.Time
	ep           tcpip.Endpoint
	phys         net.Conn

	sess         *udpSession
	validatedAt  time.Time
	lastSeen     time.Time
	expiresAt    time.Time
	holdDeadline time.Time
	failStreak   uint8
	failSafe     bool
	egressDown   bool
	revalidating bool
	holding      bool
}

// abortTarget is a splice to reset outside the flow mutex.
type abortTarget struct {
	ep   tcpip.Endpoint
	phys net.Conn
}

func (t abortTarget) run() {
	if t.ep != nil {
		t.ep.Abort()
	}
	if t.phys != nil {
		resetConn(t.phys)
	}
}

func runTargets(ts []abortTarget) {
	for _, t := range ts {
		t.run()
	}
}

type engine struct {
	d      *Device
	c      *Controller
	lim    limits
	out    chan *packetBuf
	batch  [][]byte
	sizes  []int
	offset int
	link   *linkEndpoint
	stack  *stack.Stack
	kick   chan struct{}
	ipID   atomic.Uint32

	mu          sync.Mutex
	closed      bool
	tcp         map[flowKey]*flow
	pendingQ    []*flow
	inflight    []*flow
	pending     int
	pendingCost int
	bypassTCP   int
	tunnelTCP   fifoCache
	endedTCP    fifoCache
	lastScan    time.Time

	udp          map[uint16]*flow
	udpLive      map[*flow]struct{}
	revalQ       []*flow
	holdQ        []*flow
	udpCreating  int
	egressDownTo time.Time
	valDue       bool
	lastValidate time.Time
	endedUDP     fifoCache
	frags        fragTracker
}

func newEngine(d *Device, bufLen, offset int) (*engine, error) {
	lim := d.c.lim
	bs := d.inner.BatchSize()
	if bs < 1 {
		bs = 1
	}
	e := &engine{
		d:       d,
		c:       d.c,
		lim:     lim,
		out:     make(chan *packetBuf, lim.outQueue),
		batch:   make([][]byte, bs),
		sizes:   make([]int, bs),
		offset:  offset,
		kick:    make(chan struct{}, 1),
		tcp:     make(map[flowKey]*flow),
		udp:     make(map[uint16]*flow),
		udpLive: make(map[*flow]struct{}),
	}
	for i := range e.batch {
		e.batch[i] = make([]byte, bufLen)
	}
	e.tunnelTCP.init(lim.cacheEntries)
	e.endedTCP.init(lim.cacheEntries)
	e.endedUDP.init(lim.cacheEntries)
	e.frags.init()
	e.link = newLinkEndpoint(d, uint32(d.mtu.Load()))
	s, err := newEngineStack(e.link, lim, e.handleTCP)
	if err != nil {
		return nil, err
	}
	s.SetTransportProtocolHandler(udp.ProtocolNumber, e.handleUDP)
	e.stack = s
	return e, nil
}

func (e *engine) start() {
	go e.pump()
	go e.worker()
	go e.sweeper()
}

// route handles one packet read from the TUN; keepTunnel leaves tunnel-bound packets in
// the caller's buffer (handover batch). It reports whether the packet was diverted.
func (e *engine) route(pkt []byte, now time.Time, keepTunnel bool) bool {
	var sp spill
	diverted := true
	switch e.decide(pkt, now, &sp) {
	case actTunnel:
		if keepTunnel {
			diverted = false
		} else {
			e.pushOut(copyPacket(pkt))
		}
	case actStack:
		e.link.deliver(pkt)
	}
	e.flushSpill(&sp)
	return diverted
}

// routeSafely turns a panic while handling a packet into tunnel-only operation; the
// packet itself still goes to the tunnel.
func (e *engine) routeSafely(pkt []byte, now time.Time, keepTunnel bool) (diverted bool) {
	defer func() {
		if r := recover(); r != nil {
			e.d.fault(r)
			diverted = false
			if !keepTunnel {
				diverted = true
				e.pushOut(copyPacket(pkt))
			}
		}
	}()
	return e.route(pkt, now, keepTunnel)
}

func (e *engine) pushOut(p *packetBuf) {
	select {
	case e.out <- p:
	default:
		p.release()
		e.d.stats.outDrops.Add(1)
	}
}

// gateOpen is whether a new flow may be considered for bypass at all.
func (e *engine) gateOpen() bool {
	return !e.d.rules.Load().empty() && e.c.permitted.Load() && !e.c.engDown.Load() && !e.d.tunnelOnly.Load()
}

// decide applies the always-tunnel rules, then the per-protocol flow state. It never
// blocks and never logs: it runs on the only goroutine that reads the TUN.
func (e *engine) decide(pkt []byte, now time.Time, sp *spill) action {
	if e.d.tunnelOnly.Load() {
		return actTunnel
	}
	info, ok := parseIPv4(pkt)
	if !ok {
		return actTunnel
	}
	if info.frag {
		if info.proto != protoUDP || info.src != e.d.addr {
			return actTunnel
		}
		return e.decideFrag(&info, pkt, now, sp)
	}
	switch info.proto {
	case protoTCP, protoUDP:
	case protoICMP:
		return e.decideICMP(&info, pkt, now)
	default:
		return actTunnel
	}
	if info.src != e.d.addr {
		return actTunnel
	}
	if info.dstPort == 53 || info.dstPort == 853 {
		return actTunnel
	}
	if alwaysTunnelDst(info.dst) || e.d.isTunnelDNS(info.dst) {
		return actTunnel
	}
	if info.proto == protoUDP {
		return e.decideUDP(&info, pkt, now)
	}
	return e.decideTCP(&info, pkt, now)
}

// decideICMP drops ICMP errors about bypassed flows: they would tell the remote, through
// the VPN, which tunnel address an excluded flow belongs to.
func (e *engine) decideICMP(info *pktInfo, pkt []byte, now time.Time) action {
	q, ok := icmpQuote(pkt[info.ihl:info.total])
	if !ok || q.dst != e.d.addr {
		return actTunnel
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	switch q.proto {
	case protoTCP:
		key := flowKey{srcPort: q.dstPort, dst: netip.AddrPortFrom(q.src, q.srcPort)}
		if f := e.tcp[key]; f != nil && (f.state == stateBypass || f.state == stateLinger) {
			return actDrop
		}
		if e.endedTCP.get(key, now) != nil {
			return actDrop
		}
	case protoUDP:
		if f := e.udp[q.dstPort]; f != nil && f.sess != nil {
			return actDrop
		}
		if e.endedUDP.get(flowKey{srcPort: q.dstPort}, now) != nil {
			return actDrop
		}
	}
	return actTunnel
}

func (e *engine) decideTCP(info *pktInfo, pkt []byte, now time.Time) action {
	key := flowKey{srcPort: info.srcPort, dst: netip.AddrPortFrom(info.dst, info.dstPort)}
	syn := info.flags&tcpFlagSyn != 0 && info.flags&tcpFlagAck == 0
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return actTunnel
	}
	if f := e.tcp[key]; f != nil {
		if syn && (f.isn != info.seq || f.state == stateLinger || f.aborted) {
			return e.newTCPLocked(key, info.seq, pkt, now, e.dropFlowLocked(f))
		}
		switch {
		case f.state == statePending || f.draining:
			return e.enqueueLocked(f, pkt)
		case f.state == stateTunnel:
			return actTunnel
		}
		return actStack
	}
	if ce := e.tunnelTCP.get(key, now); ce != nil {
		if syn && ce.isn != info.seq {
			e.tunnelTCP.remove(key)
			return e.newTCPLocked(key, info.seq, pkt, now, nil)
		}
		return actTunnel
	}
	if e.endedTCP.get(key, now) != nil {
		if syn {
			e.endedTCP.remove(key)
			return e.newTCPLocked(key, info.seq, pkt, now, nil)
		}
		return actDrop
	}
	if syn {
		return e.newTCPLocked(key, info.seq, pkt, now, nil)
	}
	return actTunnel
}

// newTCPLocked starts classifying a SYN. prev holds the splice of the connection it
// replaces; it is reset by the worker before the new flow's packets move on.
func (e *engine) newTCPLocked(key flowKey, isn uint32, pkt []byte, now time.Time, prev []abortTarget) action {
	if !e.gateOpen() || e.pending >= e.lim.pendingFlows || e.pendingCost+engineBufSize > e.lim.pendingBytes {
		if len(prev) > 0 {
			go func() {
				defer e.recoverFault()
				runTargets(prev)
			}()
		}
		return actTunnel
	}
	f := &flow{proto: protoTCP, key: key, isn: isn, state: statePending, deadline: now.Add(e.lim.pendingTimeout), prev: prev}
	e.tcp[key] = f
	e.pending++
	e.pendingQ = append(e.pendingQ, f)
	e.enqueueLocked(f, pkt)
	e.kickWorker()
	return actHeld
}

func (e *engine) enqueueLocked(f *flow, pkt []byte) action {
	if len(f.queue) >= e.lim.tcpQueue && !f.draining {
		return actDrop
	}
	p := copyPacket(pkt)
	cost := p.bufCost()
	if e.pendingCost+cost > e.lim.pendingBytes {
		p.release()
		return actDrop
	}
	f.queue = append(f.queue, p)
	f.queueCost += cost
	e.pendingCost += cost
	return actHeld
}

// currentLocked reports whether f is still the table entry for its key.
func (e *engine) currentLocked(f *flow) bool {
	if f.proto == protoUDP {
		return e.udp[f.key.srcPort] == f
	}
	return e.tcp[f.key] == f
}

func (e *engine) takeQueueLocked(f *flow) []*packetBuf {
	q := f.queue
	e.pendingCost -= f.queueCost
	f.queue, f.queueCost = nil, 0
	return q
}

func (e *engine) dropQueueLocked(f *flow) {
	for _, p := range e.takeQueueLocked(f) {
		p.release()
	}
}

func (e *engine) releaseLiveLocked(f *flow) {
	if f.live {
		f.live = false
		e.bypassTCP--
		e.d.stats.bypassTCP.Add(-1)
	}
}

// dropFlowLocked deletes an entry for good and returns what must be reset outside the lock.
func (e *engine) dropFlowLocked(f *flow) []abortTarget {
	if e.tcp[f.key] == f {
		delete(e.tcp, f.key)
	}
	if f.state == statePending {
		e.pending--
	}
	e.dropQueueLocked(f)
	if f.awaitingDial {
		f.awaitingDial = false
		e.releaseLiveLocked(f)
	}
	f.aborted = true
	ts := f.prev
	f.prev = nil
	if f.ep != nil || f.phys != nil {
		ts = append(ts, abortTarget{ep: f.ep, phys: f.phys})
	}
	return ts
}

// abortLocked resets a live bypass flow and keeps its key lingering: non-SYN segments
// still reach gVisor (which answers RST) and any SYN reclassifies.
func (e *engine) abortLocked(f *flow, now time.Time) abortTarget {
	f.aborted = true
	f.state = stateLinger
	f.endedAt = now
	f.deadline = now.Add(e.lim.linger)
	if f.awaitingDial {
		f.awaitingDial = false
		e.releaseLiveLocked(f)
	}
	e.d.stats.torn.Add(1)
	return abortTarget{ep: f.ep, phys: f.phys}
}

// rulesChanged resets bypassed connections and closes UDP sessions whose owner no longer
// matches; flows still pending pick up the new rules when they resolve.
func (e *engine) rulesChanged(rs *ruleSet) {
	now := time.Now()
	var targets []abortTarget
	var closing []*udpSession
	// Deferred so flows already marked aborted are still reset if a later check panics.
	defer func() {
		closeSessions(closing)
		runTargets(targets)
	}()
	e.withLock(func() {
		for _, f := range e.tcp {
			if f.state == stateBypass && !f.aborted && !rs.allows(f.owner.Chain) {
				targets = append(targets, e.abortLocked(f, now))
			}
		}
		closing = e.udpRulesChangedLocked(rs, now)
	})
}

// failSafeAll resets every bypassed connection, closes every UDP session and sends
// pending flows to the tunnel.
func (e *engine) failSafeAll() {
	now := time.Now()
	var targets []abortTarget
	var pending []*flow
	var closing []*udpSession
	var drains []drainJob
	e.withLock(func() {
		for _, f := range e.tcp {
			switch {
			case f.state == stateBypass && !f.aborted:
				targets = append(targets, e.abortLocked(f, now))
			case f.state == statePending:
				pending = append(pending, f)
			}
		}
		for _, f := range e.udp {
			switch {
			case f.sess != nil:
				e.d.stats.torn.Add(1)
				closing = append(closing, e.endSessionLocked(f, now))
				if q := e.failSafeUDPLocked(f, now, false); q != nil {
					drains = append(drains, drainJob{f: f, q: q})
				}
			case f.state == statePending:
				pending = append(pending, f)
			}
		}
	})
	closeSessions(closing)
	runTargets(targets)
	for _, j := range drains {
		e.drain(j.f, j.q, sink{})
	}
	for _, f := range pending {
		e.resolve(f, procmatch.Result{}, nil, now)
	}
}

func (e *engine) kickWorker() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *engine) pump() {
	for {
		n, err := e.d.inner.Read(e.batch, e.sizes, e.offset)
		if n > 0 {
			now := time.Now()
			for i := 0; i < n && i < len(e.batch); i++ {
				if s := e.sizes[i]; s > 0 && e.offset+s <= len(e.batch[i]) {
					e.routeSafely(e.batch[i][e.offset:e.offset+s], now, false)
				}
			}
		}
		if err != nil {
			if errors.Is(err, tun.ErrTooManySegments) {
				e.d.stats.tooMany.Add(1)
				continue
			}
			e.d.setTerm(err)
			return
		}
	}
}

// recoverFault, deferred first in a goroutine outside safely, gives a panic there the same
// tunnel-only fallback instead of taking down the privileged daemon.
func (e *engine) recoverFault() {
	if r := recover(); r != nil {
		e.d.fault(r)
	}
}

// safely runs one worker or sweeper step; a panic switches the device to tunnel-only.
func (e *engine) safely(fn func() bool) (more bool) {
	defer func() {
		if r := recover(); r != nil {
			e.d.fault(r)
			more = false
		}
	}()
	return fn()
}

// withLock runs fn holding the flow mutex; the deferred unlock keeps a panic recovered by
// safely or routeSafely from leaving the mutex held.
func (e *engine) withLock(fn func()) {
	e.mu.Lock()
	defer e.mu.Unlock()
	fn()
}

func (e *engine) worker() {
	timer := time.NewTimer(time.Hour)
	timer.Stop()
	for {
		select {
		case <-e.d.done:
			return
		case <-e.kick:
		}
		if e.queuedCount() < e.lim.batch {
			timer.Reset(e.lim.batchWait)
			select {
			case <-e.d.done:
				timer.Stop()
				return
			case <-timer.C:
			}
		}
		// Owner checks run between batches: a queue that never empties must not starve them.
		for e.safely(e.classifyOnce) {
			e.safely(e.validateOnce)
		}
		e.safely(e.validateOnce)
	}
}

func (e *engine) queuedCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.pendingQ) + len(e.revalQ)
}

func (e *engine) takeBatch() []*flow {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil
	}
	var batch []*flow
	i := 0
	for ; i < len(e.pendingQ) && len(batch) < e.lim.batch; i++ {
		f := e.pendingQ[i]
		if !e.currentLocked(f) || f.state != statePending || f.submitted {
			continue
		}
		f.submitted = true
		batch = append(batch, f)
	}
	n := copy(e.pendingQ, e.pendingQ[i:])
	clear(e.pendingQ[n:])
	e.pendingQ = e.pendingQ[:n]
	i = 0
	for ; i < len(e.revalQ) && len(batch) < e.lim.batch; i++ {
		f := e.revalQ[i]
		if !e.currentLocked(f) || f.state != stateTunnel || !f.revalidating || f.submitted {
			continue
		}
		f.submitted = true
		batch = append(batch, f)
	}
	n = copy(e.revalQ, e.revalQ[i:])
	clear(e.revalQ[n:])
	e.revalQ = e.revalQ[:n]
	e.inflight = batch
	return batch
}

// classifyOnce resolves one batch; it reports false when nothing could be done now.
func (e *engine) classifyOnce() bool {
	cls, dl, st := e.c.engines()
	switch st {
	case engineNone:
		e.c.ensureEngines()
		return false
	case engineStarting:
		return false
	case engineFailed:
		e.c.ensureEngines()
	}
	batch := e.takeBatch()
	if len(batch) == 0 {
		return false
	}
	rs := e.d.rules.Load()
	snapAt := time.Now()
	var results []procmatch.Result
	if st == engineReady && cls != nil && dl != nil && !rs.empty() {
		ids := make([]procmatch.FlowID, len(batch))
		for i, f := range batch {
			ids[i] = procmatch.FlowID{Proto: f.proto, App: netip.AddrPortFrom(e.d.addr, f.key.srcPort), Remote: f.key.dst}
		}
		results = e.classify(cls, rs, ids)
		e.d.stats.classified.Add(uint64(len(batch)))
		if results == nil {
			e.d.stats.lookupFails.Add(uint64(len(batch)))
			rs = nil
		}
	} else {
		rs = nil
	}
	now := time.Now()
	for i, f := range batch {
		var res procmatch.Result
		if rs != nil {
			res = results[i]
			// A found socket that no candidate process holds comes back with PID 0 and its SockID (Linux).
			if res.Owner.PID == 0 && res.Owner.SockID == 0 {
				e.d.stats.lookupFails.Add(1)
			}
		}
		e.resolveAt(f, res, rs, snapAt, now)
	}
	e.withLock(func() { e.inflight = nil })
	return true
}

func (e *engine) classify(cls procmatch.Classifier, rs *ruleSet, ids []procmatch.FlowID) (res []procmatch.Result) {
	defer func() {
		if r := recover(); r != nil {
			res = nil
			e.c.log.limited("classify", "split tunnel: app lookup failed")
		}
	}()
	res = cls.Classify(rs.rules, ids)
	if len(res) != len(ids) {
		e.c.log.limited("classify-len", "split tunnel: app lookup returned %d results for %d flows", len(res), len(ids))
		return nil
	}
	return res
}

// bypassVerdictLocked is the verdict for res under the rules in force, recomputed from the
// owner chain when the rules changed while Classify ran.
func (e *engine) bypassVerdictLocked(res procmatch.Result, classified *ruleSet) bool {
	if classified == nil || !e.gateOpen() {
		return false
	}
	if cur := e.d.rules.Load(); cur != classified {
		return cur.allows(res.Owner.Chain)
	}
	return res.Verdict == procmatch.VerdictBypass
}

func (e *engine) resolve(f *flow, res procmatch.Result, classified *ruleSet, now time.Time) bool {
	return e.resolveAt(f, res, classified, now, now)
}

// resolveAt installs a verdict for a pending flow (classified nil = fail-safe tunnel) taken
// from a snapshot at snapAt, and drains its queue before the verdict is final.
func (e *engine) resolveAt(f *flow, res procmatch.Result, classified *ruleSet, snapAt, now time.Time) bool {
	if f.proto == protoUDP {
		return e.resolveUDP(f, res, classified, snapAt, now)
	}
	var prev []abortTarget
	var q []*packetBuf
	var k sink
	resolved := false
	e.withLock(func() {
		if e.closed || !e.currentLocked(f) || f.state != statePending {
			return
		}
		bypass := e.bypassTCP < e.lim.bypassTCP && e.bypassVerdictLocked(res, classified)
		resolved = true
		e.pending--
		f.submitted = false
		f.owner = res.Owner
		f.draining = true
		if bypass {
			f.state = stateBypass
			f.live = true
			f.awaitingDial = true
			f.deadline = now.Add(e.lim.embryonicTTL)
			e.bypassTCP++
			e.d.stats.bypassTCP.Add(1)
			e.d.stats.bypassed.Add(1)
			k.kind = sinkStack
		} else {
			f.state = stateTunnel
		}
		prev = f.prev
		f.prev = nil
		q = e.takeQueueLocked(f)
	})
	if !resolved {
		return false
	}
	runTargets(prev)
	e.drain(f, q, k)
	return true
}

type sinkKind uint8

const (
	sinkOut sinkKind = iota
	sinkStack
	sinkSession
)

// sink is where a flow's queued packets go once its verdict is known.
type sink struct {
	kind sinkKind
	s    *udpSession
}

func (e *engine) sinkLocked(f *flow) sink {
	switch {
	case f.proto == protoTCP && f.state == stateTunnel:
		return sink{}
	case f.proto == protoTCP:
		return sink{kind: sinkStack}
	case f.state == stateBypass && f.sess != nil:
		return sink{kind: sinkSession, s: f.sess}
	}
	return sink{}
}

type drainJob struct {
	f *flow
	q []*packetBuf
}

// drain delivers a resolved flow's queue; the pump keeps appending while draining is set,
// so packets the pump reads meanwhile never overtake the queue.
func (e *engine) drain(f *flow, q []*packetBuf, k sink) {
	for more := true; more; {
		e.deliver(q, k)
		q, k, more = e.drainNext(f)
	}
}

// drainNext takes what the pump queued during the last delivery; with nothing left the
// flow stops draining and its verdict is final.
func (e *engine) drainNext(f *flow) ([]*packetBuf, sink, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || !e.currentLocked(f) {
		return nil, sink{}, false
	}
	if len(f.queue) == 0 {
		f.draining = false
		if f.proto == protoTCP && f.state == stateTunnel {
			delete(e.tcp, f.key)
			e.tunnelTCP.put(f.key, f.isn, time.Now().Add(e.lim.tunnelTTL))
		}
		return nil, sink{}, false
	}
	return e.takeQueueLocked(f), e.sinkLocked(f), true
}

func (e *engine) deliver(q []*packetBuf, k sink) {
	if len(q) == 0 {
		return
	}
	now := time.Now()
	for _, p := range q {
		switch k.kind {
		case sinkStack:
			e.link.deliver(p.data)
			p.release()
		case sinkSession:
			e.toSession(k.s, p, now)
		default:
			e.pushOut(p)
		}
	}
}

func (e *engine) sweeper() {
	t := time.NewTicker(e.lim.sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-e.d.done:
			return
		case now := <-t.C:
			e.safely(func() bool { e.sweep(now); return false })
		}
	}
}

// sweep enforces the pending and hold deadlines even while Classify or Validate is stuck,
// and ages embryonic, lingering, idle and cached entries.
func (e *engine) sweep(now time.Time) {
	var expired []*flow
	var flush []*packetBuf
	var closing []*udpSession
	var drains []drainJob
	var targets []abortTarget
	e.withLock(func() {
		for _, list := range [][]*flow{e.pendingQ, e.inflight} {
			for _, f := range list {
				if f.state == statePending && e.currentLocked(f) && now.After(f.deadline) {
					expired = append(expired, f)
				}
			}
		}
		closing, drains = e.sweepHoldsLocked(now, closing, drains)
		for _, r := range e.frags.expired(now) {
			flush = append(flush, e.frags.takeHeld(r)...)
		}
		if len(e.udpLive) > 0 && now.Sub(e.lastValidate) >= e.lim.validateEvery {
			e.valDue = true
			e.kickWorker()
		}
		if now.Sub(e.lastScan) < e.lim.scanEvery {
			return
		}
		e.lastScan = now
		e.compactPendingLocked()
		for _, f := range e.tcp {
			switch f.state {
			case stateBypass:
				if f.awaitingDial && now.After(f.deadline) {
					targets = append(targets, e.dropFlowLocked(f)...)
				}
			case stateLinger:
				if now.After(f.deadline) && !f.draining {
					delete(e.tcp, f.key)
					e.endedTCP.put(f.key, 0, f.endedAt.Add(e.lim.endedTTL))
				}
			}
		}
		e.tunnelTCP.sweep(now)
		e.endedTCP.sweep(now)
		e.endedUDP.sweep(now)
		closing, drains = e.scanUDPLocked(now, closing, drains)
	})
	closeSessions(closing)
	runTargets(targets)
	for _, p := range flush {
		e.pushOut(p)
	}
	for _, j := range drains {
		e.drain(j.f, j.q, sink{})
	}
	for _, f := range expired {
		if e.resolve(f, procmatch.Result{}, nil, now) {
			e.d.stats.pendingTimeouts.Add(1)
		}
	}
}

// compactPendingLocked forgets queue slots of flows that resolved or were replaced
// while the worker could not take batches.
func (e *engine) compactPendingLocked() {
	n := 0
	for _, f := range e.pendingQ {
		if f.state == statePending && !f.submitted && e.currentLocked(f) {
			e.pendingQ[n] = f
			n++
		}
	}
	clear(e.pendingQ[n:])
	e.pendingQ = e.pendingQ[:n]
	n = 0
	for _, f := range e.revalQ {
		if f.state == stateTunnel && f.revalidating && !f.submitted && e.currentLocked(f) {
			e.revalQ[n] = f
			n++
		}
	}
	clear(e.revalQ[n:])
	e.revalQ = e.revalQ[:n]
}

// finishFlow moves a bypass flow whose splice or dial has ended into linger, if it is
// still the entry, and releases its slot against the bypass cap.
func (e *engine) finishFlow(f *flow, linger time.Duration) {
	now := time.Now()
	e.mu.Lock()
	defer e.mu.Unlock()
	e.releaseLiveLocked(f)
	f.ep, f.phys = nil, nil
	if e.tcp[f.key] == f && f.state == stateBypass {
		f.endedAt = now
		// Past the cap it goes straight to the bounded ended cache: a burst of short connections
		// must not grow the table every scan walks under e.mu. Late segments are dropped there.
		if len(e.tcp) >= e.lim.lingerCap && !f.draining {
			delete(e.tcp, f.key)
			e.endedTCP.put(f.key, 0, now.Add(e.lim.endedTTL))
			return
		}
		f.state = stateLinger
		f.deadline = now.Add(linger)
	}
}

func (e *engine) shutdown() {
	var targets []abortTarget
	var closing []*udpSession
	e.withLock(func() {
		e.closed = true
		for _, f := range e.tcp {
			if f.phys != nil {
				targets = append(targets, abortTarget{phys: f.phys})
			}
			targets = append(targets, f.prev...)
			f.prev = nil
			e.dropQueueLocked(f)
		}
		now := time.Now()
		for _, f := range e.udp {
			if f.sess != nil {
				closing = append(closing, e.endSessionLocked(f, now))
			}
			e.dropQueueLocked(f)
		}
		for _, p := range e.frags.clear() {
			p.release()
		}
	})
	e.link.Attach(nil)
	e.stack.Close()
	for _, ep := range e.stack.CleanupEndpoints() {
		ep.Abort()
	}
	closeSessions(closing)
	runTargets(targets)
}
