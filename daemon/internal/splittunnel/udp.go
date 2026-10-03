package splittunnel

import (
	"net/netip"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/network/ipv4"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
)

func (e *engine) decideUDP(info *pktInfo, pkt []byte, now time.Time) action {
	if _, _, ok := info.udpPayload(); !ok {
		return actTunnel
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return actTunnel
	}
	act, _, _ := e.udpLocked(info, pkt, now, false)
	return act
}

// udpLocked applies the per-port UDP verdict. For a first fragment (frag) a bypass verdict
// means "inject for reassembly"; queued reports that the packet joined the port's queue.
func (e *engine) udpLocked(info *pktInfo, pkt []byte, now time.Time, frag bool) (act action, f *flow, queued bool) {
	dst := netip.AddrPortFrom(info.dst, info.dstPort)
	f = e.udp[info.srcPort]
	if f == nil {
		return e.newUDPLocked(info.srcPort, dst, pkt, now, 0)
	}
	f.lastSeen = now
	switch {
	case f.state == statePending || f.draining:
		return e.enqueueUDPLocked(f, pkt), f, true
	case f.state == stateTunnel:
		if f.failSafe && now.After(f.expiresAt) {
			streak := f.failStreak
			delete(e.udp, info.srcPort)
			return e.newUDPLocked(info.srcPort, dst, pkt, now, streak)
		}
		if !f.failSafe && !f.revalidating && now.Sub(f.validatedAt) >= e.lim.udpRevalidate && e.gateOpen() {
			f.revalidating = true
			e.revalQ = append(e.revalQ, f)
			e.kickWorker()
		}
		return actTunnel, nil, false
	}
	s := f.sess
	if s == nil {
		return actTunnel, nil, false
	}
	if s.knows(info.dst) || (!f.holding && now.Sub(f.validatedAt) <= e.lim.udpFresh) {
		if frag {
			s.admit(info.dst, now)
			return actStack, f, false
		}
		lo, hi, _ := info.udpPayload()
		s.send(udpOut{dst: dst, p: copyPacket(pkt[lo:hi])}, now)
		return actHeld, f, false
	}
	// A new destination is only trusted on a fresh owner check (UST-1): hold it for Validate.
	if !f.holding {
		f.holding = true
		f.holdDeadline = now.Add(e.lim.pendingTimeout)
		e.holdQ = append(e.holdQ, f)
		e.valDue = true
		e.kickWorker()
	}
	return e.enqueueUDPLocked(f, pkt), f, true
}

// newUDPLocked starts classifying a source port; at a cap no entry is made so the next
// packet tries again.
func (e *engine) newUDPLocked(port uint16, dst netip.AddrPort, pkt []byte, now time.Time, streak uint8) (action, *flow, bool) {
	if !e.gateOpen() || e.pending >= e.lim.pendingFlows || e.pendingCost+engineBufSize > e.lim.pendingBytes {
		return actTunnel, nil, false
	}
	f := &flow{proto: protoUDP, key: flowKey{srcPort: port, dst: dst}, state: statePending, deadline: now.Add(e.lim.pendingTimeout), failStreak: streak, lastSeen: now}
	e.udp[port] = f
	e.pending++
	e.pendingQ = append(e.pendingQ, f)
	act := e.enqueueUDPLocked(f, pkt)
	e.kickWorker()
	return act, f, true
}

func (e *engine) enqueueUDPLocked(f *flow, pkt []byte) action {
	return e.enqueueBufLocked(f, copyPacket(pkt))
}

func (e *engine) enqueueBufLocked(f *flow, p *packetBuf) action {
	cost := p.bufCost()
	if (!f.draining && f.queueCost+cost > e.lim.udpQueueBytes) || e.pendingCost+cost > e.lim.pendingBytes {
		p.release()
		e.d.stats.udpDrops.Add(1)
		return actDrop
	}
	f.queue = append(f.queue, p)
	f.queueCost += cost
	e.pendingCost += cost
	return actHeld
}

