package splittunnel

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/sagernet/gvisor/pkg/buffer"
	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/adapters/gonet"
	"github.com/sagernet/gvisor/pkg/tcpip/header"
	"github.com/sagernet/gvisor/pkg/tcpip/network/ipv4"
	"github.com/sagernet/gvisor/pkg/tcpip/stack"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/tcp"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/udp"
	"github.com/sagernet/gvisor/pkg/waiter"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

var (
	tunAddr = netip.MustParseAddr("10.7.0.2")
	tunDNS  = netip.MustParseAddr("10.7.0.1")
	remoteA = netip.MustParseAddr("198.51.100.7")
	remoteB = netip.MustParseAddr("203.0.113.9")
)

const (
	wgOffset = 16
	testMTU  = 1420
	appGame  = "/apps/game/game"
	appChat  = "/apps/chat/chat"
)

type tunKind int

const (
	kindWindows tunKind = iota
	kindLinux
	kindDarwin
)

func (k tunKind) String() string {
	return [...]string{"windows", "linux", "darwin"}[k]
}

var allKinds = []tunKind{kindWindows, kindLinux, kindDarwin}

// errInnerGone is what the fake TUN's Read returns when a nil packet is queued.
var errInnerGone = errors.New("tun device gone")

// fakeTUN checks the per-OS wireguard-go TUN contracts the wrapper must honour.
type fakeTUN struct {
	kind      tunKind
	mtu       int
	in        chan []byte
	toOS      chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	events    chan tun.Event
	readers   atomic.Int32
	reads     atomic.Int64
	tooMany   atomic.Int64
	forced    atomic.Int64
	failNext  atomic.Bool
	blackhole atomic.Bool

	mu         sync.Mutex
	violations []string
	written    [][]byte
}

func newFakeTUN(kind tunKind) *fakeTUN {
	return &fakeTUN{
		kind:   kind,
		mtu:    testMTU,
		in:     make(chan []byte, 4096),
		toOS:   make(chan []byte, 4096),
		closed: make(chan struct{}),
		events: make(chan tun.Event, 1),
	}
}

func (f *fakeTUN) violate(format string, args ...any) {
	f.mu.Lock()
	f.violations = append(f.violations, fmt.Sprintf(format, args...))
	f.mu.Unlock()
}

func (f *fakeTUN) violationList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.violations)
}

// fromOS is what the app stack sends into the tunnel interface.
func (f *fakeTUN) fromOS(p []byte) {
	select {
	case f.in <- p:
	default:
	}
}

func (f *fakeTUN) File() *os.File           { return nil }
func (f *fakeTUN) MTU() (int, error)        { return f.mtu, nil }
func (f *fakeTUN) Name() (string, error)    { return "fake0", nil }
func (f *fakeTUN) Events() <-chan tun.Event { return f.events }

func (f *fakeTUN) BatchSize() int {
	if f.kind == kindLinux {
		return 128
	}
	return 1
}

func (f *fakeTUN) Close() error {
	f.closeOnce.Do(func() {
		close(f.closed)
		close(f.events)
	})
	return nil
}

func (f *fakeTUN) Read(bufs [][]byte, sizes []int, offset int) (int, error) {
	if f.readers.Add(1) > 1 {
		f.violate("concurrent inner Read")
	}
	defer f.readers.Add(-1)
	f.reads.Add(1)
	if len(sizes) < len(bufs) || len(bufs) == 0 {
		f.violate("Read with %d bufs and %d sizes", len(bufs), len(sizes))
		return 0, errors.New("bad read args")
	}
	switch {
	case f.kind == kindLinux && offset < 10:
		f.violate("linux Read offset %d < virtio header", offset)
		return 0, errors.New("offset too small")
	case f.kind == kindDarwin && offset < 4:
		f.violate("darwin Read offset %d < AF header", offset)
		return 0, errors.New("offset too small")
	}
	if f.failNext.Swap(false) {
		return 0, errInnerGone
	}
	var first []byte
	select {
	case first = <-f.in:
	case <-f.closed:
		return 0, os.ErrClosed
	}
	if first == nil {
		return 0, errInnerGone
	}
	n := 0
	put := func(p []byte) error {
		if len(p) > len(bufs[n])-offset {
			f.violate("read len %d overflows buffer %d at offset %d", len(p), len(bufs[n]), offset)
			return fmt.Errorf("read len %d overflows", len(p))
		}
		if f.kind == kindDarwin {
			copy(bufs[n][offset-4:], []byte{0, 0, 0, 2})
		}
		copy(bufs[n][offset:], p)
		sizes[n] = len(p)
		n++
		return nil
	}
	if err := put(first); err != nil {
		return 0, err
	}
	if f.kind != kindLinux {
		return n, nil
	}
	for n < len(bufs) {
		select {
		case p := <-f.in:
			if p == nil {
				f.failNext.Store(true)
				return n, nil
			}
			if err := put(p); err != nil {
				return n, err
			}
			continue
		default:
		}
		break
	}
	if f.tooMany.Add(1)%5 == 0 {
		return n, tun.ErrTooManySegments
	}
	return n, nil
}

