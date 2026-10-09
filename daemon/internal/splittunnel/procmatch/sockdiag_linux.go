//go:build linux

package procmatch

import (
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const diagTimeout = 100 * time.Millisecond

// sockSource answers socket lookups; the classifier's tests substitute a fake.
type sockSource interface {
	tcpLookup(app, remote netip.AddrPort) ([]diagRow, error)
	dump(proto uint8, sport uint16, states uint32) ([]diagRow, error)
	close()
}

// diagConn is one persistent NETLINK_SOCK_DIAG socket; requests on it are serialised.
type diagConn struct {
	mu     sync.Mutex
	fd     int
	seq    uint32
	buf    []byte
	closed bool
}

type netlinkDiag struct {
	tcp, udp diagConn
}

func newNetlinkDiag() *netlinkDiag {
	return &netlinkDiag{tcp: diagConn{fd: -1}, udp: diagConn{fd: -1}}
}

func (n *netlinkDiag) conn(proto uint8) *diagConn {
	if proto == protoUDP {
		return &n.udp
	}
	return &n.tcp
}

func (n *netlinkDiag) tcpLookup(app, remote netip.AddrPort) ([]diagRow, error) {
	rows, err := n.tcp.request(func(seq uint32) []byte {
		return packDiagRequest(seq, afInet, protoTCP, 0, app, remote, false)
	}, false)
	if errors.Is(err, errDiagNotFound) {
		return nil, nil
	}
	return rows, err
}

// dump lists both families' sockets on sport (0 = every port); AF_INET6 dual-stack sockets
// only appear in the AF_INET6 dump.
func (n *netlinkDiag) dump(proto uint8, sport uint16, states uint32) ([]diagRow, error) {
	c := n.conn(proto)
	src := netip.AddrPortFrom(netip.Addr{}, sport)
	var out []diagRow
	for _, family := range []uint8{afInet, afInet6} {
		rows, err := c.request(func(seq uint32) []byte {
			return packDiagRequest(seq, family, proto, states, src, netip.AddrPort{}, true)
		}, true)
		if err != nil {
			if errors.Is(err, errDiagNotFound) {
				err = errDiagUnsupported
			}
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func (n *netlinkDiag) close() {
	n.tcp.close()
	n.udp.close()
}

func (c *diagConn) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	c.closeLocked()
}

func (c *diagConn) closeLocked() {
	if c.fd >= 0 {
		unix.Close(c.fd)
		c.fd = -1
	}
}

func (c *diagConn) openLocked() error {
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, unix.NETLINK_SOCK_DIAG)
	if err != nil {
		return err
	}
	tv := unix.NsecToTimeval(diagTimeout.Nanoseconds())
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		unix.Close(fd)
		return err
	}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &tv); err != nil {
		unix.Close(fd)
		return err
	}
	if err := unix.Connect(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		unix.Close(fd)
		return err
	}
	c.fd = fd
	if c.buf == nil {
		c.buf = make([]byte, 64<<10)
	}
	return nil
}

// request sends one request and collects its replies; a failed exchange reopens the socket
// once so a stale reply can never answer a later request.
func (c *diagConn) request(pack func(seq uint32) []byte, dump bool) ([]diagRow, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var err error
	for attempt := 0; attempt < 2; attempt++ {
		if c.closed {
			return nil, unix.EBADF
		}
		if c.fd < 0 {
			if err = c.openLocked(); err != nil {
				return nil, fmt.Errorf("sock_diag socket: %w", err)
			}
		}
		c.seq++
		var rows []diagRow
		rows, err = c.exchangeLocked(pack(c.seq), c.seq, dump)
		if err == nil || errors.Is(err, errDiagNotFound) {
			return rows, err
		}
		c.closeLocked()
	}
	return nil, err
}

func (c *diagConn) exchangeLocked(req []byte, seq uint32, dump bool) ([]diagRow, error) {
	if _, err := unix.Write(c.fd, req); err != nil {
		return nil, err
	}
	var rows []diagRow
	for {
		n, _, err := unix.Recvfrom(c.fd, c.buf, 0)
		if err != nil {
			if err == unix.EINTR {
				continue
			}
			return nil, err
		}
		done := false
		var rerr error
		werr := walkNetlink(c.buf[:n], func(typ uint16, mseq uint32, data []byte) bool {
			if mseq != seq {
				return true
			}
			switch typ {
			case nlmsgDone:
				done = true
				if e := netlinkErrno(data); e > 0 {
					rerr = unix.Errno(e)
				}
				return false
			case nlmsgError:
				done = true
				switch e := netlinkErrno(data); {
				case e == int(unix.ENOENT) || e == int(unix.ESRCH):
					rerr = errDiagNotFound
				case e > 0:
					rerr = unix.Errno(e)
				case e < 0:
					rerr = errDiagMalformed
				}
				return false
			case sockDiagByFamily:
				if r, ok := parseDiagMsg(data); ok {
					rows = append(rows, r)
				}
				if !dump {
					done = true
					return false
				}
			}
			return true
		})
		if werr != nil {
			return nil, werr
		}
		if done {
			return rows, rerr
		}
	}
}