func (e *engine) resolveUDP(f *flow, res procmatch.Result, classified *ruleSet, snapAt, now time.Time) bool {
	var q []*packetBuf
	var k sink
	resolved, drain := false, false
	e.withLock(func() {
		if e.closed || !e.currentLocked(f) {
			return
		}
		if f.state == stateTunnel && f.revalidating {
			e.revalidatedLocked(f, res, classified, snapAt, now)
			resolved = true
			return
		}
		if f.state != statePending {
			return
		}
		bypass := e.bypassVerdictLocked(res, classified)
		failSafe, egressDown := classified == nil, false
		if bypass && (len(e.udpLive) >= e.lim.udpSessions || e.udpCreating >= e.lim.udpCreating) {
			bypass, failSafe = false, true
		}
		if bypass && now.Before(e.egressDownTo) {
			bypass, failSafe, egressDown = false, true, true
		}
		resolved, drain = true, true
		e.pending--
		f.submitted = false
		f.owner = res.Owner
		f.validatedAt = snapAt
		f.draining = true
		if bypass {
			e.startSessionLocked(f, now)
			e.setFailSafeLocked(f, false, false, now)
		} else {
			f.state = stateTunnel
			e.setFailSafeLocked(f, failSafe, egressDown, now)
		}
		k = e.sinkLocked(f)
		q = e.takeQueueLocked(f)
	})
	if drain {
		e.drain(f, q, k)
	}
	return resolved
}

// revalidatedLocked applies a periodic re-check of a tunnel port: it flips to bypass only
// when the port's owner is now excluded.
func (e *engine) revalidatedLocked(f *flow, res procmatch.Result, classified *ruleSet, snapAt, now time.Time) {
	f.revalidating = false
	f.submitted = false
	if classified == nil {
		f.validatedAt = now
		return
	}
	f.owner = res.Owner
	f.validatedAt = snapAt
	if !f.draining && e.bypassVerdictLocked(res, classified) && len(e.udpLive) < e.lim.udpSessions &&
		e.udpCreating < e.lim.udpCreating && !now.Before(e.egressDownTo) {
		e.startSessionLocked(f, now)
	}
}

func (e *engine) setFailSafeLocked(f *flow, failSafe, egressDown bool, now time.Time) {
	if !failSafe {
		f.failSafe, f.egressDown, f.failStreak = false, false, 0
		return
	}
	ttl := e.lim.failSafeBase << min(f.failStreak, 8)
	if ttl > e.lim.failSafeMax || ttl <= 0 {
		ttl = e.lim.failSafeMax
	}
	if f.failStreak < 16 {
		f.failStreak++
	}
	f.failSafe, f.egressDown = true, egressDown
	f.expiresAt = now.Add(ttl)
}

// endSessionLocked detaches a port's session; the caller closes its socket outside the lock.
func (e *engine) endSessionLocked(f *flow, now time.Time) *udpSession {
	s := f.sess
	if s == nil {
		return nil
	}
	f.sess = nil
	s.cancel()
	delete(e.udpLive, f)
	e.d.stats.bypassUDP.Add(-1)
	e.endedUDP.put(flowKey{srcPort: f.key.srcPort}, 0, now.Add(e.lim.endedTTL))
	return s
}

// forgetUDPLocked removes a port entry so its next packet is classified again; packets
// held for a new destination start that classification right away.
func (e *engine) forgetUDPLocked(f *flow, now time.Time) {
	port := f.key.srcPort
	if e.udp[port] == f {
		delete(e.udp, port)
	}
	if f.state == statePending {
		e.pending--
	}
	f.holding = false
	q := e.takeQueueLocked(f)
	if len(q) == 0 {
		return
	}
	if e.closed || e.pending >= e.lim.pendingFlows {
		for _, p := range q {
			p.release()
			e.d.stats.udpDrops.Add(1)
		}
		return
	}
	to := f.key.dst
	if info, ok := parseIPv4(q[0].data); ok && info.ports {
		to = netip.AddrPortFrom(info.dst, info.dstPort)
	}
	nf := &flow{proto: protoUDP, key: flowKey{srcPort: port, dst: to}, state: statePending, deadline: now.Add(e.lim.pendingTimeout), lastSeen: now}
	e.udp[port] = nf
	e.pending++
	e.pendingQ = append(e.pendingQ, nf)
	for _, p := range q {
		e.enqueueBufLocked(nf, p)
	}
	e.kickWorker()
}

