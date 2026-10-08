//go:build darwin || linux

package egress

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// SOCK_SEQPACKET is not available for AF_UNIX on darwin, so the broker speaks
// length-prefixed frames over SOCK_STREAM and queues received descriptors in order.
type frameConn struct {
	c    *net.UnixConn
	wmu  sync.Mutex
	buf  []byte
	fds  []int
	rbuf []byte
	oob  []byte
}

// The oob buffer holds 64 separate one-descriptor messages: a truncated read tears the whole
// broker session down, so a burst must never outgrow it.
func newFrameConn(c *net.UnixConn) *frameConn {
	return &frameConn{c: c, rbuf: make([]byte, 4096), oob: make([]byte, 64*unix.CmsgSpace(4))}
}

// frameWriteTimeout bounds a write to a peer that stopped reading, which would otherwise hold
// wmu forever and stall every request queued behind it.
const frameWriteTimeout = 10 * time.Second

func (f *frameConn) readFrame() ([]byte, error) {
	for {
		body, rest, ok, err := splitFrame(f.buf)
		if err != nil {
			return nil, err
		}
		if ok {
			out := append([]byte(nil), body...)
			f.buf = append(f.buf[:0], rest...)
			return out, nil
		}
		n, oobn, flags, _, err := f.c.ReadMsgUnix(f.rbuf, f.oob)
		if oobn > 0 {
			f.fds = append(f.fds, parseRights(f.oob[:oobn])...)
		}
		if err != nil {
			return nil, err
		}
		if flags&unix.MSG_CTRUNC != 0 {
			return nil, fmt.Errorf("%w: control data truncated", errBrokerProtocol)
		}
		if n <= 0 && oobn == 0 {
			return nil, io.EOF
		}
		f.buf = append(f.buf, f.rbuf[:max(n, 0)]...)
	}
}

func parseRights(oob []byte) []int {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil
	}
	var fds []int
	for i := range msgs {
		if rights, err := unix.ParseUnixRights(&msgs[i]); err == nil {
			fds = append(fds, rights...)
		}
	}
	return fds
}

func (f *frameConn) takeFD() (int, bool) {
	if len(f.fds) == 0 {
		return -1, false
	}
	fd := f.fds[0]
	f.fds = f.fds[1:]
	return fd, true
}

func (f *frameConn) closeFDs() {
	for _, fd := range f.fds {
		unix.Close(fd)
	}
	f.fds = nil
}

// writeFrame sends frame, attaching file's descriptor to its first byte when file is non-nil.
func (f *frameConn) writeFrame(frame []byte, file *os.File) error {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	_ = f.c.SetWriteDeadline(time.Now().Add(frameWriteTimeout))
	defer func() { _ = f.c.SetWriteDeadline(time.Time{}) }()
	if file == nil {
		_, err := f.c.Write(frame)
		return err
	}
	raw, err := file.SyscallConn()
	if err != nil {
		return err
	}
	var n int
	var werr error
	if err := raw.Control(func(fd uintptr) {
		n, _, werr = f.c.WriteMsgUnix(frame, unix.UnixRights(int(fd)), nil)
	}); err != nil {
		return err
	}
	if werr != nil {
		return werr
	}
	if n < len(frame) {
		_, werr = f.c.Write(frame[n:])
	}
	return werr
}

type brokerDialFunc func(ctx context.Context, req brokerRequest) (*os.File, error)

const brokerMaxInFlight = 1024

// serveBroker answers requests until the peer closes; nil means a clean EOF.
func serveBroker(c *net.UnixConn, dial brokerDialFunc) error {
	fc := newFrameConn(c)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	slots := make(chan struct{}, brokerMaxInFlight)
	for {
		body, err := fc.readFrame()
		fc.closeFDs()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		req, err := decodeRequest(body)
		if err != nil || req.ID == 0 {
			continue
		}
		select {
		case slots <- struct{}{}:
		default:
			reply(fc, responseFor(req.ID, errors.New("too many requests")), nil)
			continue
		}
		go func() {
			defer func() { <-slots }()
			file, err := dial(ctx, req)
			reply(fc, responseFor(req.ID, err), file)
			if file != nil {
				file.Close()
			}
		}()
	}
}

func reply(fc *frameConn, resp brokerResponse, file *os.File) {
	if resp.Err != "" {
		file = nil
	}
	frame, err := encodeFrame(resp)
	if err != nil {
		return
	}
	_ = fc.writeFrame(frame, file)
}

