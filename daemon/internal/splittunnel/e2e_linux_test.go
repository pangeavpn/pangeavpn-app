//go:build linux

package splittunnel_test

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

const (
	e2eTunName = "ste2e0"
	e2eMTU     = 1420
	e2eOffset  = 16
	e2eBufLen  = 65535
)

var (
	e2eTunAddr = netip.MustParseAddr("10.200.0.2")
	e2eServer  = netip.MustParseAddr("10.99.0.1")
	e2ePhys    = netip.MustParseAddr("10.99.0.2")
	e2eDNS     = netip.MustParseAddr("10.200.0.53")
)

type e2ePkt struct {
	at           time.Time
	ver          int
	proto        uint8
	src, dst     netip.Addr
	sport, dport uint16
	flags        uint8
	id           uint16
	mf           bool
	fragOff      int
	length       int
	payload      string
}

func (p e2ePkt) String() string {
	if p.ver != 4 {
		return fmt.Sprintf("%s IPv%d len=%d", p.at.Format("15:04:05.000"), p.ver, p.length)
	}
	return fmt.Sprintf("%s proto=%d %s:%d > %s:%d len=%d id=%d mf=%v off=%d tcpflags=%#02x payload=%q",
		p.at.Format("15:04:05.000"), p.proto, p.src, p.sport, p.dst, p.dport, p.length, p.id, p.mf, p.fragOff, p.flags, p.payload)
}

func (p e2ePkt) isFrag() bool { return p.mf || p.fragOff != 0 }

func e2eParse(b []byte, at time.Time) e2ePkt {
	p := e2ePkt{at: at, length: len(b)}
	if len(b) == 0 {
		return p
	}
	p.ver = int(b[0] >> 4)
	if p.ver != 4 || len(b) < 20 {
		return p
	}
	ihl := int(b[0]&0xf) * 4
	p.id = binary.BigEndian.Uint16(b[4:6])
	ff := binary.BigEndian.Uint16(b[6:8])
	p.mf = ff&0x2000 != 0
	p.fragOff = int(ff&0x1fff) * 8
	p.proto = b[9]
	p.src = netip.AddrFrom4([4]byte(b[12:16]))
	p.dst = netip.AddrFrom4([4]byte(b[16:20]))
	if p.fragOff != 0 || len(b) < ihl+8 {
		return p
	}
	t := b[ihl:]
	p.sport = binary.BigEndian.Uint16(t[0:2])
	p.dport = binary.BigEndian.Uint16(t[2:4])
	var pl []byte
	switch p.proto {
	case unix.IPPROTO_TCP:
		if len(t) >= 20 {
			p.flags = t[13]
			if off := int(t[12]>>4) * 4; off <= len(t) {
				pl = t[off:]
			}
		}
	case unix.IPPROTO_UDP:
		pl = t[8:]
	}
	if len(pl) > 48 {
		pl = pl[:48]
	}
	p.payload = string(pl)
	return p
}

// e2eRecorder plays wireguard-go: it owns Device.Read and records every tunnel-bound packet.
type e2eRecorder struct {
	mu   sync.Mutex
	pkts []e2ePkt
	done chan struct{}
	err  error
}

func (r *e2eRecorder) run(dev tun.Device) {
	defer close(r.done)
	bs := dev.BatchSize()
	bufs := make([][]byte, bs)
	for i := range bufs {
		bufs[i] = make([]byte, e2eBufLen)
	}
	sizes := make([]int, bs)
	for {
		n, err := dev.Read(bufs, sizes, e2eOffset)
		now := time.Now()
		for i := 0; i < n; i++ {
			if sizes[i] > 0 {
				p := e2eParse(bufs[i][e2eOffset:e2eOffset+sizes[i]], now)
				r.mu.Lock()
				r.pkts = append(r.pkts, p)
				r.mu.Unlock()
			}
		}
		if err != nil {
			if errors.Is(err, tun.ErrTooManySegments) {
				continue
			}
			r.err = err
			return
		}
	}
}

func (r *e2eRecorder) find(from, to time.Time, pred func(e2ePkt) bool) []e2ePkt {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []e2ePkt
	for _, p := range r.pkts {
		if !p.at.Before(from) && !p.at.After(to) && pred(p) {
			out = append(out, p)
		}
	}
	return out
}