// failSafeUDPLocked turns a port whose session ended into a short-lived tunnel entry; any
// held packets are returned for draining to the tunnel in order.
func (e *engine) failSafeUDPLocked(f *flow, now time.Time, egressDown bool) []*packetBuf {
	if !e.currentLocked(f) {
		return nil
	}
	f.state = stateTunnel
	f.holding = false
	f.revalidating = false
	e.setFailSafeLocked(f, true, egressDown, now)
	if f.draining || len(f.queue) == 0 {
		return nil
	}
	f.draining = true
	return e.takeQueueLocked(f)
}

func closeSessions(ss []*udpSession) {
	for _, s := range ss {
		if s != nil {
			s.closeConn()
		}
	}
}

// udpRulesChangedLocked closes sessions whose owner lost its exclusion and forgets tunnel
// ports whose stored owner is now excluded (or every entry when the rules are empty).
func (e *engine) udpRulesChangedLocked(rs *ruleSet, now time.Time) []*udpSession {
	var closing []*udpSession
	for port, f := range e.udp {
		switch {
		case f.sess != nil:
			if !rs.allows(f.owner.Chain) {
				e.d.stats.torn.Add(1)
				closing = append(closing, e.endSessionLocked(f, now))
				e.forgetUDPLocked(f, now)
			}
		case f.state == stateTunnel && !f.draining:
			if rs.empty() || f.failSafe || rs.allows(f.owner.Chain) {
				delete(e.udp, port)
			}
		}
	}
	return closing
}

// validateOnce re-checks every session's owner with one snapshot; it runs every
// validateEvery while sessions exist and as soon as a new destination is held.
func (e *engine) validateOnce() bool {
	cls, _, st := e.c.engines()
	var ports []*flow
	var sessions []*udpSession
	var checks []procmatch.SocketCheck
	e.withLock(func() {
		if e.closed || !e.valDue || st != engineReady || cls == nil {
			return
		}
		e.valDue = false
		ports = make([]*flow, 0, len(e.udpLive))
		sessions = make([]*udpSession, 0, len(e.udpLive))
		checks = make([]procmatch.SocketCheck, 0, len(e.udpLive))
		for f := range e.udpLive {
			ports = append(ports, f)
			sessions = append(sessions, f.sess)
			checks = append(checks, procmatch.SocketCheck{Proto: protoUDP, App: netip.AddrPortFrom(e.d.addr, f.key.srcPort), Owner: f.owner})
		}
		e.lastValidate = time.Now()
	})
	if len(checks) == 0 {
		return false
	}
	snapAt := time.Now()
	ok := e.validate(cls, checks)
	e.d.stats.revalidated.Add(uint64(len(checks)))
	now := time.Now()
	var closing []*udpSession
	var releases []drainJob
	var sinks []sink
	e.withLock(func() {
		for i, f := range ports {
			if e.closed || !e.currentLocked(f) || f.sess != sessions[i] {
				continue
			}
			if ok != nil && ok[i] {
				f.validatedAt = snapAt
				if f.holding && !f.draining {
					f.holding = false
					f.draining = true
					releases = append(releases, drainJob{f: f, q: e.takeQueueLocked(f)})
					sinks = append(sinks, e.sinkLocked(f))
				}
				continue
			}
			e.d.stats.torn.Add(1)
			closing = append(closing, e.endSessionLocked(f, now))
			e.forgetUDPLocked(f, now)
		}
	})
	closeSessions(closing)
	for i, j := range releases {
		e.drain(j.f, j.q, sinks[i])
	}
	return false
}

func (e *engine) validate(cls procmatch.Classifier, checks []procmatch.SocketCheck) (ok []bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = nil
			e.c.log.limited("validate", "split tunnel: owner re-check failed")
		}
	}()
	ok = cls.Validate(checks)
	if len(ok) != len(checks) {
		e.c.log.limited("validate-len", "split tunnel: owner re-check returned %d results for %d sockets", len(ok), len(checks))
		return nil
	}
	return ok
}

// sweepHoldsLocked fails safe for ports whose new destinations waited too long for an owner check.
func (e *engine) sweepHoldsLocked(now time.Time, closing []*udpSession, drains []drainJob) ([]*udpSession, []drainJob) {
	n := 0
	for _, f := range e.holdQ {
		if !f.holding || !e.currentLocked(f) {
			continue
		}
		if !now.After(f.holdDeadline) {
			e.holdQ[n] = f
			n++
			continue
		}
		e.d.stats.pendingTimeouts.Add(1)
		closing, drains = e.retireToTunnelLocked(f, now, closing, drains)
	}
	clear(e.holdQ[n:])
	e.holdQ = e.holdQ[:n]
	return closing, drains
}