func brokerDial(pin func(fd uintptr, ifindex int) error) brokerDialFunc {
	return func(ctx context.Context, req brokerRequest) (*os.File, error) {
		dst, err := req.validate()
		if err != nil {
			return nil, err
		}
		control := dialControl(func(fd uintptr) error { return pin(fd, req.IfIndex) })
		if req.Op == brokerOpUDP {
			lc := net.ListenConfig{Control: control}
			pc, err := lc.ListenPacket(ctx, "udp4", "0.0.0.0:0")
			if err != nil {
				return nil, err
			}
			defer pc.Close()
			return pc.(*net.UDPConn).File()
		}
		ctx, cancel := context.WithTimeout(ctx, req.timeout())
		defer cancel()
		nd := net.Dialer{KeepAliveConfig: keepAliveConfig, Control: control}
		c, err := nd.DialContext(ctx, "tcp4", dst.String())
		if err != nil {
			return nil, err
		}
		defer c.Close()
		return c.(*net.TCPConn).File()
	}
}

func runBrokerFD(fd uintptr, pin func(fd uintptr, ifindex int) error) int {
	f := os.NewFile(fd, "split-egress-broker")
	if f == nil {
		return 2
	}
	c, err := net.FileConn(f)
	f.Close()
	if err != nil {
		return 2
	}
	uc, ok := c.(*net.UnixConn)
	if !ok {
		c.Close()
		return 2
	}
	defer uc.Close()
	if err := serveBroker(uc, brokerDial(pin)); err != nil {
		return 1
	}
	return 0
}

type brokerProc struct {
	conn   *net.UnixConn
	pid    int
	exited <-chan struct{}
	kill   func()
}

// stop closes the broker's socket (it exits on EOF) and kills it if it lingers.
func (p *brokerProc) stop() {
	p.conn.Close()
	select {
	case <-p.exited:
	case <-time.After(2 * time.Second):
		p.kill()
		<-p.exited
	}
}

func socketPair() (parent, child *os.File, err error) {
	syscall.ForkLock.RLock()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	if err == nil {
		unix.CloseOnExec(fds[0])
		unix.CloseOnExec(fds[1])
	}
	syscall.ForkLock.RUnlock()
	if err != nil {
		return nil, nil, os.NewSyscallError("socketpair", err)
	}
	return os.NewFile(uintptr(fds[0]), "split-egress-broker"), os.NewFile(uintptr(fds[1]), "split-egress-broker-child"), nil
}

// spawnBroker starts exe with one end of a socket pair as fd 3.
func spawnBroker(exe string, args, env []string, cred *syscall.Credential) (*brokerProc, error) {
	parent, child, err := socketPair()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = env
	cmd.Dir = "/"
	cmd.ExtraFiles = []*os.File{child}
	cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	err = cmd.Start()
	child.Close()
	if err != nil {
		parent.Close()
		return nil, err
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	kill := func() { _ = cmd.Process.Kill() }
	c, err := net.FileConn(parent)
	parent.Close()
	if err != nil {
		kill()
		<-exited
		return nil, err
	}
	return &brokerProc{conn: c.(*net.UnixConn), pid: cmd.Process.Pid, exited: exited, kill: kill}, nil
}

type brokerResult struct {
	file *os.File
	err  error
}

// brokerClient matches responses to requests and keeps one broker process running.
type brokerClient struct {
	spawn      func() (*brokerProc, error)
	log        *rateLog
	minBackoff time.Duration
	maxBackoff time.Duration
	stableRun  time.Duration

	mu      sync.Mutex
	fc      *frameConn
	pending map[uint64]chan brokerResult
	nextID  uint64
	closed  bool
	strays  int

	stopCh chan struct{}
	done   chan struct{}
}

func newBrokerClient(spawn func() (*brokerProc, error), log *rateLog) *brokerClient {
	return &brokerClient{
		spawn:      spawn,
		log:        log,
		minBackoff: 500 * time.Millisecond,
		maxBackoff: 30 * time.Second,
		stableRun:  time.Minute,
		pending:    make(map[uint64]chan brokerResult),
		stopCh:     make(chan struct{}),
		done:       make(chan struct{}),
	}
}

// start spawns the first broker synchronously so a broken install fails New.
func (b *brokerClient) start() error {
	proc, err := b.spawn()
	if err != nil {
		close(b.done)
		return err
	}
	b.up(proc)
	go b.supervise(proc)
	return nil
}

func (b *brokerClient) up(proc *brokerProc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.fc = newFrameConn(proc.conn)
}

func (b *brokerClient) down() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.fc != nil {
		b.fc.closeFDs()
		b.fc = nil
	}
	for id, ch := range b.pending {
		ch <- brokerResult{err: ErrBrokerUnavailable}
		delete(b.pending, id)
	}
}

