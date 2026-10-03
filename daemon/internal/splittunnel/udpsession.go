package splittunnel

import (
	"container/list"
	"context"
	"errors"
	"net"
	"net/netip"
	"os"
	"sync"
	"syscall"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

const udpReadBufSize = 64 << 10

var udpReadPool = sync.Pool{New: func() any {
	b := make([]byte, udpReadBufSize)
	return &b
}}

// udpOut is one datagram for the session's socket: the payload is p.data[off:].
type udpOut struct {
	dst netip.AddrPort
	p   *packetBuf
	off int
}

type peer struct {
	addr netip.Addr
	last time.Time
}

// udpSession is the NAT state of one bypassed app source port: one off-tunnel socket
// (endpoint-independent mapping) that accepts replies only from addresses the app sent to.
type udpSession struct {
	e      *engine
	f      *flow
	port   uint16
	ctx    context.Context
	cancel context.CancelFunc
	sendQ  chan udpOut

	mu       sync.Mutex
	conn     net.PacketConn
	gen      uint64
	peers    map[netip.Addr]*list.Element
	lru      list.List
	lastSent time.Time
}

// startSessionLocked creates the session struct and flips f to bypass. It makes no syscalls:
// the session goroutine opens the socket, so packets wait in sendQ meanwhile.
func (e *engine) startSessionLocked(f *flow, now time.Time) {
	ctx, cancel := context.WithCancel(e.d.ctx)
	s := &udpSession{
		e:        e,
		f:        f,
		port:     f.key.srcPort,
		ctx:      ctx,
		cancel:   cancel,
		sendQ:    make(chan udpOut, e.lim.udpSendQ),
		peers:    make(map[netip.Addr]*list.Element),
		lastSent: now,
	}
	f.sess = s
	f.state = stateBypass
	f.holding = false
	e.udpLive[f] = struct{}{}
	e.udpCreating++
	e.d.stats.bypassUDP.Add(1)
	e.d.stats.bypassed.Add(1)
	go s.run(e.c.netGen.Load())
}

func (s *udpSession) run(gen uint64) {
	e := s.e
	conn, err := s.listen()
	e.listenDone(s, err)
	if err != nil {
		s.discard()
		return
	}
	if !s.setConn(conn, gen) {
		_ = conn.Close()
		s.discard()
		return
	}
	if cur := e.c.netGen.Load(); cur != gen {
		if err := s.repin(cur); err != nil {
			e.sessionFailed(s, err)
			s.discard()
			return
		}
	}
	go s.read(conn)
	defer conn.Close()
	for {
		select {
		case <-s.ctx.Done():
			s.discard()
			return
		case o := <-s.sendQ:
			s.write(conn, o)
		}
	}
}

func (s *udpSession) listen() (net.PacketConn, error) {
	dl := s.e.c.egressDialer()
	if dl == nil {
		return nil, egress.ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(s.ctx, s.e.lim.listenTimeout)
	defer cancel()
	conn, err := dl.ListenUDP(ctx)
	if err == nil && conn == nil {
		err = egress.ErrUnsupported
	}
	if err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		_ = conn.Close()
		return nil, ctx.Err()
	}
	return conn, nil
}

// listenDone releases the creation slot; a failed socket turns the port into a fail-safe
// tunnel entry and keeps new UDP bypass verdicts in the tunnel for egressBackoff.
func (e *engine) listenDone(s *udpSession, err error) {
	now := time.Now()
	abandoned := s.ctx.Err() != nil
	var drain []*packetBuf
	f := s.f
	e.withLock(func() {
		e.udpCreating--
		if err != nil && !e.closed && e.currentLocked(f) && f.sess == s {
			e.egressDownTo = now.Add(e.lim.egressBackoff)
			e.endSessionLocked(f, now)
			drain = e.failSafeUDPLocked(f, now, true)
		}
	})
	if err != nil && !abandoned {
		e.c.log.limited("udp-listen", "split tunnel: off-tunnel UDP socket failed: %s", errClass(err))
		if egress.IsUnreachable(err) {
			e.c.networkHint()
		}
	}
	if drain != nil {
		e.drain(f, drain, sink{})
	}
}

func (s *udpSession) setConn(c net.PacketConn, gen uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ctx.Err() != nil {
		return false
	}
	s.conn, s.gen = c, gen
	return true
}

// closeConn unblocks the session's socket I/O; the goroutines then exit.
func (s *udpSession) closeConn() {
	s.cancel()
	s.mu.Lock()
	c := s.conn
	s.mu.Unlock()
	if c != nil {
		_ = c.Close()
	}
}

func (s *udpSession) discard() {
	for {
		select {
		case o := <-s.sendQ:
			o.p.release()
			s.e.d.stats.udpDrops.Add(1)
		default:
			return
		}
	}
}

// repin re-applies the egress interface pin to the live socket after a network change.
func (s *udpSession) repin(gen uint64) error {
	s.mu.Lock()
	c := s.conn
	if c == nil || s.gen == gen {
		s.mu.Unlock()
		return nil
	}
	s.gen = gen
	s.mu.Unlock()
	dl := s.e.c.egressDialer()
	sc, ok := c.(syscall.Conn)
	if dl == nil || !ok {
		return egress.ErrUnsupported
	}
	return dl.Repin(sc)
}

// send queues one datagram without blocking and admits its destination as a peer.
func (s *udpSession) send(o udpOut, now time.Time) bool {
	if s.ctx.Err() != nil {
		o.p.release()
		s.e.d.stats.udpDrops.Add(1)
		return false
	}
	s.admit(o.dst.Addr(), now)
	select {
	case s.sendQ <- o:
		return true
	default:
		o.p.release()
		s.e.d.stats.udpDrops.Add(1)
		return false
	}
}

func (s *udpSession) write(conn net.PacketConn, o udpOut) {
	payload := o.p.data[o.off:]
	var err error
	if uc, ok := conn.(*net.UDPConn); ok {
		_, err = uc.WriteToUDPAddrPort(payload, o.dst)
	} else {
		_, err = conn.WriteTo(payload, net.UDPAddrFromAddrPort(o.dst))
	}
	o.p.release()
	if err == nil {
		s.sent(o.dst.Addr(), time.Now())
		return
	}
	if s.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
		return
	}
	e := s.e
	e.d.stats.udpDrops.Add(1)
	if egress.IsUnreachable(err) {
		e.c.networkHint()
	}
	e.c.log.limited("udp-send", "split tunnel: off-tunnel UDP send failed: %s", errClass(err))
}

