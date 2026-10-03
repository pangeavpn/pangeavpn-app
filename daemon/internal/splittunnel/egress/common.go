package egress

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sync"
	"syscall"
	"time"
)

// ErrBrokerUnavailable is returned without waiting while the macOS socket broker is down.
var ErrBrokerUnavailable = errors.New("split tunnelling egress broker unavailable")

var errNotIPv4 = errors.New("split tunnelling egress dials IPv4 only")

var keepAliveConfig = net.KeepAliveConfig{
	Enable:   true,
	Idle:     30 * time.Second,
	Interval: 10 * time.Second,
	Count:    3,
}

// IsUnreachable reports whether err says the route or interface a socket was pinned to is gone.
func IsUnreachable(err error) bool {
	var errno syscall.Errno
	if err == nil || !errors.As(err, &errno) {
		return false
	}
	return slices.Contains(unreachableErrnos, errno)
}

func tcpTarget(dst netip.AddrPort) (string, error) {
	addr := dst.Addr().Unmap()
	if !addr.Is4() || addr.IsUnspecified() || dst.Port() == 0 {
		return "", fmt.Errorf("egress: %w: %s", errNotIPv4, dst)
	}
	return netip.AddrPortFrom(addr, dst.Port()).String(), nil
}

func sortedV4(addrs []netip.Addr) []netip.Addr {
	out := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		if a = a.Unmap(); a.Is4() && !slices.Contains(out, a) {
			out = append(out, a)
		}
	}
	slices.SortFunc(out, func(a, b netip.Addr) int { return a.Compare(b) })
	return out
}

func cloneIdentity(id Identity) Identity {
	id.Addrs = slices.Clone(id.Addrs)
	return id
}

// identityCache holds the last resolved physical interface; only refresh resolves.
type identityCache struct {
	resolve   func() (Identity, error)
	refreshMu sync.Mutex
	mu        sync.Mutex
	cur       Identity
	err       error
}

func newIdentityCache(resolve func() (Identity, error)) *identityCache {
	return &identityCache{resolve: resolve, err: ErrNoInterface}
}

func (c *identityCache) current() (Identity, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return Identity{}, c.err
	}
	return cloneIdentity(c.cur), nil
}

func (c *identityCache) refresh() (old, cur Identity, changed bool, err error) {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	id, err := c.resolve()
	c.mu.Lock()
	defer c.mu.Unlock()
	hadOld := c.err == nil
	if hadOld {
		old = cloneIdentity(c.cur)
	}
	// A failed lookup is not a lost route: keep pinning to the last interface until one says so.
	if err != nil && hadOld && !errors.Is(err, ErrNoInterface) {
		return old, cloneIdentity(old), false, err
	}
	if err != nil {
		c.cur, c.err = Identity{}, err
		return old, Identity{}, hadOld, err
	}
	c.cur, c.err = cloneIdentity(id), nil
	return old, cloneIdentity(id), !hadOld || !old.Equal(id), nil
}

func controlFD(c syscall.RawConn, fn func(fd uintptr) error) error {
	var opErr error
	if err := c.Control(func(fd uintptr) { opErr = fn(fd) }); err != nil {
		return err
	}
	return opErr
}

func connControl(c syscall.Conn, fn func(fd uintptr) error) error {
	raw, err := c.SyscallConn()
	if err != nil {
		return err
	}
	return controlFD(raw, fn)
}

func dialControl(fn func(fd uintptr) error) func(network, address string, c syscall.RawConn) error {
	return func(_, _ string, c syscall.RawConn) error { return controlFD(c, fn) }
}

// rateLog writes at most one line per class per minute.
type rateLog struct {
	logf func(format string, args ...any)
	mu   sync.Mutex
	last map[string]time.Time
}

func newRateLog(logf func(format string, args ...any)) *rateLog {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &rateLog{logf: logf, last: make(map[string]time.Time)}
}

func (l *rateLog) log(class, format string, args ...any) {
	l.mu.Lock()
	now := time.Now()
	if t, ok := l.last[class]; ok && now.Sub(t) < time.Minute {
		l.mu.Unlock()
		return
	}
	l.last[class] = now
	l.mu.Unlock()
	l.logf(format, args...)
}