func (b *brokerClient) supervise(proc *brokerProc) {
	defer close(b.done)
	backoff := b.minBackoff
	for {
		started := time.Now()
		b.mu.Lock()
		fc := b.fc
		b.mu.Unlock()
		err := b.readLoop(fc)
		b.down()
		proc.stop()
		if b.stopped() {
			return
		}
		if time.Since(started) >= b.stableRun {
			backoff = b.minBackoff
		}
		b.log.log("exit", "split egress broker stopped (%v); restarting", err)
		for {
			if !b.sleep(backoff) {
				return
			}
			backoff = min(backoff*2, b.maxBackoff)
			if proc, err = b.spawn(); err == nil {
				break
			}
			b.log.log("spawn", "split egress broker failed to start: %v", err)
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			proc.stop()
			return
		}
		b.fc = newFrameConn(proc.conn)
		b.mu.Unlock()
	}
}

func (b *brokerClient) stopped() bool {
	select {
	case <-b.stopCh:
		return true
	default:
		return false
	}
}

func (b *brokerClient) sleep(d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-b.stopCh:
		return false
	}
}

func (b *brokerClient) readLoop(fc *frameConn) error {
	for {
		body, err := fc.readFrame()
		if err != nil {
			return err
		}
		resp, err := decodeResponse(body)
		if err != nil {
			return err
		}
		var file *os.File
		if resp.Err == "" {
			fd, ok := fc.takeFD()
			if !ok {
				return fmt.Errorf("%w: response without descriptor", errBrokerProtocol)
			}
			file = os.NewFile(uintptr(fd), "split-egress")
		}
		b.mu.Lock()
		ch, ok := b.pending[resp.ID]
		delete(b.pending, resp.ID)
		if !ok && file != nil {
			b.strays++
		}
		b.mu.Unlock()
		if !ok {
			if file != nil {
				file.Close()
			}
			continue
		}
		ch <- brokerResult{file: file, err: resp.error()}
	}
}

func requestTimeout(ctx context.Context) time.Duration {
	if deadline, ok := ctx.Deadline(); ok {
		return min(max(time.Until(deadline), time.Millisecond), brokerMaxTimeout)
	}
	return brokerDefaultTimeout
}

// request asks the broker for a socket; a reply that arrives after ctx ends has its descriptor closed.
func (b *brokerClient) request(ctx context.Context, op string, ifindex int, addr string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	timeout := requestTimeout(ctx)
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil, net.ErrClosed
	}
	fc := b.fc
	if fc == nil {
		b.mu.Unlock()
		return nil, ErrBrokerUnavailable
	}
	b.nextID++
	id := b.nextID
	ch := make(chan brokerResult, 1)
	b.pending[id] = ch
	b.mu.Unlock()

	frame, err := encodeFrame(brokerRequest{ID: id, Op: op, IfIndex: ifindex, Addr: addr, TimeoutMs: timeout.Milliseconds()})
	if err == nil {
		err = fc.writeFrame(frame, nil)
		if err != nil {
			err = fmt.Errorf("%w: %v", ErrBrokerUnavailable, err)
		}
	}
	if err != nil {
		b.forget(id, ch)
		return nil, err
	}
	timer := time.NewTimer(timeout + 5*time.Second)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.file, r.err
	case <-ctx.Done():
		b.forget(id, ch)
		return nil, ctx.Err()
	case <-timer.C:
		b.forget(id, ch)
		return nil, fmt.Errorf("%w: request timed out", ErrBrokerUnavailable)
	}
}

func (b *brokerClient) dialTCP(ctx context.Context, ifindex int, target string) (net.Conn, error) {
	file, err := b.request(ctx, brokerOpTCP, ifindex, target)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	c, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	tc, ok := c.(*net.TCPConn)
	if !ok {
		c.Close()
		return nil, fmt.Errorf("%w: not a TCP socket", errBrokerProtocol)
	}
	_ = tc.SetKeepAliveConfig(keepAliveConfig)
	return tc, nil
}

func (b *brokerClient) listenUDP(ctx context.Context, ifindex int) (net.PacketConn, error) {
	file, err := b.request(ctx, brokerOpUDP, ifindex, "")
	if err != nil {
		return nil, err
	}
	defer file.Close()
	pc, err := net.FilePacketConn(file)
	if err != nil {
		return nil, err
	}
	uc, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return nil, fmt.Errorf("%w: not a UDP socket", errBrokerProtocol)
	}
	return uc, nil
}

func (b *brokerClient) forget(id uint64, ch chan brokerResult) {
	b.mu.Lock()
	_, pending := b.pending[id]
	delete(b.pending, id)
	b.mu.Unlock()
	if pending {
		return
	}
	if r := <-ch; r.file != nil {
		r.file.Close()
	}
}

func (b *brokerClient) strayCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.strays
}

func (b *brokerClient) close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	close(b.stopCh)
	fc := b.fc
	b.mu.Unlock()
	if fc != nil {
		fc.c.Close()
	}
	select {
	case <-b.done:
	case <-time.After(3 * time.Second):
	}
	return nil
}
