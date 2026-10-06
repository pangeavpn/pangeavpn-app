package transport

import (
	"context"
	"errors"
	"net"
	"os"
	"sync"
	"sync/atomic"
)

// MaxConsecutiveBridgeErrors bounds a direction that is failing every datagram,
// so a genuinely dead socket still exits instead of spinning.
const MaxConsecutiveBridgeErrors = 32

// BridgeUDP relays datagrams between WireGuard's loopback socket and a
// transport's outbound packet connection as a single-flow NAT. A delivery
// failure is dropped, not fatal: UDP is lossy and WireGuard retries. It is the
// shared data path for every sing-box outbound bridged through ListenPacket.
func BridgeUDP(ctx context.Context, local *net.UDPConn, remote net.PacketConn, remoteAddr net.Addr) error {
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
				if IsBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > MaxConsecutiveBridgeErrors {
					errCh <- err
					return
				}
				continue
			}
			// Pin the peer to whoever spoke first; a later source is rejected
			// unless it opens with a fresh WireGuard initiation.
			if known := peer.Load(); known == nil {
				peer.Store(addr)
			} else if !sameUDPAddr(known, addr) {
				if !isWireGuardInitiation(buf[:n]) {
					continue
				}
				peer.Store(addr)
			}
			if _, err := remote.WriteTo(buf[:n], remoteAddr); err != nil {
				if IsBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > MaxConsecutiveBridgeErrors {
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
				if IsBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > MaxConsecutiveBridgeErrors {
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
				if IsBridgeShutdown(err) {
					errCh <- err
					return
				}
				fails++
				if fails > MaxConsecutiveBridgeErrors {
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
		if !IsBridgeShutdown(err) {
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

// IsBridgeShutdown reports the errors that mean Stop happened, as opposed to a
// per-datagram delivery failure.
func IsBridgeShutdown(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, os.ErrClosed) ||
		errors.Is(err, context.Canceled)
}

// sameUDPAddr compares by IP and port only: Zone differs harmlessly across
// platforms for loopback traffic and would otherwise reject a valid peer.
func sameUDPAddr(a, b *net.UDPAddr) bool {
	return a.Port == b.Port && a.IP.Equal(b.IP)
}

// wireGuardInitiationLen is a WireGuard handshake-initiation datagram: message
// type 1 plus three zero reserved bytes, 148 bytes total.
const wireGuardInitiationLen = 148

// isWireGuardInitiation gates re-pinning the reply peer: a forged initiation
// only diverts ciphertext the forger cannot read.
func isWireGuardInitiation(pkt []byte) bool {
	return len(pkt) == wireGuardInitiationLen && pkt[0] == 1 && pkt[1] == 0 && pkt[2] == 0 && pkt[3] == 0
}
