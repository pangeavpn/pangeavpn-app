package shadowsocks

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

// maxConsecutiveBridgeErrors bounds a direction that is failing every datagram,
// so a genuinely dead socket still exits instead of spinning.
const maxConsecutiveBridgeErrors = 32

// bridgeUDP relays datagrams between WireGuard's loopback socket and the SS
// outbound. One remote peer, so the local side is a single-flow NAT.
//
// A datagram that cannot be delivered — oversized for the path (EMSGSIZE), or
// an ICMP-driven reset — is dropped, not fatal: UDP is lossy by contract and
// WireGuard retransmits. Tearing the transport down per packet would put the
// health check into a restart loop instead.
func bridgeUDP(ctx context.Context, local *net.UDPConn, remote net.PacketConn, remoteAddr net.Addr) error {
	errCh := make(chan error, 2)
	var peer atomic.Pointer[net.UDPAddr]
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		buf := make([]byte, 65535)
		fails := 0
		for {
			n, addr, err := local.ReadFromUDP(buf)
			if err != nil {
				if isBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > maxConsecutiveBridgeErrors {
					errCh <- err
					return
				}
				continue
			}
			// Pin the peer to whoever spoke first; anything else is another
			// local process trying to hijack the return path.
			if known := peer.Load(); known == nil {
				peer.Store(addr)
			} else if !sameUDPAddr(known, addr) {
				continue
			}
			if _, err := remote.WriteTo(buf[:n], remoteAddr); err != nil {
				if isBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > maxConsecutiveBridgeErrors {
					errCh <- err
					return
				}
				continue
			}
			fails = 0
		}
	}()

	go func() {
		defer wg.Done()
		buf := make([]byte, 65535)
		fails := 0
		for {
			n, _, err := remote.ReadFrom(buf)
			if err != nil {
				if isBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > maxConsecutiveBridgeErrors {
					errCh <- err
					return
				}
				continue
			}
			dst := peer.Load()
			if dst == nil {
				continue // no local client observed yet, drop
			}
			if _, err := local.WriteToUDP(buf[:n], dst); err != nil {
				if isBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > maxConsecutiveBridgeErrors {
					errCh <- err
					return
				}
				continue
			}
			fails = 0
		}
	}()

	var result error
	select {
	case <-ctx.Done():
		result = nil
	case err := <-errCh:
		if !isBridgeShutdown(err) {
			result = err
		}
	}

	// Either exit path must close both sockets before returning: that is what
	// unblocks whichever direction's goroutine is still parked in a read.
	local.Close()
	remote.Close()
	wg.Wait()
	return result
}

// isBridgeShutdown reports the errors that mean Stop happened, as opposed to a
// per-datagram delivery failure.
func isBridgeShutdown(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, context.Canceled)
}

// sameUDPAddr compares by IP and port only: Zone differs harmlessly across
// platforms for loopback traffic and would otherwise reject a valid peer.
func sameUDPAddr(a, b *net.UDPAddr) bool {
	return a.Port == b.Port && a.IP.Equal(b.IP)
}
