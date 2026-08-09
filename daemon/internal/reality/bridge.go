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
// of Stop), or the first unexpected I/O error otherwise.
func bridgeUDP(ctx context.Context, local *net.UDPConn, remote net.PacketConn, remoteAddr net.Addr) error {
	errCh := make(chan error, 2)
	var peer atomic.Pointer[net.UDPAddr]

	go func() {
		buf := make([]byte, 65535)
		for {
			n, addr, err := local.ReadFromUDP(buf)
			if err != nil {
				errCh <- err
				return
			}
			peer.Store(addr)
			if _, err := remote.WriteTo(buf[:n], remoteAddr); err != nil {
				errCh <- err
				return
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
		}
	}()

	select {
	case <-ctx.Done():
		return nil
	case err := <-errCh:
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
}