func (f *fakeTUN) Write(bufs [][]byte, offset int) (int, error) {
	select {
	case <-f.closed:
		return 0, os.ErrClosed
	default:
	}
	switch {
	case f.kind == kindLinux && offset < 10:
		f.violate("linux Write offset %d < virtio header", offset)
		return 0, errors.New("offset too small")
	case f.kind == kindDarwin && offset < 4:
		f.violate("darwin Write offset %d < AF header", offset)
		return 0, errors.New("offset too small")
	}
	for _, b := range bufs {
		if len(b) <= offset {
			f.violate("Write of empty packet")
			continue
		}
		switch f.kind {
		case kindLinux:
			clear(b[offset-10 : offset])
		case kindDarwin:
			copy(b[offset-4:], []byte{0, 0, 0, 2})
		}
		p := slices.Clone(b[offset:])
		f.mu.Lock()
		if len(f.written) < 4096 {
			f.written = append(f.written, p)
		}
		f.mu.Unlock()
		if f.blackhole.Load() {
			continue
		}
		select {
		case f.toOS <- p:
		default:
		}
	}
	return len(bufs), nil
}

func (f *fakeTUN) writtenPackets() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.written)
}

// winFakeTUN adds the NativeTun methods the daemon type-asserts on Windows.
type winFakeTUN struct {
	*fakeTUN
	panicOnForce atomic.Bool
}

func (w *winFakeTUN) LUID() uint64 { return 0xfeed }

func (w *winFakeTUN) ForceMTU(mtu int) {
	if w.panicOnForce.Load() {
		panic("send on closed channel")
	}
	w.forced.Store(int64(mtu))
}

// pipeLink is a plain gVisor link endpoint whose outbound packets go to send.
type pipeLink struct {
	mtu  uint32
	send func([]byte)

	mu   sync.RWMutex
	disp stack.NetworkDispatcher
}

func (l *pipeLink) MTU() uint32                                  { return l.mtu }
func (l *pipeLink) SetMTU(mtu uint32)                            { l.mtu = mtu }
func (l *pipeLink) MaxHeaderLength() uint16                      { return 0 }
func (l *pipeLink) LinkAddress() tcpip.LinkAddress               { return "" }
func (l *pipeLink) SetLinkAddress(tcpip.LinkAddress)             {}
func (l *pipeLink) Capabilities() stack.LinkEndpointCapabilities { return 0 }
func (l *pipeLink) Wait()                                        {}
func (l *pipeLink) ARPHardwareType() header.ARPHardwareType      { return header.ARPHardwareNone }
func (l *pipeLink) AddHeader(*stack.PacketBuffer)                {}
func (l *pipeLink) ParseHeader(*stack.PacketBuffer) bool         { return true }
func (l *pipeLink) Close()                                       {}
func (l *pipeLink) SetOnCloseAction(func())                      {}

func (l *pipeLink) Attach(d stack.NetworkDispatcher) {
	l.mu.Lock()
	l.disp = d
	l.mu.Unlock()
}

func (l *pipeLink) IsAttached() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.disp != nil
}

func (l *pipeLink) WritePackets(list stack.PacketBufferList) (int, tcpip.Error) {
	for _, pkt := range list.AsSlice() {
		b := make([]byte, 0, pkt.Size())
		for _, s := range pkt.AsSlices() {
			b = append(b, s...)
		}
		l.send(b)
	}
	return list.Len(), nil
}

func (l *pipeLink) deliver(p []byte) {
	l.mu.RLock()
	d := l.disp
	l.mu.RUnlock()
	if d == nil {
		return
	}
	pb := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(p)})
	d.DeliverNetworkPacket(ipv4.ProtocolNumber, pb)
	pb.DecRef()
}

func newTestStack(t testing.TB, link *pipeLink, addr netip.Addr) *stack.Stack {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	if err := s.CreateNIC(1, link); err != nil {
		t.Fatalf("CreateNIC: %v", err)
	}
	if addr.IsValid() {
		pa := tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddrFrom4(addr.As4()).WithPrefix()}
		if err := s.AddProtocolAddress(1, pa, stack.AddressProperties{}); err != nil {
			t.Fatalf("AddProtocolAddress: %v", err)
		}
	} else {
		s.SetSpoofing(1, true)
		s.SetPromiscuousMode(1, true)
	}
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}})
	sack := tcpip.TCPSACKEnabled(true)
	s.SetTransportProtocolOption(tcp.ProtocolNumber, &sack)
	timeWait := tcpip.TCPTimeWaitTimeoutOption(100 * time.Millisecond)
	s.SetTransportProtocolOption(tcp.ProtocolNumber, &timeWait)
	if runtime.GOOS == "windows" {
		recovery := tcpip.TCPRecovery(0)
		s.SetTransportProtocolOption(tcp.ProtocolNumber, &recovery)
	}
	return s
}

func closeStack(s *stack.Stack) {
	s.Close()
	for _, ep := range s.CleanupEndpoints() {
		ep.Abort()
	}
}

// fakeRules matches chain elements literally; it stands in for compiled procmatch rules.
type fakeRules []string

func (r fakeRules) Empty() bool { return len(r) == 0 }

func (r fakeRules) MatchChain(chain []string) bool {
	for _, p := range chain {
		if slices.Contains(r, p) {
			return true
		}
	}
	return false
}

