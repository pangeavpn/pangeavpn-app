package transport

import (
	"context"
	"net"
	"strings"
	"time"
)

// networkChangedRetryBackoff paces redials after a spurious interface reset. It
// is short enough to fit several tries inside a 10s session budget, yet long
// enough to let a flapping default route settle between attempts.
const networkChangedRetryBackoff = 200 * time.Millisecond

// IsNetworkChanged reports whether err is sing-box's "network changed" reset.
//
// sing-box runs a default-interface monitor and, on every change of the host's
// default route, calls InterfaceUpdated on its QUIC outbounds (hysteria2, tuic),
// which close the live connection with this error. That is correct for a client
// that owns the network, but our in-process transports are loopback-fronted
// carriers the daemon already supervises, so the reset is spurious here: the
// outbound stays alive and the next dial rebuilds the connection. The message is
// matched by text because sing-box raises it as a plain error with no sentinel.
func IsNetworkChanged(err error) bool {
	return err != nil && strings.Contains(err.Error(), "network changed")
}

// ListenPacketResilient proves a QUIC session, redialing through the transient
// "network changed" reset that sing-box's interface monitor raises when the
// host's default route flaps — the shape a corporate VPN client such as Zscaler
// produces when it shifts the local address during a handshake. Without this a
// single flap mid-handshake fails the whole transport, even though the outbound
// could simply redial; with it the dial keeps trying until it lands in a settled
// window or the caller's context (its session budget) is spent.
//
// Any error that is not a network-change reset is returned at once, so a genuine
// block or refusal still fails fast. On a spent budget the last reset error is
// returned rather than the bare context error, so logs still name the cause.
func ListenPacketResilient(ctx context.Context, dial func(context.Context) (net.PacketConn, error)) (net.PacketConn, error) {
	for {
		pc, err := dial(ctx)
		if err == nil {
			return pc, nil
		}
		if !IsNetworkChanged(err) {
			return nil, err
		}
		// The monitor closed the connection; wait for the route to settle, then
		// redial — unless the session budget is already spent.
		timer := time.NewTimer(networkChangedRetryBackoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, err
		case <-timer.C:
		}
	}
}