// e2eCapture taps the TUN with AF_PACKET: out = routed into the TUN by the kernel, in = written by the engine.
type e2eCapture struct {
	fd   int
	mu   sync.Mutex
	pkts []e2eCapPkt
	stop atomic.Bool
	done chan struct{}
}

type e2eCapPkt struct {
	e2ePkt
	in bool
}

func startCapture(ifname string) (*e2eCapture, error) {
	ifi, err := net.InterfaceByName(ifname)
	if err != nil {
		return nil, err
	}
	proto := int(htons(unix.ETH_P_ALL))
	fd, err := unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, proto)
	if err != nil {
		return nil, err
	}
	if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: htons(unix.ETH_P_ALL), Ifindex: ifi.Index}); err != nil {
		unix.Close(fd)
		return nil, err
	}
	tv := unix.NsecToTimeval(int64(100 * time.Millisecond))
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		unix.Close(fd)
		return nil, err
	}
	c := &e2eCapture{fd: fd, done: make(chan struct{})}
	go c.loop()
	return c, nil
}

func htons(v uint16) uint16 { return v<<8 | v>>8 }

func (c *e2eCapture) loop() {
	defer close(c.done)
	defer unix.Close(c.fd)
	buf := make([]byte, 1<<17)
	for !c.stop.Load() {
		n, from, err := unix.Recvfrom(c.fd, buf, 0)
		if err != nil {
			if errors.Is(err, unix.EAGAIN) || errors.Is(err, unix.EINTR) {
				continue
			}
			return
		}
		ll, _ := from.(*unix.SockaddrLinklayer)
		p := e2eCapPkt{e2eParse(buf[:n], time.Now()), ll != nil && ll.Pkttype != unix.PACKET_OUTGOING}
		c.mu.Lock()
		c.pkts = append(c.pkts, p)
		c.mu.Unlock()
	}
}

func (c *e2eCapture) close() {
	c.stop.Store(true)
	<-c.done
}

func (c *e2eCapture) find(from, to time.Time, pred func(e2eCapPkt) bool) []e2ePkt {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []e2ePkt
	for _, p := range c.pkts {
		if !p.at.Before(from) && !p.at.After(to) && pred(p) {
			out = append(out, p.e2ePkt)
		}
	}
	return out
}

type e2eEvent struct {
	at     time.Time
	kind   string
	remote netip.AddrPort
	tag    string
	n      int
	sha    string
	err    string
}

func (e e2eEvent) String() string {
	return fmt.Sprintf("%s %s from=%s tag=%q n=%d sha=%s err=%q", e.at.Format("15:04:05.000"), e.kind, e.remote, e.tag, e.n, e.sha, e.err)
}

// e2eEchoServer lives in the "internet" namespace and reports the source address it saw.
type e2eEchoServer struct {
	mu     sync.Mutex
	events []e2eEvent
	conns  map[net.Conn]struct{}
	tcp    net.Listener
	udp    net.PacketConn
	dns    net.PacketConn
	wg     sync.WaitGroup
}

func (s *e2eEchoServer) add(e e2eEvent) {
	e.at = time.Now()
	s.mu.Lock()
	s.events = append(s.events, e)
	s.mu.Unlock()
}

func (s *e2eEchoServer) find(from, to time.Time, pred func(e2eEvent) bool) []e2eEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []e2eEvent
	for _, e := range s.events {
		if !e.at.Before(from) && !e.at.After(to) && (pred == nil || pred(e)) {
			out = append(out, e)
		}
	}
	return out
}

func (s *e2eEchoServer) waitFor(timeout time.Duration, pred func(e2eEvent) bool) (e2eEvent, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if evs := s.find(time.Time{}, time.Now().Add(time.Hour), pred); len(evs) > 0 {
			return evs[0], true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return e2eEvent{}, false
}

// inNetNS runs fn on a thread switched into the namespace; the thread is discarded afterwards.
func inNetNS(path string, fn func() error) error {
	errc := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		f, err := os.Open(path)
		if err != nil {
			errc <- err
			return
		}
		defer f.Close()
		if err := unix.Setns(int(f.Fd()), unix.CLONE_NEWNET); err != nil {
			errc <- fmt.Errorf("setns: %w", err)
			return
		}
		errc <- fn()
	}()
	return <-errc
}