func (r fakeRules) sameRules(o matcher) bool {
	q, ok := o.(fakeRules)
	return ok && slices.Equal(r, q)
}

func fakeCompile(apps, never []string) (*ruleSet, []procmatch.RuleError) {
	var errs []procmatch.RuleError
	var out fakeRules
	for i, a := range apps {
		switch {
		case !strings.HasPrefix(a, "/"):
			errs = append(errs, procmatch.RuleError{Index: i, Code: procmatch.CodeNotAbsolute})
		case slices.Contains(never, a):
			errs = append(errs, procmatch.RuleError{Index: i, Code: procmatch.CodeOwnImage})
		case !slices.Contains(out, a):
			out = append(out, a)
		}
	}
	slices.Sort(out)
	return &ruleSet{match: out, apps: len(out)}, errs
}

// fakeClassifier answers by app port, like a socket table with scripted owners.
type fakeClassifier struct {
	mu        sync.Mutex
	owners    map[uint16]procmatch.Owner
	bypass    map[uint16]bool
	ambiguous map[uint16]bool
	gate      chan struct{}
	valGate   chan struct{}
	releases  []func()
	seen      []procmatch.FlowID
	bad       []string

	fail      atomic.Bool
	panics    atomic.Bool
	valFail   atomic.Bool
	calls     atomic.Int64
	flows     atomic.Int64
	valCalls  atomic.Int64
	valChecks atomic.Int64
	closed    atomic.Bool
	observed  atomic.Pointer[procmatch.Rules]
}

func newFakeClassifier() *fakeClassifier {
	return &fakeClassifier{owners: map[uint16]procmatch.Owner{}, bypass: map[uint16]bool{}, ambiguous: map[uint16]bool{}}
}

func (f *fakeClassifier) set(port uint16, bypass bool, chain ...string) {
	f.setOwner(port, 1000+int(port), bypass, chain...)
}

// setOwner models another process (pid) holding the port, e.g. after the previous owner closed it.
func (f *fakeClassifier) setOwner(port uint16, pid int, bypass bool, chain ...string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners[port] = procmatch.Owner{PID: pid, Start: 7, Chain: chain}
	f.bypass[port] = bypass
}

// setAmbiguous models two sockets sharing the port (SO_REUSEADDR).
func (f *fakeClassifier) setAmbiguous(port uint16, on bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ambiguous[port] = on
}

func (f *fakeClassifier) classifiedCount(port uint16) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.seen {
		if id.App.Port() == port {
			n++
		}
	}
	return n
}

// blockValidate makes Validate wait until the returned release func runs.
func (f *fakeClassifier) blockValidate() (release func()) {
	g := make(chan struct{})
	f.mu.Lock()
	f.valGate = g
	f.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			f.mu.Lock()
			if f.valGate == g {
				f.valGate = nil
			}
			f.mu.Unlock()
			close(g)
		})
	}
	f.mu.Lock()
	f.releases = append(f.releases, release)
	f.mu.Unlock()
	return release
}

// block makes Classify wait until the returned release func runs.
func (f *fakeClassifier) block() (release func()) {
	g := make(chan struct{})
	f.mu.Lock()
	f.gate = g
	f.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			f.mu.Lock()
			if f.gate == g {
				f.gate = nil
			}
			f.mu.Unlock()
			close(g)
		})
	}
	f.mu.Lock()
	f.releases = append(f.releases, release)
	f.mu.Unlock()
	return release
}

func (f *fakeClassifier) classifiedPort(port uint16) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.seen, func(id procmatch.FlowID) bool { return id.App.Port() == port })
}

func (f *fakeClassifier) Classify(_ *procmatch.Rules, flows []procmatch.FlowID) []procmatch.Result {
	f.calls.Add(1)
	f.flows.Add(int64(len(flows)))
	f.mu.Lock()
	g := f.gate
	for _, id := range flows {
		f.seen = append(f.seen, id)
		if (id.Proto != protoTCP && id.Proto != protoUDP) || id.App.Addr() != tunAddr || !id.Remote.IsValid() {
			f.bad = append(f.bad, fmt.Sprint(id))
		}
	}
	f.mu.Unlock()
	if g != nil {
		<-g
	}
	if f.panics.Load() {
		panic("fake classifier blew up")
	}
	if f.fail.Load() {
		return nil
	}
	out := make([]procmatch.Result, len(flows))
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, id := range flows {
		o, ok := f.owners[id.App.Port()]
		if !ok || f.ambiguous[id.App.Port()] {
			continue
		}
		o.Chain = slices.Clone(o.Chain)
		out[i].Owner = o
		if f.bypass[id.App.Port()] {
			out[i].Verdict = procmatch.VerdictBypass
		}
	}
	return out
}

// Validate confirms a check while the same pid and start time hold the port alone.
func (f *fakeClassifier) Validate(checks []procmatch.SocketCheck) []bool {
	f.valCalls.Add(1)
	f.valChecks.Add(int64(len(checks)))
	f.mu.Lock()
	g := f.valGate
	f.mu.Unlock()
	if g != nil {
		<-g
	}
	if f.valFail.Load() {
		return nil
	}
	out := make([]bool, len(checks))
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range checks {
		if c.Proto != protoUDP || c.App.Addr() != tunAddr {
			f.bad = append(f.bad, fmt.Sprint(c))
		}
		o, ok := f.owners[c.App.Port()]
		out[i] = ok && !f.ambiguous[c.App.Port()] && o.PID == c.Owner.PID && o.Start == c.Owner.Start
	}
	return out
}