func (e *engine) retireToTunnelLocked(f *flow, now time.Time, closing []*udpSession, drains []drainJob) ([]*udpSession, []drainJob) {
	if f.sess != nil {
		e.d.stats.torn.Add(1)
		closing = append(closing, e.endSessionLocked(f, now))
	}
	if q := e.failSafeUDPLocked(f, now, false); q != nil {
		drains = append(drains, drainJob{f: f, q: q})
	}
	return closing, drains
}

// scanUDPLocked ages idle sessions, peers and tunnel ports, and fails safe for sessions
// whose owner has not been confirmed for validateStale (a stuck classifier).
func (e *engine) scanUDPLocked(now time.Time, closing []*udpSession, drains []drainJob) ([]*udpSession, []drainJob) {
	for port, f := range e.udp {
		if f.draining {
			continue
		}
		switch {
		case f.sess != nil:
			s := f.sess
			switch {
			case now.Sub(s.lastOut()) > e.lim.udpIdle:
				closing = append(closing, e.endSessionLocked(f, now))
				e.forgetUDPLocked(f, now)
			case now.Sub(f.validatedAt) > e.lim.validateStale:
				closing, drains = e.retireToTunnelLocked(f, now, closing, drains)
			default:
				s.prune(now.Add(-e.lim.udpIdle))
			}
		case f.state == stateTunnel && now.Sub(f.lastSeen) > e.lim.udpIdle:
			delete(e.udp, port)
		}
	}
	return closing, drains
}

// toSession hands one queued packet of a bypass port to its session: fragments go
// through gVisor reassembly, whole datagrams straight to the send queue.
func (e *engine) toSession(s *udpSession, p *packetBuf, now time.Time) {
	info, ok := parseIPv4(p.data)
	if !ok || info.proto != protoUDP {
		p.release()
		e.d.stats.udpDrops.Add(1)
		return
	}
	if info.frag {
		if info.ports {
			s.admit(info.dst, now)
		}
		e.link.deliver(p.data)
		p.release()
		return
	}
	lo, hi, ok := info.udpPayload()
	if !ok {
		p.release()
		e.d.stats.udpDrops.Add(1)
		return
	}
	p.data = p.data[:hi]
	s.send(udpOut{dst: netip.AddrPortFrom(info.dst, info.dstPort), p: p, off: lo}, now)
}

// handleUDP receives datagrams gVisor reassembled from fragments of bypass ports: the
// local side of id is the original destination, the remote side the app.
func (e *engine) handleUDP(id stack.TransportEndpointID, pkt *stack.PacketBuffer) bool {
	drop := func() bool {
		e.d.stats.udpDrops.Add(1)
		return true
	}
	if netip.AddrFrom4(id.RemoteAddress.As4()) != e.d.addr {
		return drop()
	}
	hdr := header.UDP(pkt.TransportHeader().Slice())
	if len(hdr) < header.UDPMinimumSize {
		return drop()
	}
	nh := pkt.Network()
	lengthOK, csumOK := header.UDPValid(hdr, func() uint16 { return pkt.Data().Checksum() }, uint16(pkt.Data().Size()),
		ipv4.ProtocolNumber, nh.SourceAddress(), nh.DestinationAddress(), false)
	if !lengthOK || !csumOK {
		return drop()
	}
	s := e.bypassSession(id.RemotePort)
	if s == nil {
		return drop()
	}
	dst := netip.AddrPortFrom(netip.AddrFrom4(id.LocalAddress.As4()), id.LocalPort)
	s.send(udpOut{dst: dst, p: &packetBuf{data: pkt.Data().AsRange().ToSlice()}}, time.Now())
	return true
}

func (e *engine) bypassSession(port uint16) *udpSession {
	e.mu.Lock()
	defer e.mu.Unlock()
	if f := e.udp[port]; !e.closed && f != nil && f.state == stateBypass {
		return f.sess
	}
	return nil
}