func (s *udpSession) read(conn net.PacketConn) {
	bp := udpReadPool.Get().(*[]byte)
	defer udpReadPool.Put(bp)
	buf := *bp
	uc, _ := conn.(*net.UDPConn)
	for {
		var (
			n    int
			from netip.AddrPort
			err  error
		)
		if uc != nil {
			n, from, err = uc.ReadFromUDPAddrPort(buf)
		} else {
			var a net.Addr
			n, a, err = conn.ReadFrom(buf)
			if ua, ok := a.(*net.UDPAddr); ok {
				from = ua.AddrPort()
			}
		}
		if err != nil {
			if s.ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			if errors.Is(err, os.ErrDeadlineExceeded) {
				continue
			}
			s.e.sessionFailed(s, err)
			return
		}
		from = netip.AddrPortFrom(from.Addr().Unmap(), from.Port())
		if !from.Addr().Is4() || !s.knows(from.Addr()) {
			continue
		}
		s.e.injectReply(s, from, buf[:n])
	}
}

// sessionFailed closes a session whose socket stopped working; its port re-classifies.
func (e *engine) sessionFailed(s *udpSession, err error) {
	now := time.Now()
	e.withLock(func() {
		if f := s.f; !e.closed && e.currentLocked(f) && f.sess == s {
			e.endSessionLocked(f, now)
			e.forgetUDPLocked(f, now)
		}
	})
	s.closeConn()
	e.c.log.limited("udp-read", "split tunnel: off-tunnel UDP socket closed: %s", errClass(err))
}

// injectReply writes a reply from an allowed peer to the app as IPv4 from the peer's address.
func (e *engine) injectReply(s *udpSession, from netip.AddrPort, payload []byte) {
	if s.ctx.Err() != nil {
		return
	}
	wb := writeBatchPool.Get().(*writeBatch)
	var ok bool
	wb.bufs, ok = buildUDPReply(wb.bufs, from, netip.AddrPortFrom(e.d.addr, s.port), payload, int(e.d.mtu.Load()), e.nextID)
	if ok {
		for _, p := range wb.bufs {
			wb.raw = append(wb.raw, p.data)
		}
		_ = e.d.writeInjected(wb.raw)
	} else {
		e.d.stats.udpDrops.Add(1)
	}
	for _, p := range wb.bufs {
		p.release()
	}
	clear(wb.bufs)
	clear(wb.raw)
	wb.bufs, wb.raw = wb.bufs[:0], wb.raw[:0]
	writeBatchPool.Put(wb)
}

func (e *engine) nextID() uint16 { return uint16(e.ipID.Add(1)) }

func (s *udpSession) knows(a netip.Addr) bool {
	s.mu.Lock()
	_, ok := s.peers[a]
	s.mu.Unlock()
	return ok
}

// admit records a destination the app may send to and receive from (address-dependent
// filtering); the oldest peer is evicted at the cap.
func (s *udpSession) admit(a netip.Addr, now time.Time) {
	s.mu.Lock()
	if _, ok := s.peers[a]; !ok {
		s.insertLocked(a, now)
	}
	s.mu.Unlock()
}

// sent refreshes the session and the peer; only successful sends keep them alive.
func (s *udpSession) sent(a netip.Addr, now time.Time) {
	s.mu.Lock()
	s.lastSent = now
	if el := s.peers[a]; el != nil {
		el.Value.(*peer).last = now
		s.lru.MoveToBack(el)
	} else {
		s.insertLocked(a, now)
	}
	s.mu.Unlock()
}

func (s *udpSession) insertLocked(a netip.Addr, now time.Time) {
	for max := s.e.lim.udpPeers; max > 0 && s.lru.Len() >= max; {
		front := s.lru.Front()
		delete(s.peers, front.Value.(*peer).addr)
		s.lru.Remove(front)
	}
	s.peers[a] = s.lru.PushBack(&peer{addr: a, last: now})
}

func (s *udpSession) prune(before time.Time) {
	s.mu.Lock()
	for el := s.lru.Front(); el != nil; el = s.lru.Front() {
		p := el.Value.(*peer)
		if !p.last.Before(before) {
			break
		}
		delete(s.peers, p.addr)
		s.lru.Remove(el)
	}
	s.mu.Unlock()
}

func (s *udpSession) lastOut() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastSent
}

func (s *udpSession) peerCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lru.Len()
}
