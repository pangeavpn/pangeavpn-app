package splittunnel

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

type deviceStats struct {
	classified      atomic.Uint64
	bypassed        atomic.Uint64
	lookupFails     atomic.Uint64
	pendingTimeouts atomic.Uint64
	udpDrops        atomic.Uint64
	outDrops        atomic.Uint64
	revalidated     atomic.Uint64
	torn            atomic.Uint64
	tooMany         atomic.Uint64
	bypassTCP       atomic.Int64
	bypassUDP       atomic.Int64
}

func (s *deviceStats) snapshot() Counters {
	return Counters{
		Classified:      s.classified.Load(),
		Bypassed:        s.bypassed.Load(),
		LookupFails:     s.lookupFails.Load(),
		PendingTimeouts: s.pendingTimeouts.Load(),
		UDPDrops:        s.udpDrops.Load(),
		OutDrops:        s.outDrops.Load(),
		Revalidated:     s.revalidated.Load(),
		Torn:            s.torn.Load(),
	}
}

// Device wraps the OS TUN. Until app rules first appear it is a pass-through; after that a
// pump owns inner.Read and Read serves copies of tunnel-bound packets.
type Device struct {
	c     *Controller
	inner tun.Device
	addr  netip.Addr

	dns atomic.Pointer[[]netip.Addr]
	mtu atomic.Int32

	rules       atomic.Pointer[ruleSet]
	wantPump    atomic.Bool
	pumping     atomic.Bool
	tunnelOnly  atomic.Bool
	stackFailed atomic.Bool
	closed      atomic.Bool

	mu        sync.Mutex
	eng       atomic.Pointer[engine]
	done      chan struct{}
	closeOnce sync.Once

	term     chan struct{}
	termOnce sync.Once
	termErr  error

	writeMu      sync.Mutex
	writesClosed bool

	ctx    context.Context
	cancel context.CancelFunc

	stats     deviceStats
	faultOnce sync.Once
}

func newDevice(c *Controller, inner tun.Device, info wg.TunnelInfo, addr netip.Addr) *Device {
	ctx, cancel := context.WithCancel(context.Background())
	d := &Device{
		c:      c,
		inner:  inner,
		addr:   addr,
		done:   make(chan struct{}),
		term:   make(chan struct{}),
		ctx:    ctx,
		cancel: cancel,
	}
	d.setDNS(info.DNS)
	mtu := info.MTU
	if mtu <= 0 {
		if m, err := inner.MTU(); err == nil {
			mtu = m
		}
	}
	if mtu <= 0 {
		mtu = 1280
	}
	d.mtu.Store(int32(mtu))
	return d
}

func firstIPv4(addrs []netip.Addr) netip.Addr {
	for _, a := range addrs {
		if a = a.Unmap(); a.Is4() && !a.IsUnspecified() {
			return a
		}
	}
	return netip.Addr{}
}

func (d *Device) setDNS(dns []netip.Addr) {
	out := make([]netip.Addr, 0, len(dns))
	for _, a := range dns {
		if a = a.Unmap(); a.Is4() && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	d.dns.Store(&out)
}

func (d *Device) isTunnelDNS(a netip.Addr) bool {
	if p := d.dns.Load(); p != nil {
		return slices.Contains(*p, a)
	}
	return false
}

func (d *Device) File() *os.File           { return d.inner.File() }
func (d *Device) MTU() (int, error)        { return d.inner.MTU() }
func (d *Device) Name() (string, error)    { return d.inner.Name() }
func (d *Device) Events() <-chan tun.Event { return d.inner.Events() }
func (d *Device) BatchSize() int           { return d.inner.BatchSize() }

// TunnelAddr is the IPv4 source the engine expects on app packets; it never changes for a Device.
func (d *Device) TunnelAddr() netip.Addr { return d.addr }

// UpdateTunnelInfo applies DNS and MTU after an in-place reconfigure; addresses are fixed.
func (d *Device) UpdateTunnelInfo(info wg.TunnelInfo) {
	d.setDNS(info.DNS)
	if info.MTU > 0 {
		d.setMTU(info.MTU)
	}
}

func (d *Device) LUID() uint64 {
	if l, ok := d.inner.(interface{ LUID() uint64 }); ok {
		return l.LUID()
	}
	return 0
}

// ForceMTU calls the inner device before touching engine state, holding no lock, so a
// panic from a concurrently closed inner device still reaches the caller's recover.
func (d *Device) ForceMTU(mtu int) {
	if f, ok := d.inner.(interface{ ForceMTU(int) }); ok {
		f.ForceMTU(mtu)
	}
	d.setMTU(mtu)
}

func (d *Device) setMTU(mtu int) {
	if mtu <= 0 {
		return
	}
	d.mtu.Store(int32(mtu))
	if e := d.eng.Load(); e != nil {
		e.link.SetMTU(uint32(mtu))
	}
}

func (d *Device) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if d.closed.Load() {
		return 0, os.ErrClosed
	}
	if d.pumping.Load() {
		return d.readPump(bufs, sizes, offset)
	}
	n, err := d.inner.Read(bufs, sizes, offset)
	if n > 0 && d.wantPump.Load() && !d.tunnelOnly.Load() && (err == nil || errors.Is(err, tun.ErrTooManySegments)) {
		d.handover(bufs, sizes, offset, n)
	}
	return n, err
}