func startEchoServer(t *testing.T, nsPath string) *e2eEchoServer {
	s := &e2eEchoServer{conns: make(map[net.Conn]struct{})}
	err := inNetNS(nsPath, func() error {
		var err error
		if s.tcp, err = net.Listen("tcp4", "10.99.0.1:8080"); err != nil {
			return err
		}
		if s.udp, err = net.ListenPacket("udp4", "10.99.0.1:8080"); err != nil {
			return err
		}
		s.dns, err = net.ListenPacket("udp4", "10.99.0.1:53")
		return err
	})
	if err != nil {
		t.Fatalf("echo server: %v", err)
	}
	s.wg.Add(3)
	go s.acceptLoop()
	go s.udpLoop(s.udp, false)
	go s.udpLoop(s.dns, true)
	return s
}

func e2eAddrPort(a net.Addr) netip.AddrPort {
	switch v := a.(type) {
	case *net.TCPAddr:
		ap := v.AddrPort()
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
	case *net.UDPAddr:
		ap := v.AddrPort()
		return netip.AddrPortFrom(ap.Addr().Unmap(), ap.Port())
	}
	return netip.AddrPort{}
}

func (s *e2eEchoServer) acceptLoop() {
	defer s.wg.Done()
	for {
		c, err := s.tcp.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serveTCP(c)
	}
}

func (s *e2eEchoServer) serveTCP(c net.Conn) {
	defer s.wg.Done()
	defer func() {
		c.Close()
		s.mu.Lock()
		delete(s.conns, c)
		s.mu.Unlock()
	}()
	remote := e2eAddrPort(c.RemoteAddr())
	s.add(e2eEvent{kind: "tcp-accept", remote: remote})
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	line, err := br.ReadString('\n')
	if err != nil {
		s.add(e2eEvent{kind: "tcp-closed", remote: remote, err: err.Error()})
		return
	}
	tag := strings.TrimSpace(strings.TrimPrefix(line, "HELLO "))
	s.add(e2eEvent{kind: "tcp-hello", remote: remote, tag: tag})
	fmt.Fprintf(c, "SRC %s TAG %s\n", remote, tag)
	c.SetReadDeadline(time.Now().Add(60 * time.Second))
	_, err = io.Copy(io.Discard, br)
	msg := "EOF"
	if err != nil {
		msg = err.Error()
	}
	s.add(e2eEvent{kind: "tcp-closed", remote: remote, tag: tag, err: msg})
}

func (s *e2eEchoServer) udpLoop(pc net.PacketConn, dns bool) {
	defer s.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		remote := e2eAddrPort(from)
		data := buf[:n]
		if dns {
			s.add(e2eEvent{kind: "dns", remote: remote, tag: string(data), n: n})
			continue
		}
		if bytes.HasPrefix(data, []byte("BIG ")) {
			line, _, _ := bytes.Cut(data, []byte("\n"))
			f := strings.Fields(string(line))
			sum := sha256.Sum256(data)
			sha := hex.EncodeToString(sum[:8])
			s.add(e2eEvent{kind: "udp-big", remote: remote, tag: f[1], n: n, sha: sha})
			out := []byte(fmt.Sprintf("SRC %s TAG %s LEN %d SHA %s\n", remote, f[1], n, sha))
			if len(f) > 2 && f[2] == "PAD" && n > len(out) {
				out = append(out, bytes.Repeat([]byte{'x'}, n-len(out))...)
			}
			pc.WriteTo(out, from)
			continue
		}
		tag := strings.TrimPrefix(string(data), "HELLO ")
		s.add(e2eEvent{kind: "udp", remote: remote, tag: tag, n: n})
		pc.WriteTo([]byte(fmt.Sprintf("SRC %s TAG %s\n", remote, tag)), from)
	}
}