func (f *fakeClassifier) Close() error {
	f.closed.Store(true)
	return nil
}

func (f *fakeClassifier) ObserveRules(rules *procmatch.Rules) { f.observed.Store(rules) }

// fakeEgress dials 127.0.0.1 listeners in place of the physical interface; UDP sockets
// translate remote addresses to 127.0.0.1 servers and back.
type fakeEgress struct {
	mu         sync.Mutex
	target     string
	targets    map[netip.AddrPort]string
	hook       func(ctx context.Context, dst netip.AddrPort) (net.Conn, error)
	conns      []net.Conn
	udpTo      map[netip.AddrPort]netip.AddrPort
	udpFrom    map[netip.AddrPort]netip.AddrPort
	listenHook func(ctx context.Context) (net.PacketConn, error)
	writeGate  chan struct{}
	writeErr   error
	writes     []udpWrite
	pconns     []*fakePacketConn
	ident      egress.Identity
	identErr   error
	last       egress.Identity
	lastOK     bool
	repinErr   error
	releases   []func()

	dials     atomic.Int64
	refreshes atomic.Int64
	listens   atomic.Int64
	udpWrites atomic.Int64
	repins    atomic.Int64
	closed    atomic.Bool
}

type udpWrite struct {
	to   netip.AddrPort
	size int
}

var physIdentity = egress.Identity{Index: 1, Name: "phys0", Addrs: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}

func newFakeEgress() *fakeEgress {
	return &fakeEgress{
		udpTo:   map[netip.AddrPort]netip.AddrPort{},
		udpFrom: map[netip.AddrPort]netip.AddrPort{},
		ident:   physIdentity,
		last:    physIdentity,
		lastOK:  true,
	}
}

func (f *fakeEgress) mapUDP(remote, local netip.AddrPort) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.udpTo[remote] = local
	f.udpFrom[local] = remote
}

func (f *fakeEgress) setIdentity(id egress.Identity, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ident, f.identErr = id, err
}

// blockWrites makes every UDP WriteTo wait until release or until its socket closes.
func (f *fakeEgress) blockWrites() (release func()) {
	g := make(chan struct{})
	f.mu.Lock()
	f.writeGate = g
	f.mu.Unlock()
	var once sync.Once
	release = func() {
		once.Do(func() {
			f.mu.Lock()
			if f.writeGate == g {
				f.writeGate = nil
			}
			f.mu.Unlock()
			close(g)
		})
	}
	f.mu.Lock()
	f.releases = append(f.releases, release)
	f.mu.Unlock()
	return release
}

func (f *fakeEgress) setWriteErr(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.writeErr = err
}

func (f *fakeEgress) writeLog() []udpWrite {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.writes)
}

func (f *fakeEgress) writesTo(remote netip.AddrPort) []int {
	var sizes []int
	for _, w := range f.writeLog() {
		if w.to == remote {
			sizes = append(sizes, w.size)
		}
	}
	return sizes
}

// fakePacketConn is a real loopback UDP socket (so Repin and Close behave) whose
// addresses are translated through the fake egress maps.
type fakePacketConn struct {
	*net.UDPConn
	eg        *fakeEgress
	closed    chan struct{}
	closeOnce sync.Once
}

func (c *fakePacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	eg := c.eg
	eg.udpWrites.Add(1)
	to := addr.(*net.UDPAddr).AddrPort()
	eg.mu.Lock()
	g, werr := eg.writeGate, eg.writeErr
	local, mapped := eg.udpTo[to]
	eg.mu.Unlock()
	if g != nil {
		select {
		case <-g:
		case <-c.closed:
			return 0, net.ErrClosed
		}
	}
	if werr != nil {
		return 0, werr
	}
	eg.mu.Lock()
	eg.writes = append(eg.writes, udpWrite{to: to, size: len(p)})
	eg.mu.Unlock()
	if !mapped {
		return len(p), nil
	}
	return c.UDPConn.WriteToUDPAddrPort(p, local)
}

func (c *fakePacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, from, err := c.UDPConn.ReadFromUDPAddrPort(p)
	if err != nil {
		return n, nil, err
	}
	c.eg.mu.Lock()
	if r, ok := c.eg.udpFrom[from]; ok {
		from = r
	}
	c.eg.mu.Unlock()
	return n, net.UDPAddrFromAddrPort(from), nil
}

func (c *fakePacketConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return c.UDPConn.Close()
}

func (f *fakeEgress) setTarget(addr string) {
	f.mu.Lock()
	f.target = addr
	f.mu.Unlock()
}

func (f *fakeEgress) setHook(h func(ctx context.Context, dst netip.AddrPort) (net.Conn, error)) {
	f.mu.Lock()
	f.hook = h
	f.mu.Unlock()
}

