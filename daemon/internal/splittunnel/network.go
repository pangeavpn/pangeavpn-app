package splittunnel

import (
	"net"
	"net/netip"
	"time"
)

// NetworkChanged retries a failed egress permit or engine start and re-resolves the physical
// interface, which is a no-op unless its identity really changed to a usable one.
func (c *Controller) NetworkChanged() {
	if c.permitFailed.Load() {
		c.kickPermit()
	}
	c.retryEngines()
	_ = c.refreshNetwork()
}

// refreshNetwork acts only on a confirmed change to a usable interface; a change to "no
// interface" keeps every flow, whose sends then fail until one returns.
func (c *Controller) refreshNetwork() error {
	c.mu.Lock()
	dl, closed, upAddrs := c.dialer, c.closed, c.upAddrs
	c.mu.Unlock()
	if dl == nil || closed {
		return nil
	}
	c.netMu.Lock()
	defer c.netMu.Unlock()
	_, cur, changed, err := dl.Refresh()
	if err != nil || !changed || (cur.Index == 0 && cur.Name == "") {
		return err
	}
	gen := c.netGen.Add(1)
	c.mu.Lock()
	devs := c.deviceListLocked()
	c.mu.Unlock()
	repinned, reset := 0, 0
	for _, d := range devs {
		if e := d.eng.Load(); e != nil {
			e.safely(func() bool {
				r, t := e.networkChanged(gen, upAddrs)
				repinned += r
				reset += t
				return false
			})
		}
	}
	c.log.limited("netchange", "split tunnel: physical interface changed (%d UDP sockets re-pinned, %d TCP connections reset)", repinned, reset)
	return nil
}

// networkHint is called when a bypass socket reports its route gone; it re-resolves the
// interface at most once per unreachableEvery.
func (c *Controller) networkHint() {
	now := time.Now().UnixNano()
	last := c.lastHint.Load()
	if now-last < int64(c.lim.unreachableEvery) || !c.lastHint.CompareAndSwap(last, now) {
		return
	}
	go func() { _ = c.refreshNetwork() }()
}

type splice struct {
	f    *flow
	phys net.Conn
}

func (e *engine) networkChanged(gen uint64, upAddrs func() (map[netip.Addr]struct{}, error)) (repinned, reset int) {
	now := time.Now()
	var sessions []*udpSession
	var splices []splice
	closed := false
	e.withLock(func() {
		if closed = e.closed; closed {
			return
		}
		e.egressDownTo = time.Time{}
		for port, f := range e.udp {
			switch {
			case f.sess != nil:
				sessions = append(sessions, f.sess)
			case f.state == stateTunnel && !f.draining && (f.failSafe || f.egressDown):
				delete(e.udp, port)
			}
		}
		for _, f := range e.tcp {
			if f.state == stateBypass && !f.aborted && f.phys != nil {
				splices = append(splices, splice{f: f, phys: f.phys})
			}
		}
	})
	if closed {
		return 0, 0
	}

	var failed []*udpSession
	for _, s := range sessions {
		if err := s.repin(gen); err != nil {
			failed = append(failed, s)
			continue
		}
		repinned++
	}
	if len(failed) > 0 {
		e.withLock(func() {
			for _, s := range failed {
				if f := s.f; !e.closed && e.currentLocked(f) && f.sess == s {
					e.endSessionLocked(f, now)
					e.forgetUDPLocked(f, now)
				}
			}
		})
		closeSessions(failed)
		e.c.log.limited("repin", "split tunnel: %d UDP sockets could not follow the network change", len(failed))
	}

	if len(splices) == 0 || upAddrs == nil {
		return repinned, 0
	}
	addrs, err := upAddrs()
	if err != nil {
		return repinned, 0
	}
	var targets []abortTarget
	e.withLock(func() {
		for _, sp := range splices {
			local, ok := connLocalAddr(sp.phys)
			if !ok {
				continue
			}
			if _, up := addrs[local]; up {
				continue
			}
			if f := sp.f; e.tcp[f.key] == f && f.state == stateBypass && !f.aborted && f.phys == sp.phys {
				targets = append(targets, e.abortLocked(f, now))
			}
		}
	})
	runTargets(targets)
	return repinned, len(targets)
}

func connLocalAddr(c net.Conn) (netip.Addr, bool) {
	ta, ok := c.LocalAddr().(*net.TCPAddr)
	if !ok {
		return netip.Addr{}, false
	}
	a := ta.AddrPort().Addr().Unmap()
	return a, a.IsValid()
}

// interfaceAddrs lists the addresses of every interface that is up.
func interfaceAddrs() (map[netip.Addr]struct{}, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	out := make(map[netip.Addr]struct{})
	for _, ifc := range ifs {
		if ifc.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok {
				if ip, ok := netip.AddrFromSlice(n.IP); ok {
					out[ip.Unmap()] = struct{}{}
				}
			}
		}
	}
	return out, nil
}
