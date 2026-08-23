package reality

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
)

// bridgeUDP relays WireGuard's UDP datagrams between the local loopback
// socket (WireGuard's peer Endpoint) and the REALITY outbound's packet
// connection — a single virtual UDP session carried inside the VLESS+TLS
// stream (see manager.go). WireGuard has exactly one remote peer, so the
// local side is a single-flow NAT: track the last sender and mirror
// responses back to it. remoteAddr is the fixed destination (the node's
// local WireGuard listener) every datagram is addressed to on the wire.
//
// Returns nil on a clean shutdown (ctx cancelled or a socket closed as part
// of Stop), or the first unexpected I/O error otherwise. Closes both local
// and remote on every return path, so a blocked reader on the other goroutine
// is always kicked loose instead of leaking.
func bridgeUDP(ctx context.Context, local *net.UDPConn, remote net.PacketConn, remoteAddr net.Addr, debugf func(format string, args ...any)) error {
	defer local.Close()
	defer remote.Close()
	if debugf == nil {
		debugf = func(string, ...any) {}
	}

	errCh := make(chan error, 2)
	var peer atomic.Pointer[net.UDPAddr]
	var sent, received, dropped atomic.Int64

	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := local.ReadFromUDP(buf)
			if err != nil {
				errCh <- err
				return
			}
			if !acceptFromPeer(&peer, addr, buf[:n]) {
				if dropped.Add(1) == 1 {
					debugf("bridge dropped a datagram from unpinned local source %s (%dB)", addr, n)
				}
				continue
			}
			if _, err := remote.WriteTo(buf[:n], remoteAddr); err != nil {
				errCh <- err
				return
			}
			if sent.Add(1) == 1 {
				debugf("bridge forwarded first local→remote datagram (%dB from %s)", n, addr)
			}
		}
	}()

	go func() {
		buf := make([]byte, 65535)
		for {
			n, _, err := remote.ReadFrom(buf)
			if err != nil {
				errCh <- err
				return
			}
			dst := peer.Load()
			if dst == nil {
				continue // no local client observed yet, drop
			}
			if _, err := local.WriteToUDP(buf[:n], dst); err != nil {
				errCh <- err
				return
			}
			if received.Add(1) == 1 {
				debugf("bridge forwarded first remote→local datagram (%dB to %s)", n, dst)
			}
		}
	}()

	finish := func(err error) error {
		debugf("bridge closed: %d sent, %d received, %d dropped", sent.Load(), received.Load(), dropped.Load())
		return err
	}
	select {
	case <-ctx.Done():
		return finish(nil)
	case err := <-errCh:
		if errors.Is(err, net.ErrClosed) {
			return finish(nil)
		}
		return finish(err)
	}
}

// wireGuardInitiationLen is a WireGuard handshake-initiation datagram: message
// type 1 plus three zero reserved bytes, 148 bytes total.
const wireGuardInitiationLen = 148

// acceptFromPeer pins the loopback reply peer to whichever address sends the
// first datagram, rejecting other local sources so no other process can pull
// the return path onto itself with a stray packet.
//
// A well-formed handshake initiation re-pins: WireGuard's socket is recreated
// when the device is rebuilt mid-session (failed in-place switch), and a pin
// to the dead socket would blackhole every reply for the whole session. A
// forged initiation only diverts ciphertext the forger cannot read.
func acceptFromPeer(peer *atomic.Pointer[net.UDPAddr], addr *net.UDPAddr, pkt []byte) bool {
	if peer.CompareAndSwap(nil, addr) {
		return true
	}
	pinned := peer.Load()
	if pinned != nil && pinned.IP.Equal(addr.IP) && pinned.Port == addr.Port {
		return true
	}
	if isWireGuardInitiation(pkt) {
		peer.Store(addr)
		return true
	}
	return false
}

func isWireGuardInitiation(pkt []byte) bool {
	return len(pkt) == wireGuardInitiationLen && pkt[0] == 1 && pkt[1] == 0 && pkt[2] == 0 && pkt[3] == 0
}