func (f *fakeEgress) DialTCP(ctx context.Context, dst netip.AddrPort) (net.Conn, error) {
	f.dials.Add(1)
	f.mu.Lock()
	hook, target := f.hook, f.target
	if t, ok := f.targets[dst]; ok {
		target = t
	}
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx, dst)
	}
	if target == "" {
		return nil, fmt.Errorf("dial tcp %s: %w", dst, syscall.ECONNREFUSED)
	}
	var d net.Dialer
	c, err := d.DialContext(ctx, "tcp4", target)
	if err == nil {
		f.mu.Lock()
		f.conns = append(f.conns, c)
		f.mu.Unlock()
	}
	return c, err
}

func (f *fakeEgress) ListenUDP(ctx context.Context) (net.PacketConn, error) {
	f.listens.Add(1)
	f.mu.Lock()
	hook := f.listenHook
	f.mu.Unlock()
	if hook != nil {
		return hook(ctx)
	}
	var lc net.ListenConfig
	pc, err := lc.ListenPacket(ctx, "udp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	c := &fakePacketConn{UDPConn: pc.(*net.UDPConn), eg: f, closed: make(chan struct{})}
	f.mu.Lock()
	f.pconns = append(f.pconns, c)
	f.mu.Unlock()
	return c, nil
}

func (f *fakeEgress) Repin(c syscall.Conn) error {
	f.repins.Add(1)
	if _, ok := c.(*fakePacketConn); !ok {
		return errors.New("repin of a foreign socket")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.repinErr
}

func (f *fakeEgress) Current() (egress.Identity, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ident, f.identErr
}

// Refresh reports a change only when the scripted identity differs from the last one seen.
func (f *fakeEgress) Refresh() (old, cur egress.Identity, changed bool, err error) {
	f.refreshes.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	old = f.last
	if f.identErr != nil {
		changed = f.lastOK
		f.lastOK = false
		return old, egress.Identity{}, changed, f.identErr
	}
	changed = !f.lastOK || !f.last.Equal(f.ident)
	f.last, f.lastOK = f.ident, true
	return old, f.ident, changed, nil
}

func (f *fakeEgress) Close() error {
	f.closed.Store(true)
	return nil
}

type logCapture struct {
	mu    sync.Mutex
	lines []string
}

func (l *logCapture) logf(format string, args ...any) {
	l.mu.Lock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
	l.mu.Unlock()
}

func (l *logCapture) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Clone(l.lines)
}

type harnessOpts struct {
	kind     tunKind
	lim      func(*limits)
	permit   func(ctx context.Context, on bool) error
	noReader bool
	bufLen   int
	peer     bool
	clsErr   error
	egErr    error
}

// harness wires app stack -> fake TUN -> wrapper -> tunnel reader (+ optional tunnel peer
// stack standing in for hosts behind the VPN), with fakes for classifier and egress.
type harness struct {
	t    testing.TB
	o    harnessOpts
	c    *Controller
	cls  *fakeClassifier
	eg   *fakeEgress
	tun  *fakeTUN
	dev  tun.Device
	logs *logCapture

	appLink  *pipeLink
	app      *stack.Stack
	peerLink *pipeLink
	peer     *stack.Stack

	quit       chan struct{}
	readerDone chan struct{}
	readErr    atomic.Value

	mu      sync.Mutex
	tunnel  [][]byte
	tunWait chan struct{}

	closeOnce  sync.Once
	servers    []net.Listener
	udpServers []*udpServer
	appConns   []*gonet.UDPConn
	clsMade    atomic.Int64
	egMade     atomic.Int64

	recordApp atomic.Bool
	appOut    [][]byte
}

func newHarness(t testing.TB, o harnessOpts) *harness {
	t.Helper()
	if o.bufLen == 0 {
		o.bufLen = 65535 + wgOffset
	}
	h := &harness{
		t:          t,
		o:          o,
		cls:        newFakeClassifier(),
		eg:         newFakeEgress(),
		logs:       &logCapture{},
		quit:       make(chan struct{}),
		readerDone: make(chan struct{}),
		tunWait:    make(chan struct{}),
	}
	h.c = NewController(ControllerOptions{
		NewClassifier: func() (procmatch.Classifier, error) {
			h.clsMade.Add(1)
			if o.clsErr != nil {
				return nil, o.clsErr
			}
			return h.cls, nil
		},
		NewEgress: func() (egress.Dialer, error) {
			h.egMade.Add(1)
			if o.egErr != nil {
				return nil, o.egErr
			}
			return h.eg, nil
		},
		SetEgressPermit: o.permit,
		Logf:            h.logs.logf,
	})
	h.c.compile = fakeCompile
	if o.lim != nil {
		o.lim(&h.c.lim)
	}
	h.tun = newFakeTUN(o.kind)
	var inner tun.Device = h.tun
	if o.kind == kindWindows {
		inner = &winFakeTUN{fakeTUN: h.tun}
	}
	h.dev = h.c.WrapTUN(inner, testTunnelInfo())
	h.appLink = &pipeLink{mtu: testMTU, send: h.fromApp}
	h.app = newTestStack(t, h.appLink, tunAddr)
	go h.osLoop()
	if o.peer {
		h.peerLink = &pipeLink{mtu: testMTU, send: h.writeFromTunnel}
		h.peer = newTestStack(t, h.peerLink, netip.Addr{})
		fwd := tcp.NewForwarder(h.peer, 0, 1024, func(r *tcp.ForwarderRequest) {
			id := r.ID()
			local := netip.AddrFrom4(id.LocalAddress.As4())
			if netip.AddrFrom4(id.RemoteAddress.As4()) != tunAddr || alwaysTunnelDst(local) {
				r.Complete(true)
				return
			}
			var wq waiter.Queue
			ep, err := r.CreateEndpoint(&wq)
			if err != nil {
				r.Complete(true)
				return
			}
			r.Complete(false)
			go echoConn(gonet.NewTCPConn(&wq, ep))
		})
		h.peer.SetTransportProtocolHandler(tcp.ProtocolNumber, fwd.HandlePacket)
	}
	if o.noReader {
		close(h.readerDone)
	} else {
		go h.readLoop()
	}
	t.Cleanup(h.close)
	return h
}

func (h *harness) fromApp(p []byte) {
	if h.recordApp.Load() {
		h.mu.Lock()
		h.appOut = append(h.appOut, slices.Clone(p))
		h.mu.Unlock()
	}
	h.tun.fromOS(p)
}

func (h *harness) appPackets() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.appOut)
}