// handover runs on the caller's goroutine right after its inner.Read, so exactly one
// goroutine ever reads the inner device; diverted packets are copied out and zero-sized.
func (d *Device) handover(bufs [][]byte, sizes []int, offset, n int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed.Load() || d.pumping.Load() {
		return
	}
	e, err := newEngine(d, len(bufs[0]), offset)
	if err != nil {
		d.stackFailed.Store(true)
		d.tunnelOnly.Store(true)
		d.c.log.printf("split tunnel: packet engine unavailable, device stays tunnel-only: %v", err)
		return
	}
	d.eng.Store(e)
	now := time.Now()
	for i := 0; i < n && i < len(sizes); i++ {
		if s := sizes[i]; s > 0 && offset+s <= len(bufs[i]) && e.routeSafely(bufs[i][offset:offset+s], now, true) {
			sizes[i] = 0
		}
	}
	d.pumping.Store(true)
	e.start()
	d.c.ensureEngines()
	d.c.log.printf("split tunnel: packet engine started")
}

func (d *Device) readPump(bufs [][]byte, sizes []int, offset int) (int, error) {
	if len(bufs) == 0 {
		return 0, nil
	}
	e := d.eng.Load()
	var p *packetBuf
	select {
	case <-d.done:
		return 0, os.ErrClosed
	case p = <-e.out:
	default:
		select {
		case p = <-e.out:
		case <-d.done:
			return 0, os.ErrClosed
		case <-d.term:
			select {
			case p = <-e.out:
			default:
				return 0, d.termErr
			}
		}
	}
	n := 0
	for {
		if len(p.data) <= len(bufs[n])-offset {
			copy(bufs[n][offset:], p.data)
			sizes[n] = len(p.data)
			n++
		} else {
			d.stats.outDrops.Add(1)
		}
		p.release()
		if n == len(bufs) {
			return n, nil
		}
		select {
		case p = <-e.out:
		default:
			return n, nil
		}
	}
}

func (d *Device) Write(bufs [][]byte, offset int) (int, error) {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.writesClosed {
		return 0, os.ErrClosed
	}
	return d.inner.Write(bufs, offset)
}

// writeInjected sends engine-built packets; each buffer carries writeHeadroom spare bytes in front.
func (d *Device) writeInjected(bufs [][]byte) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()
	if d.writesClosed {
		return os.ErrClosed
	}
	_, err := d.inner.Write(bufs, writeHeadroom)
	return err
}

func (d *Device) setTerm(err error) {
	d.termOnce.Do(func() {
		d.termErr = err
		close(d.term)
	})
}

// Close never waits on goroutines blocked in I/O and never drains out: it returns within
// inner.Close plus engine teardown.
func (d *Device) Close() error {
	var err error
	d.closeOnce.Do(func() {
		d.closed.Store(true)
		close(d.done)
		d.writeMu.Lock()
		d.writesClosed = true
		d.writeMu.Unlock()
		err = d.inner.Close()
		d.cancel()
		d.mu.Lock()
		e := d.eng.Load()
		d.mu.Unlock()
		if e != nil {
			e.shutdown()
		}
		d.c.deregister(d)
		if e != nil {
			s := d.stats.snapshot()
			d.c.log.printf("split tunnel: engine stopped (classified=%d bypassed=%d lookupFails=%d pendingTimeouts=%d udpDrops=%d outDrops=%d revalidated=%d torn=%d)",
				s.Classified, s.Bypassed, s.LookupFails, s.PendingTimeouts, s.UDPDrops, s.OutDrops, s.Revalidated, s.Torn)
		}
	})
	return err
}

func (d *Device) applyRules(rs *ruleSet) {
	d.rules.Store(rs)
	if !rs.empty() {
		d.wantPump.Store(true)
	}
	if e := d.eng.Load(); e != nil {
		e.safely(func() bool { e.rulesChanged(rs); return false })
	}
}

func (d *Device) resetEngine() {
	if e := d.eng.Load(); e != nil {
		e.safely(func() bool { e.failSafeAll(); return false })
	}
}

func (d *Device) kickWorker() {
	if e := d.eng.Load(); e != nil {
		e.kickWorker()
	}
}

// fault turns a recovered engine panic into tunnel-only operation for this device.
func (d *Device) fault(r any) {
	d.tunnelOnly.Store(true)
	d.faultOnce.Do(func() {
		d.c.log.printf("split tunnel: engine fault, device switched to tunnel-only: %v", r)
		if e := d.eng.Load(); e != nil {
			go e.safely(func() bool { e.failSafeAll(); return false })
		}
	})
}
