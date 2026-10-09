package hysteria2

import "net"

// pickFreeLoopbackPort grabs an OS-assigned ephemeral TCP port on loopback for
// the internal mixed inbound; a close-then-rebind race just surfaces as a clear error.
func pickFreeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// pickFreeLoopbackUDPPort is pickFreeLoopbackPort's UDP counterpart: on Windows,
// TCP and UDP ephemeral ranges aren't interchangeable, so this needs its own probe.
func pickFreeLoopbackUDPPort() (int, error) {
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port, nil
}
