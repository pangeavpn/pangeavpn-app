package splittunnel

import (
	"container/list"
	"net/netip"
	"time"
)

// fragKey names one fragmented datagram from the tunnel address; the protocol is always UDP.
type fragKey struct {
	dst netip.Addr
	id  uint16
}

type fragKind uint8

const (
	fragHeld fragKind = iota
	fragTunnel
	fragCollect
	fragDrop
)

// fragRec makes every fragment of a datagram take the path its first fragment took.
// Fragments seen before the first one wait in held; collect points at the UDP port entry.
type fragRec struct {
	key     fragKey
	kind    fragKind
	f       *flow
	queued  bool
	held    []*packetBuf
	cost    int
	expires time.Time
}

type fragTracker struct {
	m     map[fragKey]*list.Element
	l     list.List
	bytes int
}

func (t *fragTracker) init() {
	t.m = make(map[fragKey]*list.Element)
}

func (t *fragTracker) get(k fragKey) *fragRec {
	if el := t.m[k]; el != nil {
		return el.Value.(*fragRec)
	}
	return nil
}

// add inserts r, evicting the oldest record at max; the caller flushes an evicted record's held fragments.
func (t *fragTracker) add(r *fragRec, max int) (evicted *fragRec) {
	if old := t.m[r.key]; old != nil {
		evicted = old.Value.(*fragRec)
		t.l.Remove(old)
		delete(t.m, r.key)
	} else if max > 0 && t.l.Len() >= max {
		front := t.l.Front()
		evicted = front.Value.(*fragRec)
		t.l.Remove(front)
		delete(t.m, evicted.key)
	}
	t.m[r.key] = t.l.PushBack(r)
	return evicted
}

func (t *fragTracker) hold(r *fragRec, p *packetBuf) {
	r.held = append(r.held, p)
	r.cost += p.bufCost()
	t.bytes += p.bufCost()
}

func (t *fragTracker) takeHeld(r *fragRec) []*packetBuf {
	h := r.held
	t.bytes -= r.cost
	r.held, r.cost = nil, 0
	return h
}

// expired removes records past their TTL; records are added in expiry order.
func (t *fragTracker) expired(now time.Time) []*fragRec {
	var out []*fragRec
	for el := t.l.Front(); el != nil; el = t.l.Front() {
		r := el.Value.(*fragRec)
		if !now.After(r.expires) {
			break
		}
		t.l.Remove(el)
		delete(t.m, r.key)
		out = append(out, r)
	}
	return out
}

func (t *fragTracker) clear() []*packetBuf {
	var out []*packetBuf
	for el := t.l.Front(); el != nil; el = el.Next() {
		out = append(out, t.takeHeld(el.Value.(*fragRec))...)
	}
	t.l.Init()
	clear(t.m)
	return out
}

func (t *fragTracker) len() int { return t.l.Len() }

// spill carries packets decide() released besides the one it was called for; the reading
// goroutine forwards them after the flow mutex is dropped.
type spill struct {
	items []spillItem
}

type spillItem struct {
	act action
	p   *packetBuf
}

func (s *spill) add(act action, ps ...*packetBuf) {
	for _, p := range ps {
		s.items = append(s.items, spillItem{act: act, p: p})
	}
}

func (e *engine) flushSpill(s *spill) {
	for i, it := range s.items {
		switch it.act {
		case actTunnel:
			e.pushOut(it.p)
		case actStack:
			e.link.deliver(it.p.data)
			it.p.release()
		default:
			it.p.release()
		}
		s.items[i] = spillItem{}
	}
	s.items = s.items[:0]
}

func (s *spill) release() {
	for i, it := range s.items {
		it.p.release()
		s.items[i] = spillItem{}
	}
	s.items = s.items[:0]
}

// decideFrag handles an IPv4 fragment sent from the tunnel address by a UDP socket.
func (e *engine) decideFrag(info *pktInfo, pkt []byte, now time.Time, sp *spill) action {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return actTunnel
	}
	key := fragKey{dst: info.dst, id: info.id}
	rec := e.frags.get(key)
	if info.fragOff == 0 {
		return e.firstFragLocked(key, rec, info, pkt, now, sp)
	}
	if rec == nil {
		if !e.gateOpen() && e.frags.len() == 0 {
			return actTunnel
		}
		rec = &fragRec{key: key, kind: fragHeld, expires: now.Add(e.lim.fragTTL)}
		e.addFragLocked(rec, sp)
	}
	switch rec.kind {
	case fragHeld:
		p := copyPacket(pkt)
		if e.frags.bytes+p.bufCost() > e.lim.fragBytes {
			rec.kind = fragTunnel
			sp.add(actTunnel, e.frags.takeHeld(rec)...)
			sp.add(actTunnel, p)
			return actDrop
		}
		e.frags.hold(rec, p)
		return actHeld
	case fragTunnel:
		return actTunnel
	case fragDrop:
		return actDrop
	}
	return e.collectLocked(rec, pkt)
}

func (e *engine) addFragLocked(rec *fragRec, sp *spill) {
	if ev := e.frags.add(rec, e.lim.fragMax); ev != nil && len(ev.held) > 0 {
		sp.add(actTunnel, e.frags.takeHeld(ev)...)
	}
}

// firstFragLocked decides a datagram by its first fragment's 5-tuple, like an unfragmented
// packet, and sends fragments that arrived earlier the same way.
func (e *engine) firstFragLocked(key fragKey, rec *fragRec, info *pktInfo, pkt []byte, now time.Time, sp *spill) action {
	var held []*packetBuf
	if rec != nil {
		held = e.frags.takeHeld(rec)
	}
	act, f, queued := actTunnel, (*flow)(nil), false
	if info.ports && info.dstPort != 53 && info.dstPort != 853 && !alwaysTunnelDst(info.dst) && !e.d.isTunnelDNS(info.dst) {
		act, f, queued = e.udpLocked(info, pkt, now, true)
	}
	kind := fragTunnel
	switch {
	case act == actDrop:
		kind = fragDrop
	case f != nil:
		kind = fragCollect
	}
	if rec == nil {
		if kind == fragTunnel && !e.gateOpen() && e.frags.len() == 0 {
			return act
		}
		rec = &fragRec{key: key, expires: now.Add(e.lim.fragTTL)}
		e.addFragLocked(rec, sp)
	}
	rec.kind, rec.f, rec.queued = kind, f, queued
	for _, p := range held {
		switch {
		case kind == fragTunnel:
			sp.add(actTunnel, p)
		case kind == fragCollect && queued:
			e.enqueueBufLocked(f, p)
		case kind == fragCollect:
			sp.add(actStack, p)
		default:
			p.release()
		}
	}
	return act
}

// collectLocked routes a later fragment of a datagram whose first fragment went to a UDP port entry.
func (e *engine) collectLocked(rec *fragRec, pkt []byte) action {
	f := rec.f
	if f == nil || !e.currentLocked(f) {
		return actDrop
	}
	switch {
	case f.state == statePending || f.draining || (rec.queued && f.holding):
		return e.enqueueUDPLocked(f, pkt)
	case f.state == stateBypass && f.sess != nil:
		return actStack
	case f.state == stateTunnel:
		return actTunnel
	}
	return actDrop
}