func (s *e2eEchoServer) close() {
	s.tcp.Close()
	s.udp.Close()
	s.dns.Close()
	s.mu.Lock()
	for c := range s.conns {
		c.Close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

type e2eRun struct {
	start, end time.Time
	Out        string `json:"out"`
	Err        string `json:"err"`
	PID        int    `json:"pid"`
	TookMs     int64  `json:"tookMs"`
}

func (r e2eRun) line(prefix string) (string, bool) {
	for _, l := range strings.Split(r.Out, "\n") {
		if strings.HasPrefix(l, prefix) {
			return l, true
		}
	}
	return "", false
}

func (r e2eRun) port(prefix string) uint16 {
	l, ok := r.line(prefix + " LPORT ")
	if !ok {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(l, prefix+" LPORT ")))
	return uint16(n)
}

// launch runs argv through the launcher, which is not a descendant of the daemon (self tree never bypasses).
func launch(t *testing.T, sock string, timeout time.Duration, argv ...string) e2eRun {
	t.Helper()
	run, err := launchRun(sock, timeout, argv...)
	if err != nil {
		t.Fatalf("launcher: %v", err)
	}
	t.Logf("run %v (pid %d, %dms, err=%q):\n%s", argv, run.PID, run.TookMs, run.Err, strings.TrimRight(run.Out, "\n"))
	return run
}

func launchRun(sock string, timeout time.Duration, argv ...string) (e2eRun, error) {
	run := e2eRun{start: time.Now()}
	c, err := net.Dial("unix", sock)
	if err != nil {
		return run, err
	}
	defer c.Close()
	req := map[string]any{"argv": argv, "timeoutMs": timeout.Milliseconds()}
	if err := json.NewEncoder(c).Encode(req); err != nil {
		return run, err
	}
	if err := json.NewDecoder(c).Decode(&run); err != nil {
		return run, err
	}
	run.end = time.Now()
	return run, nil
}

type e2eLogs struct {
	mu    sync.Mutex
	lines []string
}

func (l *e2eLogs) logf(format string, args ...any) {
	line := time.Now().Format("15:04:05.000 ") + fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	fmt.Println("ENGINE-LOG", line)
}

func (l *e2eLogs) has(sub string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, s := range l.lines {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

func mustIP(t *testing.T, args ...string) {
	t.Helper()
	out, err := exec.Command("ip", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ip %s: %v: %s", strings.Join(args, " "), err, out)
	}
}

func ipShow(args ...string) string {
	out, _ := exec.Command("ip", args...).CombinedOutput()
	return strings.TrimSpace(string(out))
}

func logPkts(t *testing.T, label string, pkts []e2ePkt) {
	t.Helper()
	var b strings.Builder
	for i, p := range pkts {
		if i == 12 {
			fmt.Fprintf(&b, "\n  ... %d more", len(pkts)-i)
			break
		}
		fmt.Fprintf(&b, "\n  %s", p)
	}
	t.Logf("%s: %d packet(s)%s", label, len(pkts), b.String())
}

func logEvents(t *testing.T, label string, evs []e2eEvent) {
	t.Helper()
	var b strings.Builder
	for _, e := range evs {
		fmt.Fprintf(&b, "\n  %s", e)
	}
	t.Logf("%s: %d event(s)%s", label, len(evs), b.String())
}

func fromPort(port uint16, proto uint8) func(e2ePkt) bool {
	return func(p e2ePkt) bool {
		return p.ver == 4 && p.proto == proto && p.src == e2eTunAddr && p.sport == port && p.fragOff == 0
	}
}

func TestSplitTunnelRealKernelE2E(t *testing.T) {
	dir := os.Getenv("ST_E2E_DIR")
	nsPath := os.Getenv("ST_E2E_NET_NS")
	if dir == "" || nsPath == "" {
		t.Skip("set ST_E2E_DIR and ST_E2E_NET_NS (run via the e2e namespace script as root)")
	}
	sock := filepath.Join(dir, "launcher.sock")
	excluded := filepath.Join(dir, "excluded", "app")
	normal := filepath.Join(dir, "normal", "app")
	child := filepath.Join(dir, "child", "app")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	baseG := runtime.NumGoroutine()
	t.Logf("baseline goroutines=%d self=%s pid=%d", baseG, self, os.Getpid())

	srv := startEchoServer(t, nsPath)
	dev, err := tun.CreateTUN(e2eTunName, e2eMTU)
	if err != nil {
		t.Fatalf("CreateTUN: %v", err)
	}
	mustIP(t, "addr", "add", e2eTunAddr.String()+"/32", "dev", e2eTunName)
	mustIP(t, "link", "set", e2eTunName, "up")
	mustIP(t, "route", "add", "0.0.0.0/1", "dev", e2eTunName, "table", "51820")
	mustIP(t, "route", "add", "128.0.0.0/1", "dev", e2eTunName, "table", "51820")
	mustIP(t, "rule", "add", "pref", "32764", "not", "fwmark", "0xca6c/0xca6c", "table", "51820")
	mustIP(t, "rule", "add", "pref", "32765", "table", "main", "suppress_prefixlength", "0")
	t.Logf("client ns:\n%s\n%s\n%s\n%s\nroute get (unmarked): %s\nroute get (mark 0x1ca6c): %s",
		ipShow("-4", "addr"), ipShow("rule"), ipShow("route", "show", "table", "51820"), ipShow("route"),
		ipShow("route", "get", "10.99.0.1"), ipShow("route", "get", "10.99.0.1", "mark", "0x1ca6c"))

	logs := &e2eLogs{}
	ctrl := splittunnel.NewController(splittunnel.ControllerOptions{
		NewClassifier: func() (procmatch.Classifier, error) {
			return procmatch.NewClassifier(procmatch.Options{SelfPID: os.Getpid(), NeverBypass: []string{self}, Logf: logs.logf})
		},
		NewEgress: func() (egress.Dialer, error) { return egress.New(egress.Options{Logf: logs.logf}) },
		Logf:      logs.logf,
	})
	wrapped := ctrl.WrapTUN(dev, wg.TunnelInfo{Name: e2eTunName, Addresses: []netip.Addr{e2eTunAddr}, DNS: []netip.Addr{e2eDNS}, MTU: e2eMTU})
	if _, ok := wrapped.(*splittunnel.Device); !ok {
		t.Fatalf("WrapTUN returned %T, want *splittunnel.Device", wrapped)
	}
	capt, err := startCapture(e2eTunName)
	if err != nil {
		t.Fatalf("AF_PACKET capture: %v", err)
	}
	rec := &e2eRecorder{done: make(chan struct{})}
	go rec.run(wrapped)
	if errs := ctrl.SetRules([]string{excluded}, []string{self}); len(errs) > 0 {
		t.Fatalf("SetRules refused: %+v", errs)
	}

	// The first tunnel-bound packet hands the device to the pump and starts the lazy engines.
	warm, err := net.Dial("udp4", "10.99.0.1:9")
	if err != nil {
		t.Fatal(err)
	}
	warm.Write([]byte("warm-up"))
	warm.Close()
	deadline := time.Now().Add(15 * time.Second)
	for !logs.has("app lookup and off-tunnel egress ready") {
		if logs.has("unavailable") || time.Now().After(deadline) {
			t.Fatalf("engines did not become ready; status=%+v", ctrl.Status())
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(1500 * time.Millisecond)
	t.Logf("engines ready; status=%+v", ctrl.Status())

	status := func(label string) { t.Logf("status after %s: %+v", label, ctrl.Status()) }
	never := func(from, to time.Time, port uint16, proto uint8) []e2ePkt {
		return rec.find(from, to, fromPort(port, proto))
	}

	t.Run("1_excluded_app_bypasses", func(t *testing.T) {
		r := launch(t, sock, 20*time.Second, excluded, "oneshot", "ex1")
		tcpLine, _ := r.line("TCP OK")
		udpLine, _ := r.line("UDP OK")
		want := "SRC " + e2ePhys.String() + ":"
		if !strings.Contains(tcpLine, want) {
			t.Errorf("excluded TCP: want server-seen source %s, got %q", e2ePhys, tcpLine)
		}
		if !strings.Contains(udpLine, want) {
			t.Errorf("excluded UDP: want server-seen source %s, got %q", e2ePhys, udpLine)
		}
		tp, up := r.port("TCP"), r.port("UDP")
		tunTCP, tunUDP := never(r.start, r.end, tp, unix.IPPROTO_TCP), never(r.start, r.end, up, unix.IPPROTO_UDP)
		logPkts(t, fmt.Sprintf("tunnel packets from excluded TCP port %d", tp), tunTCP)
		logPkts(t, fmt.Sprintf("tunnel packets from excluded UDP port %d", up), tunUDP)
		if len(tunTCP)+len(tunUDP) > 0 {
			t.Errorf("excluded app traffic leaked into the tunnel")
		}
		logEvents(t, "server events", srv.find(r.start, r.end, nil))
		routed := capt.find(r.start, r.end, func(p e2eCapPkt) bool {
			return !p.in && p.src == e2eTunAddr && (p.sport == tp || p.sport == up) && p.proto != 0
		})
		logPkts(t, "kernel routed into the TUN from the excluded ports (diverted by the engine)", routed)
		injected := capt.find(r.start, r.end, func(p e2eCapPkt) bool { return p.in && p.dst == e2eTunAddr && (p.dport == tp || p.dport == up) })
		logPkts(t, "engine wrote into the TUN towards the excluded ports", injected)
		status("excluded oneshot")
	})

	t.Run("2_normal_app_tunnelled", func(t *testing.T) {
		r := launch(t, sock, 20*time.Second, normal, "oneshot", "nm1")
		if _, ok := r.line("TCP ERR"); !ok {
			t.Errorf("normal TCP should time out (no peer behind the tunnel)")
		}
		if _, ok := r.line("UDP ERR"); !ok {
			t.Errorf("normal UDP should time out")
		}
		tp, up := r.port("TCP"), r.port("UDP")
		syns := rec.find(r.start, r.end, func(p e2ePkt) bool {
			return fromPort(tp, unix.IPPROTO_TCP)(p) && p.dst == e2eServer && p.dport == 8080 && p.flags&0x12 == 0x02
		})
		udps := rec.find(r.start, r.end, func(p e2ePkt) bool {
			return fromPort(up, unix.IPPROTO_UDP)(p) && p.dst == e2eServer && p.dport == 8080 && strings.Contains(p.payload, "nm1-udp")
		})
		logPkts(t, fmt.Sprintf("tunnel SYNs from normal TCP port %d", tp), syns)
		logPkts(t, fmt.Sprintf("tunnel datagrams from normal UDP port %d", up), udps)
		if tp == 0 || len(syns) == 0 {
			t.Errorf("normal app's SYN never reached Device.Read")
		}
		if up == 0 || len(udps) == 0 {
			t.Errorf("normal app's UDP datagram never reached Device.Read")
		}
		evs := srv.find(r.start, r.end, nil)
		logEvents(t, "server events during normal run", evs)
		if len(evs) > 0 {
			t.Errorf("server saw traffic during the normal app's run")
		}
		status("normal oneshot")
	})

	t.Run("3_excluded_dns_stays_in_tunnel", func(t *testing.T) {
		r := launch(t, sock, 20*time.Second, excluded, "dns", "dns1")
		up := r.port("DNS")
		pk := rec.find(r.start, r.end, func(p e2ePkt) bool {
			return fromPort(up, unix.IPPROTO_UDP)(p) && p.dst == e2eServer && p.dport == 53 && strings.Contains(p.payload, "dns1")
		})
		logPkts(t, fmt.Sprintf("tunnel DNS packets from excluded port %d", up), pk)
		if up == 0 || len(pk) == 0 {
			t.Errorf("excluded app's DNS query never reached Device.Read")
		}
		evs := srv.find(r.start, r.end, func(e e2eEvent) bool { return e.kind == "dns" })
		logEvents(t, "server DNS events", evs)
		if len(evs) > 0 {
			t.Errorf("excluded DNS query reached the server directly")
		}
	})

	t.Run("4_lineage", func(t *testing.T) {
		r := launch(t, sock, 20*time.Second, excluded, "spawn", child, "oneshot", "ch1")
		want := "SRC " + e2ePhys.String() + ":"
		tcpLine, _ := r.line("CHILD| TCP OK")
		udpLine, _ := r.line("CHILD| UDP OK")
		if !strings.Contains(tcpLine, want) || !strings.Contains(udpLine, want) {
			t.Errorf("child of the excluded app was not bypassed: tcp=%q udp=%q", tcpLine, udpLine)
		}
		logEvents(t, "server events for spawned child", srv.find(r.start, r.end, nil))
		tp, up := r.port("CHILD| TCP"), r.port("CHILD| UDP")
		leaked := append(never(r.start, r.end, tp, unix.IPPROTO_TCP), never(r.start, r.end, up, unix.IPPROTO_UDP)...)
		logPkts(t, "tunnel packets from spawned child", leaked)
		if len(leaked) > 0 {
			t.Errorf("spawned child's traffic leaked into the tunnel")
		}

		d := launch(t, sock, 20*time.Second, child, "oneshot", "ch2")
		if _, ok := d.line("TCP ERR"); !ok {
			t.Errorf("child binary started directly should be tunnelled (TCP)")
		}
		if _, ok := d.line("UDP ERR"); !ok {
			t.Errorf("child binary started directly should be tunnelled (UDP)")
		}
		dtp, dup := d.port("TCP"), d.port("UDP")
		direct := append(never(d.start, d.end, dtp, unix.IPPROTO_TCP), never(d.start, d.end, dup, unix.IPPROTO_UDP)...)
		logPkts(t, "tunnel packets from directly started child binary", direct)
		if len(never(d.start, d.end, dtp, unix.IPPROTO_TCP)) == 0 || len(never(d.start, d.end, dup, unix.IPPROTO_UDP)) == 0 {
			t.Errorf("directly started child binary's traffic not seen in Device.Read")
		}
		evs := srv.find(d.start, d.end, nil)
		logEvents(t, "server events for direct child", evs)
		if len(evs) > 0 {
			t.Errorf("server saw the directly started child binary")
		}
	})

	t.Run("5_fragmented_udp_bypass", func(t *testing.T) {
		r := launch(t, sock, 20*time.Second, excluded, "bigudp", "big1", "4000")
		sent, _ := r.line("BIG SENT")
		ok, _ := r.line("BIG OK")
		evs := srv.find(r.start, r.end, func(e e2eEvent) bool { return e.kind == "udp-big" && e.tag == "big1" })
		logEvents(t, "server big-datagram events", evs)
		wantSHA := ""
		if f := strings.Fields(sent); len(f) == 5 {
			wantSHA = f[4]
		}
		if len(evs) != 1 || evs[0].n != 4000 || evs[0].sha != wantSHA || evs[0].remote.Addr() != e2ePhys {
			t.Errorf("server did not get the 4000-byte datagram intact via the physical path (want sha %s)", wantSHA)
		}
		if !strings.Contains(ok, "replyLen=4000") || !strings.Contains(ok, "shaMatch=true") {
			t.Errorf("app did not get the 4000-byte reply back: %q", ok)
		}
		frags := rec.find(r.start, r.end, func(p e2ePkt) bool { return p.ver == 4 && p.src == e2eTunAddr && p.isFrag() })
		logPkts(t, "fragments seen in Device.Read", frags)
		if len(frags) > 0 {
			t.Errorf("fragments of the excluded datagram leaked into the tunnel")
		}
		out := capt.find(r.start, r.end, func(p e2eCapPkt) bool { return !p.in && p.src == e2eTunAddr && p.proto == unix.IPPROTO_UDP })
		logPkts(t, "kernel routed into the TUN (app datagram fragments)", out)
		in := capt.find(r.start, r.end, func(p e2eCapPkt) bool { return p.in && p.dst == e2eTunAddr && p.proto == unix.IPPROTO_UDP })
		logPkts(t, "engine wrote into the TUN (reply)", in)
		for _, p := range in {
			if p.length > e2eMTU {
				t.Errorf("engine injected a %d-byte packet above the TUN MTU", p.length)
			}
		}
		status("big UDP")
	})

	t.Run("6_rules_removed", func(t *testing.T) {
		held := make(chan e2eRun, 1)
		go func() {
			r, err := launchRun(sock, 40*time.Second, excluded, "hold", "hold1", "25")
			if err != nil {
				r.Out = "launcher error: " + err.Error()
			}
			held <- r
		}()
		ev, ok := srv.waitFor(10*time.Second, func(e e2eEvent) bool { return e.kind == "tcp-hello" && e.tag == "hold1" })
		if !ok {
			t.Fatalf("long-lived connection never reached the server")
		}
		t.Logf("long-lived connection established: %s", ev)
		if ev.remote.Addr() != e2ePhys {
			t.Errorf("long-lived connection was not bypassed: %s", ev)
		}
		time.Sleep(300 * time.Millisecond)
		status("hold established")
		start := time.Now()
		if errs := ctrl.SetRules(nil, []string{self}); len(errs) > 0 {
			t.Errorf("SetRules(nil): %+v", errs)
		}
		t.Logf("SetRules(nil) returned in %v", time.Since(start))
		var hr e2eRun
		select {
		case hr = <-held:
			t.Logf("hold run (pid %d, err=%q):\n%s", hr.PID, hr.Err, strings.TrimRight(hr.Out, "\n"))
		case <-time.After(30 * time.Second):
			t.Fatalf("long-lived app never finished")
		}
		resetLine, _ := hr.line("HOLD ")
		if !strings.HasPrefix(resetLine, "HOLD RESET") {
			t.Errorf("bypassed long-lived TCP connection was not reset: %q", resetLine)
		} else {
			t.Logf("app saw reset %v after SetRules(nil)", hr.end.Sub(start))
		}
		closed, _ := srv.waitFor(5*time.Second, func(e e2eEvent) bool { return e.kind == "tcp-closed" && e.tag == "hold1" })
		t.Logf("server side of long-lived connection: %s", closed)
		if !strings.Contains(closed.err, "reset") {
			t.Errorf("server side of the bypassed connection was not reset: %q", closed.err)
		}
		hp := hr.port("TCP")
		rst := capt.find(start, start.Add(2*time.Second), func(p e2eCapPkt) bool {
			return p.in && p.proto == unix.IPPROTO_TCP && p.dport == hp && p.flags&0x04 != 0
		})
		logPkts(t, "RSTs the engine wrote to the app", rst)
		status("rules removed")

		r := launch(t, sock, 20*time.Second, excluded, "oneshot", "ex2")
		if _, ok := r.line("TCP ERR"); !ok {
			t.Errorf("previously excluded app's new TCP connection should now be tunnelled")
		}
		if _, ok := r.line("UDP ERR"); !ok {
			t.Errorf("previously excluded app's new UDP flow should now be tunnelled")
		}
		tp, up := r.port("TCP"), r.port("UDP")
		tcpT, udpT := never(r.start, r.end, tp, unix.IPPROTO_TCP), never(r.start, r.end, up, unix.IPPROTO_UDP)
		logPkts(t, "tunnel TCP after rules removed", tcpT)
		logPkts(t, "tunnel UDP after rules removed", udpT)
		if len(tcpT) == 0 || len(udpT) == 0 {
			t.Errorf("previously excluded app's traffic not seen in Device.Read")
		}
		evs := srv.find(r.start, r.end, nil)
		logEvents(t, "server events after rules removed", evs)
		if len(evs) > 0 {
			t.Errorf("server saw the previously excluded app directly")
		}
	})

	t.Run("7_close_and_goroutines", func(t *testing.T) {
		capt.close()
		t.Logf("goroutines before close: %d", runtime.NumGoroutine())
		start := time.Now()
		closed := make(chan error, 1)
		go func() { closed <- wrapped.Close() }()
		select {
		case err := <-closed:
			t.Logf("Device.Close returned %v in %v", err, time.Since(start))
			if d := time.Since(start); d > time.Second {
				t.Errorf("Device.Close took %v", d)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("Device.Close hung")
		}
		select {
		case <-rec.done:
			t.Logf("reader exited with %v", rec.err)
		case <-time.After(2 * time.Second):
			t.Errorf("Device.Read did not return after Close")
		}
		cstart := time.Now()
		if err := ctrl.Close(); err != nil {
			t.Errorf("Controller.Close: %v", err)
		}
		t.Logf("Controller.Close took %v; final status %+v", time.Since(cstart), ctrl.Status())
		srv.close()
		// This subtest's own goroutine is the one expected above the parent's baseline.
		want := baseG + 1
		var n int
		for i := 0; i < 50; i++ {
			if n = runtime.NumGoroutine(); n <= want {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		var b bytes.Buffer
		pprof.Lookup("goroutine").WriteTo(&b, 1)
		t.Logf("goroutines: baseline=%d (+1 subtest) after=%d", baseG, n)
		if n > want {
			t.Errorf("goroutine leak: %d > %d\n%s", n, want, b.String())
		}
	})

	var all strings.Builder
	logs.mu.Lock()
	for _, l := range logs.lines {
		all.WriteString("\n  " + l)
	}
	logs.mu.Unlock()
	t.Logf("engine log:%s", all.String())
}
