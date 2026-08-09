package hysteria2

import "net"

// pickFreeLoopbackPort grabs an OS-assigned ephemeral TCP port on loopback
// for the internal mixed inbound (never exposed outside this package). The
// brief close-then-rebind race is acceptable here: loopback-only, one
// daemon process, and a collision just surfaces as a clear "address in
// use" error from box.Start.
func pickFreeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// pickFreeLoopbackUDPPort is pickFreeLoopbackPort's UDP counterpart. On
// Windows, TCP and UDP ephemeral ranges aren't interchangeable (Hyper-V
// reserves UDP port blocks a free TCP probe won't see), so UDP listeners
// must be sized with a UDP probe.
func pickFreeLoopbackUDPPort() (int, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port, nil
}
