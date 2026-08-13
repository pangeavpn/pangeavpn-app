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
}

// newUDPBridge opens the WG-facing loopback socket, establishes a SOCKS5
// UDP ASSOCIATE session against the mixed inbound at mixedAddr, and starts
// pumping datagrams in both directions.
func newUDPBridge(ctx context.Context, logs *state.LogStore, localPort int, mixedAddr string, targetPort int) (*udpBridge, error) {
	localAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort}
	local, err := listenUDPWithRetry(localAddr, 10, 100*time.Millisecond)
	if err != nil {
		return nil, fmt.Errorf("hysteria2: listen local udp: %w", err)
	}

	target, err := net.ResolveUDPAddr("udp", relayDestination(targetPort))
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

// pumpFromWG forwards datagrams WireGuard sends to the loopback socket into
// the tunnel, remembering the sender so responses can be routed back.
func (b *udpBridge) pumpFromWG() {
	defer b.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, src, err := b.local.ReadFromUDP(buf)
		if err != nil {
			return
		}
		b.peerMu.Lock()
		b.peer = src
		b.peerMu.Unlock()

		if _, err := b.tunnel.WriteTo(buf[:n], b.target); err != nil {
			if b.stopped() {
				return
			}
			b.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("hysteria2 bridge: write to tunnel failed: %v", err))
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
			b.logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("hysteria2 bridge: write to wireguard failed: %v", err))
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
// socket). Mirrors cloak's listener retry helper.
func listenUDPWithRetry(addr *net.UDPAddr, attempts int, delay time.Duration) (*net.UDPConn, error) {
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
		time.Sleep(delay)
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
	s := err.Error()
	return strings.Contains(s, "address already in use") ||
		strings.Contains(s, "Only one usage of each socket address")
}