func (h *harness) osLoop() {
	for {
		select {
		case <-h.quit:
			return
		case p := <-h.tun.toOS:
			h.appLink.deliver(p)
		}
	}
}

// writeFromTunnel is the VPN side answering: wireguard-go writes decrypted packets at offset 16.
func (h *harness) writeFromTunnel(p []byte) {
	b := make([]byte, wgOffset+len(p))
	copy(b[wgOffset:], p)
	_, _ = h.dev.Write([][]byte{b}, wgOffset)
}

// readLoop plays wireguard-go's RoutineReadFromTUN and scribbles over its buffers after
// each read, as encryption in place would.
func (h *harness) readLoop() {
	defer close(h.readerDone)
	bs := h.dev.BatchSize()
	bufs := make([][]byte, bs)
	sizes := make([]int, bs)
	for i := range bufs {
		bufs[i] = make([]byte, h.o.bufLen)
	}
	for {
		n, err := h.dev.Read(bufs, sizes, wgOffset)
		for i := 0; i < n; i++ {
			if sizes[i] < 1 {
				continue
			}
			p := slices.Clone(bufs[i][wgOffset : wgOffset+sizes[i]])
			for j := range bufs[i][:wgOffset+sizes[i]] {
				bufs[i][j] = 0xAA
			}
			sizes[i] = 0
			h.recordTunnel(p)
			if h.peer != nil {
				h.peerLink.deliver(p)
			}
		}
		if err != nil {
			if errors.Is(err, tun.ErrTooManySegments) {
				continue
			}
			h.readErr.Store(err)
			return
		}
	}
}

func (h *harness) recordTunnel(p []byte) {
	h.mu.Lock()
	h.tunnel = append(h.tunnel, p)
	close(h.tunWait)
	h.tunWait = make(chan struct{})
	h.mu.Unlock()
}

func (h *harness) tunnelPackets() [][]byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.tunnel)
}

// waitTunnel waits for a packet the tunnel reader saw that matches pred.
func (h *harness) waitTunnel(timeout time.Duration, pred func(pktInfo) bool) bool {
	deadline := time.After(timeout)
	for {
		h.mu.Lock()
		w := h.tunWait
		found := slices.ContainsFunc(h.tunnel, func(p []byte) bool {
			info, ok := parseIPv4(p)
			return ok && pred(info)
		})
		h.mu.Unlock()
		if found {
			return true
		}
		select {
		case <-w:
		case <-deadline:
			return false
		}
	}
}

func (h *harness) tunnelHasPort(port uint16) bool {
	for _, p := range h.tunnelPackets() {
		if info, ok := parseIPv4(p); ok && info.srcPort == port {
			return true
		}
	}
	return false
}

func (h *harness) device() *Device { return h.dev.(*Device) }

func (h *harness) engine() *engine {
	h.t.Helper()
	var e *engine
	waitFor(h.t, 2*time.Second, "pump start", func() bool {
		e = h.device().eng.Load()
		return e != nil && h.device().pumping.Load()
	})
	return e
}

func (h *harness) setRules(apps ...string) {
	h.t.Helper()
	if errs := h.c.SetRules(apps, nil); len(errs) > 0 {
		h.t.Fatalf("SetRules: %v", errs)
	}
}

// dial connects from a fixed app port, like an app socket bound by the OS.
func (h *harness) dial(port uint16, dst netip.AddrPort, timeout time.Duration) (*gonet.TCPConn, tcpip.Endpoint, error) {
	var wq waiter.Queue
	ep, terr := h.app.NewEndpoint(tcp.ProtocolNumber, ipv4.ProtocolNumber, &wq)
	if terr != nil {
		return nil, nil, errors.New(terr.String())
	}
	ep.SocketOptions().SetReuseAddress(true)
	entry, ch := waiter.NewChannelEntry(waiter.WritableEvents)
	wq.EventRegister(&entry)
	defer wq.EventUnregister(&entry)
	if terr := ep.Bind(tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFrom4(tunAddr.As4()), Port: port}); terr != nil {
		ep.Close()
		return nil, nil, errors.New("bind: " + terr.String())
	}
	terr = ep.Connect(tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFrom4(dst.Addr().As4()), Port: dst.Port()})
	if _, ok := terr.(*tcpip.ErrConnectStarted); ok {
		select {
		case <-ch:
		case <-time.After(timeout):
			ep.Abort()
			return nil, nil, errors.New("connect timed out")
		}
		terr = ep.LastError()
	}
	if terr != nil {
		ep.Close()
		return nil, nil, errors.New("connect: " + terr.String())
	}
	return gonet.NewTCPConn(&wq, ep), ep, nil
}

