package hysteria2

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	M "github.com/sagernet/sing/common/metadata"
	"github.com/sagernet/sing/protocol/socks"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// logRateLimit bounds how often a single pump goroutine logs repeated write
// failures, so a persistent outage floods neither the log sink nor its mutex.
const logRateLimit = 5 * time.Second

// wsaeaddrinuse is WSAEADDRINUSE (10048): Windows returns this numeric
// error, not the POSIX EADDRINUSE errors.Is otherwise matches on non-English
// systems.
const wsaeaddrinuse = 10048

// udpBridge is the loopback UDP listener WireGuard's peer Endpoint points
// at. It translates each raw UDP datagram to/from a SOCKS5 UDP ASSOCIATE
// session against the local mixed inbound, which routes it through the
// Hysteria2 tunnel to relayDestination.
type udpBridge struct {
	local  *net.UDPConn
	tunnel net.PacketConn
	target *net.UDPAddr
	logs   *state.LogStore

	peerMu sync.RWMutex
	peer   *net.UDPAddr

	wg       sync.WaitGroup
	stop     chan struct{}
	closeErr sync.Once

	dead     chan struct{}
	deadOnce sync.Once

	tunnelWriteLog rateGate
	wgWriteLog     rateGate
}

// rateGate lets at most one caller through per interval; used to keep pump
// goroutines from logging every datagram during a persistent failure.
type rateGate struct {
	mu   sync.Mutex
	last time.Time
}

func (g *rateGate) allow(interval time.Duration) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	if now := time.Now(); now.Sub(g.last) >= interval {
		g.last = now
		return true
	}
	return false
}

// newUDPBridge opens the WG-facing loopback socket, establishes a SOCKS5
// UDP ASSOCIATE session against the mixed inbound at mixedAddr, and starts
// pumping datagrams in both directions.
func newUDPBridge(ctx context.Context, logs *state.LogStore, localPort int, mixedAddr string) (*udpBridge, error) {
	localAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort}
	local, err := listenUDPWithRetry(ctx, localAddr, 10, 100*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("hysteria2: listen local udp: %w", err)
	}

	target, err := net.ResolveUDPAddr("udp", relayDestination)
	if err != nil {
		local.Close()
		return nil, fmt.Errorf("hysteria2: resolve relay destination: %w", err)
	}

	client := socks.NewClient(systemDialer{}, M.ParseSocksaddr(mixedAddr), socks.Version5, "", "")
	tunnel, err := client.ListenPacket(ctx, M.SocksaddrFromNet(target))
	if err != nil {
		local.Close()
		return nil, fmt.Errorf("hysteria2: socks5 udp associate: %w", err)
	}

	b := &udpBridge{
		local:  local,
		tunnel: tunnel,
		target: target,
		logs:   logs,
		stop:   make(chan struct{}),
		dead:   make(chan struct{}),
	}
	b.wg.Add(2)
	go b.pumpFromWG()
	go b.pumpToWG()
	return b, nil
}

// boundPort reports the loopback UDP port WireGuard should point at.
func (b *udpBridge) boundPort() int {
	if addr, ok := b.local.LocalAddr().(*net.UDPAddr); ok {
		return addr.Port
	}
	return 0
}

// markDead flags the bridge as no longer forwarding traffic after an
// unrecoverable pump failure, so Manager.Status stops reporting Running.
func (b *udpBridge) markDead() {
	b.deadOnce.Do(func() { close(b.dead) })
}

func (b *udpBridge) isDead() bool {
	select {
	case <-b.dead:
		return true
	default:
		return false
	}
}

// pumpFromWG forwards datagrams WireGuard sends to the loopback socket into
// the tunnel, pinning the first sender as the peer and ignoring any other
// local source so another process can't hijack the tunnel by sending here.
func (b *udpBridge) pumpFromWG() {
	defer b.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, src, err := b.local.ReadFromUDP(buf)
		if err != nil {
			if b.stopped() {
				return
			}
			if isRecoverableUDPErr(err) {
				continue
			}
			b.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("hysteria2 bridge: read from wireguard failed: %v", err))
			b.markDead()
			return
		}

		b.peerMu.Lock()
		if b.peer == nil {
			b.peer = src
		} else if !addrEqual(b.peer, src) {
			b.peerMu.Unlock()
			continue
		}
		b.peerMu.Unlock()

		if _, err := b.tunnel.WriteTo(buf[:n], b.target); err != nil {
			if b.stopped() {
				return
			}
			if b.tunnelWriteLog.allow(logRateLimit) {
				b.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("hysteria2 bridge: write to tunnel failed: %v", err))
			}
		}
	}
}

// pumpToWG forwards datagrams arriving from the tunnel back to whichever
// local peer last sent WireGuard traffic through this bridge.
func (b *udpBridge) pumpToWG() {
	defer b.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, _, err := b.tunnel.ReadFrom(buf)
		if err != nil {
			if b.stopped() {
				return
			}
			if isRecoverableUDPErr(err) {
				continue
			}
			b.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("hysteria2 bridge: read from tunnel failed: %v", err))
			b.markDead()
			return
		}
		b.peerMu.RLock()
		peer := b.peer
		b.peerMu.RUnlock()
		if peer == nil {
			continue
		}
		if _, err := b.local.WriteToUDP(buf[:n], peer); err != nil {
			if b.stopped() {
				return
			}
			if b.wgWriteLog.allow(logRateLimit) {
				b.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("hysteria2 bridge: write to wireguard failed: %v", err))
			}
		}
	}
}

func (b *udpBridge) stopped() bool {
	select {
	case <-b.stop:
		return true
	default:
		return false
	}
}

// Close tears down both sockets and waits for the pump goroutines to exit.
func (b *udpBridge) Close() {
	b.closeErr.Do(func() { close(b.stop) })
	b.local.Close()
	b.tunnel.Close()
	b.wg.Wait()
}

func addrEqual(a, b *net.UDPAddr) bool {
	return a.IP.Equal(b.IP) && a.Port == b.Port
}

// isRecoverableUDPErr reports transient per-datagram failures a pump should
// retry past rather than exit on — notably WSAECONNRESET, which an
// unconnected Windows UDP socket surfaces after an ICMP port-unreachable
// from an earlier write, with no bearing on the socket's own health.
func isRecoverableUDPErr(err error) bool {
	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	return false
}

// systemDialer is a minimal sing N.Dialer used only to reach the local
// mixed inbound's control port; it never leaves loopback.
type systemDialer struct{}

func (systemDialer) DialContext(ctx context.Context, network string, destination M.Socksaddr) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, destination.String())
}

func (systemDialer) ListenPacket(ctx context.Context, destination M.Socksaddr) (net.PacketConn, error) {
	var lc net.ListenConfig
	return lc.ListenPacket(ctx, "udp", "127.0.0.1:0")
}

// listenUDPWithRetry retries briefly on transient "address already in use"
// errors (e.g. a just-stopped previous instance still releasing its
// socket), honoring ctx cancellation between attempts. Mirrors cloak's
// listener retry helper.
func listenUDPWithRetry(ctx context.Context, addr *net.UDPAddr, attempts int, delay time.Duration) (*net.UDPConn, error) {
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for i := 0; i < attempts; i++ {
		conn, err := net.ListenUDP("udp", addr)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		if !isAddrInUseErr(err) {
			return nil, err
		}
		if i == attempts-1 {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, lastErr
}

func isAddrInUseErr(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EADDRINUSE) {
		return true
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == wsaeaddrinuse {
		return true
	}
	s := err.Error()
	return strings.Contains(s, "address already in use") ||
		strings.Contains(s, "Only one usage of each socket address")
}
