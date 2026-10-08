package splittunnel

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/sagernet/gvisor/pkg/tcpip"
	"github.com/sagernet/gvisor/pkg/tcpip/adapters/gonet"
	"github.com/sagernet/gvisor/pkg/tcpip/transport/tcp"
	"github.com/sagernet/gvisor/pkg/waiter"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

const spliceBufSize = 16 << 10

var splicePool = sync.Pool{New: func() any {
	b := make([]byte, spliceBufSize)
	return &b
}}

// handleTCP is the forwarder handler. The dial happens before the handshake with the app
// completes (lazy handshake), so a failed dial reaches the app as a refusal.
func (e *engine) handleTCP(r *tcp.ForwarderRequest) {
	defer e.recoverFault()
	id := r.ID()
	key := flowKey{
		srcPort: id.RemotePort,
		dst:     netip.AddrPortFrom(netip.AddrFrom4(id.LocalAddress.As4()), id.LocalPort),
	}
	f := e.takeDialMark(key)
	if f == nil {
		r.Complete(true)
		return
	}
	conn, err := e.dial(key.dst)
	if err != nil {
		r.Complete(true)
		e.finishFlow(f, e.lim.dialFailLinger)
		return
	}
	if !e.attach(f, nil, conn) {
		resetConn(conn)
		r.Complete(true)
		e.finishFlow(f, e.lim.dialFailLinger)
		return
	}
	var wq waiter.Queue
	created := make(chan struct{})
	stop := context.AfterFunc(e.d.ctx, func() { e.abortHandshakes(created) })
	ep, terr := r.CreateEndpoint(&wq)
	close(created)
	stop()
	if terr != nil {
		r.Complete(true)
		resetConn(conn)
		e.finishFlow(f, e.lim.dialFailLinger)
		return
	}
	r.Complete(false)
	ep.SocketOptions().SetKeepAlive(true)
	if !e.attach(f, ep, conn) {
		ep.Abort()
		resetConn(conn)
		e.finishFlow(f, e.lim.linger)
		return
	}
	e.splice(f, gonet.NewTCPConn(&wq, ep), ep, conn)
}

// takeDialMark consumes the mark the worker set when it injected a freshly classified bypass SYN.
func (e *engine) takeDialMark(key flowKey) *flow {
	e.mu.Lock()
	defer e.mu.Unlock()
	f := e.tcp[key]
	if e.closed || f == nil || f.state != stateBypass || !f.awaitingDial || f.aborted {
		return nil
	}
	f.awaitingDial = false
	return f
}

// abortHandshakes runs once the device is closing: a handshake that registers after
// Stack.Close has aborted the others would otherwise wait forever.
func (e *engine) abortHandshakes(created <-chan struct{}) {
	t := time.NewTicker(10 * time.Millisecond)
	defer t.Stop()
	for {
		for _, ep := range e.stack.RegisteredEndpoints() {
			ep.Abort()
		}
		select {
		case <-created:
			return
		case <-t.C:
		}
	}
}

func (e *engine) dial(dst netip.AddrPort) (net.Conn, error) {
	dl := e.c.egressDialer()
	if dl == nil {
		return nil, egress.ErrUnsupported
	}
	ctx, cancel := context.WithTimeout(e.d.ctx, e.lim.dialTimeout)
	defer cancel()
	conn, err := dl.DialTCP(ctx, dst)
	if err != nil && egress.IsUnreachable(err) && ctx.Err() == nil {
		if rerr := e.c.refreshNetwork(); rerr == nil {
			conn, err = dl.DialTCP(ctx, dst)
		}
	}
	if err != nil {
		if e.d.ctx.Err() == nil {
			e.c.log.limited("dial", "split tunnel: off-tunnel connect failed: %s", errClass(err))
		}
		return nil, err
	}
	if e.d.ctx.Err() != nil {
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	return conn, nil
}

// attach records the splice's endpoints on the flow unless it was reset meanwhile.
func (e *engine) attach(f *flow, ep tcpip.Endpoint, conn net.Conn) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed || f.aborted {
		return false
	}
	f.ep, f.phys = ep, conn
	return true
}

// splice copies both directions: EOF half-closes the other side, a reset or error on
// one side resets the other, and the flow lingers once both directions are done.
func (e *engine) splice(f *flow, app *gonet.TCPConn, ep tcpip.Endpoint, phys net.Conn) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer e.recoverFault()
		bp := splicePool.Get().(*[]byte)
		defer splicePool.Put(bp)
		buf := *bp
		for {
			n, err := phys.Read(buf)
			if n > 0 {
				if _, werr := app.Write(buf[:n]); werr != nil {
					resetConn(phys)
					return
				}
			}
			if err != nil {
				if errors.Is(err, io.EOF) {
					_ = app.CloseWrite()
				} else {
					ep.Abort()
				}
				return
			}
		}
	}()
	bp := splicePool.Get().(*[]byte)
	buf := *bp
	for {
		n, err := app.Read(buf)
		if n > 0 {
			if _, werr := phys.Write(buf[:n]); werr != nil {
				ep.Abort()
				resetConn(phys)
				break
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				closeWrite(phys)
			} else {
				resetConn(phys)
			}
			break
		}
	}
	splicePool.Put(bp)
	<-done
	_ = app.Close()
	_ = phys.Close()
	e.finishFlow(f, e.lim.linger)
}

func resetConn(c net.Conn) {
	if l, ok := c.(interface{ SetLinger(int) error }); ok {
		_ = l.SetLinger(0)
	}
	_ = c.Close()
}

func closeWrite(c net.Conn) {
	if cw, ok := c.(interface{ CloseWrite() error }); ok && cw.CloseWrite() == nil {
		return
	}
	_ = c.Close()
}

// errClass reduces a dial error to its class so logs never carry addresses.
func errClass(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled), errors.Is(err, net.ErrClosed):
		return "cancelled"
	case egress.IsUnreachable(err):
		return "unreachable"
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		return errno.Error()
	}
	return "error"
}