func (h *harness) mustDial(port uint16, dst netip.AddrPort) (*gonet.TCPConn, tcpip.Endpoint) {
	h.t.Helper()
	c, ep, err := h.dial(port, dst, 5*time.Second)
	if err != nil {
		h.t.Fatalf("dial from port %d: %v", port, err)
	}
	return c, ep
}

// server starts a 127.0.0.1 listener the fake egress can reach.
func (h *harness) server(handle func(net.Conn)) string {
	h.t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		h.t.Fatalf("listen: %v", err)
	}
	h.mu.Lock()
	h.servers = append(h.servers, ln)
	h.mu.Unlock()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go handle(c)
		}
	}()
	return ln.Addr().String()
}

func (h *harness) close() {
	h.closeOnce.Do(func() {
		releaseAll(&h.cls.mu, &h.cls.releases)
		releaseAll(&h.eg.mu, &h.eg.releases)
		start := time.Now()
		if err := h.dev.Close(); err != nil {
			h.t.Errorf("device Close: %v", err)
		}
		if d := time.Since(start); d > 500*time.Millisecond {
			h.t.Errorf("device Close took %v", d)
		}
		_ = h.c.Close()
		select {
		case <-h.readerDone:
		case <-time.After(2 * time.Second):
			h.t.Errorf("tunnel reader still blocked after Close")
		}
		close(h.quit)
		closeStack(h.app)
		if h.peer != nil {
			closeStack(h.peer)
		}
		h.mu.Lock()
		for _, ln := range h.servers {
			_ = ln.Close()
		}
		for _, s := range h.udpServers {
			_ = s.conn.Close()
		}
		for _, c := range h.appConns {
			_ = c.Close()
		}
		h.mu.Unlock()
		h.eg.mu.Lock()
		for _, c := range h.eg.conns {
			_ = c.Close()
		}
		h.eg.mu.Unlock()
		for _, v := range h.tun.violationList() {
			h.t.Errorf("TUN contract violation: %s", v)
		}
		h.cls.mu.Lock()
		for _, b := range h.cls.bad {
			h.t.Errorf("classifier got malformed flow %s", b)
		}
		h.cls.mu.Unlock()
		checkNoEngineGoroutines(h.t)
	})
}

// checkNoEngineGoroutines fails if engine, device or gVisor goroutines outlive their harness.
func checkNoEngineGoroutines(t testing.TB) {
	t.Helper()
	markers := []string{"splittunnel.(*engine)", "splittunnel.(*Device)", "splittunnel.(*Controller)", "splittunnel.(*udpSession)", "sagernet/gvisor"}
	var leaked []string
	deadline := time.Now().Add(5 * time.Second)
	for {
		leaked = leaked[:0]
		buf := make([]byte, 1<<20)
		buf = buf[:runtime.Stack(buf, true)]
		for _, g := range bytes.Split(buf, []byte("\n\n")) {
			s := string(g)
			if strings.Contains(s, "checkNoEngineGoroutines") {
				continue
			}
			for _, m := range markers {
				if strings.Contains(s, m) {
					leaked = append(leaked, s)
					break
				}
			}
		}
		if len(leaked) == 0 || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	for _, g := range leaked {
		t.Errorf("leaked goroutine:\n%s", g)
	}
}

func waitFor(t testing.TB, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func echoConn(c net.Conn) {
	defer c.Close()
	_, _ = io.Copy(c, c)
}

// echoThenBye echoes until the client half-closes, then sends "bye" and closes.
func echoThenBye(c net.Conn) {
	defer c.Close()
	_, _ = io.Copy(c, c)
	_, _ = c.Write([]byte("bye"))
}

func roundTrip(t testing.TB, c net.Conn, msg string) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	defer c.SetDeadline(time.Time{})
	if _, err := c.Write([]byte(msg)); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(msg))
	if _, err := io.ReadFull(c, got); err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if string(got) != msg {
		t.Fatalf("echo = %q, want %q", got, msg)
	}
}

// expectReset waits for the connection to fail with something other than a clean EOF.
func expectReset(t testing.TB, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 512)
	for {
		_, err := c.Read(buf)
		if err == nil {
			continue
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			t.Fatalf("no reset within 5s")
		}
		if errors.Is(err, io.EOF) {
			t.Fatalf("got clean EOF, want a reset")
		}
		return
	}
}

func dst(a netip.Addr, port uint16) netip.AddrPort { return netip.AddrPortFrom(a, port) }

// tunnelUDP is a UDP packet decide() always sends to the tunnel (resolver port).
func tunnelUDP(srcPort uint16, payload []byte) []byte {
	return udpPacket(dst(tunAddr, srcPort), dst(remoteB, 53), payload)
}

