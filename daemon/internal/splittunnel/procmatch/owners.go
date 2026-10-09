package procmatch

import (
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	protoTCP = 6
	protoUDP = 17

	tcpStateListen   = 2
	tcpStateTimeWait = 11
)

type tcpRow struct {
	state  uint32
	local  netip.AddrPort
	remote netip.AddrPort
	pid    int
}

type udpRow struct {
	local netip.AddrPort
	pid   int
}

// tcpOwner returns the pid behind every live row with the exact 4-tuple; differing owners or no row fail.
func tcpOwner(rows []tcpRow, app, remote netip.AddrPort) (int, bool) {
	pid, found := 0, false
	for _, r := range rows {
		if r.state == tcpStateListen || r.state == tcpStateTimeWait || r.local != app {
			continue
		}
		if remote.IsValid() && r.remote != remote {
			continue
		}
		if found && r.pid != pid {
			return 0, false
		}
		pid, found = r.pid, true
	}
	return pid, found
}

// udpOwner considers rows bound to the app address or the wildcard on the app port; a wildcard
// socket's datagrams carry the same source as an exact bind, so all of them must agree.
func udpOwner(rows []udpRow, app netip.AddrPort) (int, bool) {
	pid, found := 0, false
	for _, r := range rows {
		if r.local.Port() != app.Port() {
			continue
		}
		if a := r.local.Addr(); a != app.Addr() && !a.IsUnspecified() {
			continue
		}
		if found && r.pid != pid {
			return 0, false
		}
		pid, found = r.pid, true
	}
	return pid, found
}

func flowOwner(tcp []tcpRow, udp []udpRow, f FlowID) (int, bool) {
	switch f.Proto {
	case protoTCP:
		return tcpOwner(tcp, f.App, f.Remote)
	case protoUDP:
		return udpOwner(udp, f.App)
	}
	return 0, false
}

// ntToDos maps an NT device path (\Device\HarddiskVolume3\x) to its drive form using a
// lower-cased device → "c:" table.
func ntToDos(devices map[string]string, nt string) (string, bool) {
	lower := strings.ToLower(nt)
	for dev, drive := range devices {
		if rest, ok := strings.CutPrefix(lower, dev); ok && strings.HasPrefix(rest, `\`) {
			return drive + rest, true
		}
	}
	return "", false
}

const logEvery = 60 * time.Second

// logLimiter keeps error logging to one line per class per minute.
type logLimiter struct {
	mu   sync.Mutex
	last map[string]time.Time
}

func (l *logLimiter) logf(logf func(string, ...any), class, format string, args ...any) {
	if logf == nil {
		return
	}
	l.mu.Lock()
	now := time.Now()
	if t, ok := l.last[class]; ok && now.Sub(t) < logEvery {
		l.mu.Unlock()
		return
	}
	if l.last == nil {
		l.last = make(map[string]time.Time)
	}
	l.last[class] = now
	l.mu.Unlock()
	logf(format, args...)
}

func addrPortOf(a net.Addr) netip.AddrPort {
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