// udpApp opens an app socket on the tunnel address; it never sets DF, so the app stack
// fragments datagrams larger than the TUN MTU like an OS would.
func (h *harness) udpApp(port uint16) *gonet.UDPConn {
	h.t.Helper()
	var wq waiter.Queue
	ep, terr := h.app.NewEndpoint(udp.ProtocolNumber, ipv4.ProtocolNumber, &wq)
	if terr != nil {
		h.t.Fatalf("udp endpoint: %v", terr)
	}
	ep.SocketOptions().SetReceiveBufferSize(4<<20, true)
	ep.SocketOptions().SetSendBufferSize(4<<20, true)
	if terr := ep.SetSockOptInt(tcpip.MTUDiscoverOption, int(tcpip.PMTUDiscoveryDont)); terr != nil {
		h.t.Fatalf("pmtud: %v", terr)
	}
	if terr := ep.Bind(tcpip.FullAddress{NIC: 1, Addr: tcpip.AddrFrom4(tunAddr.As4()), Port: port}); terr != nil {
		ep.Close()
		h.t.Fatalf("udp bind %d: %v", port, terr)
	}
	c := gonet.NewUDPConn(&wq, ep)
	h.mu.Lock()
	h.appConns = append(h.appConns, c)
	h.mu.Unlock()
	return c
}

// udpServer is a 127.0.0.1 UDP peer the fake egress maps remote to. It echoes, answers
// "big:N" with N pattern bytes and stays silent for "quiet".
type udpServer struct {
	remote netip.AddrPort
	conn   *net.UDPConn
	mu     sync.Mutex
	got    [][]byte
	froms  []netip.AddrPort
}

func (h *harness) udpServer(remote netip.AddrPort) *udpServer {
	h.t.Helper()
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		h.t.Fatalf("listen udp: %v", err)
	}
	_ = c.SetReadBuffer(4 << 20)
	s := &udpServer{remote: remote, conn: c}
	h.eg.mapUDP(remote, c.LocalAddr().(*net.UDPAddr).AddrPort())
	h.mu.Lock()
	h.udpServers = append(h.udpServers, s)
	h.mu.Unlock()
	go s.serve()
	return s
}

func (s *udpServer) serve() {
	buf := make([]byte, 65535)
	for {
		n, from, err := s.conn.ReadFromUDPAddrPort(buf)
		if err != nil {
			return
		}
		p := slices.Clone(buf[:n])
		s.mu.Lock()
		s.got = append(s.got, p)
		s.froms = append(s.froms, from)
		s.mu.Unlock()
		reply := p
		switch {
		case bytes.HasPrefix(p, []byte("quiet")):
			continue
		case bytes.HasPrefix(p, []byte("big:")):
			size, _ := strconv.Atoi(string(p[4:]))
			reply = pattern(size)
		}
		_, _ = s.conn.WriteToUDPAddrPort(reply, from)
	}
}

func (s *udpServer) received() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.got)
}

func (s *udpServer) sources() []netip.AddrPort {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.froms)
}

func (s *udpServer) lastSource(t testing.TB) netip.AddrPort {
	t.Helper()
	src := s.sources()
	if len(src) == 0 {
		t.Fatal("server saw no datagram")
	}
	return src[len(src)-1]
}

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i % 251)
	}
	return b
}

func udpAddr(ap netip.AddrPort) *net.UDPAddr {
	return &net.UDPAddr{IP: ap.Addr().AsSlice(), Port: int(ap.Port())}
}

// udpExchange sends msg from the app socket to to and waits for want back from to.
func udpExchange(t testing.TB, c *gonet.UDPConn, to netip.AddrPort, msg, want []byte) {
	t.Helper()
	if _, err := c.WriteTo(msg, udpAddr(to)); err != nil {
		t.Fatalf("app send: %v", err)
	}
	expectUDP(t, c, to, want, 5*time.Second)
}

func expectUDP(t testing.TB, c *gonet.UDPConn, from netip.AddrPort, want []byte, timeout time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(timeout))
	defer c.SetReadDeadline(time.Time{})
	buf := make([]byte, 70000)
	n, a, err := c.ReadFrom(buf)
	if err != nil {
		t.Fatalf("app receive (want %d bytes from %s): %v", len(want), from, err)
	}
	got := a.(*net.UDPAddr).AddrPort()
	got = netip.AddrPortFrom(got.Addr().Unmap(), got.Port())
	if got != from || !bytes.Equal(buf[:n], want) {
		t.Fatalf("app got %d bytes from %s, want %d bytes from %s", n, got, len(want), from)
	}
}

// expectNoUDP fails if the app socket receives anything within d.
func expectNoUDP(t testing.TB, c *gonet.UDPConn, d time.Duration) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(d))
	defer c.SetReadDeadline(time.Time{})
	buf := make([]byte, 70000)
	if n, a, err := c.ReadFrom(buf); err == nil {
		t.Fatalf("app unexpectedly received %d bytes from %v", n, a)
	}
}

func testTunnelInfo() wg.TunnelInfo {
	return wg.TunnelInfo{Name: "fake0", Addresses: []netip.Addr{tunAddr}, DNS: []netip.Addr{tunDNS}, MTU: testMTU}
}

func releaseAll(mu *sync.Mutex, fns *[]func()) {
	mu.Lock()
	rs := slices.Clone(*fns)
	mu.Unlock()
	for _, r := range rs {
		r()
	}
}
