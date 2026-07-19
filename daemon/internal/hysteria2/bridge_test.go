package hysteria2

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// newTestSocksBox starts a minimal local box with just a mixed (SOCKS)
// inbound fronting a direct outbound — enough to exercise bridge.go's real
// SOCKS5 UDP ASSOCIATE framing without pulling in Hysteria2/QUIC/TLS. That
// full path is covered separately by the transport_e2e test.
func newTestSocksBox(t *testing.T) (mixedAddr string, closeFn func()) {
	t.Helper()
	mixedPort, err := pickFreeLoopbackPort()
	if err != nil {
		t.Fatalf("pickFreeLoopbackPort: %v", err)
	}

	opts := option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeMixed,
				Tag:  "test-mixed-in",
				Options: &option.HTTPMixedInboundOptions{
					ListenOptions: option.ListenOptions{Listen: loopbackAddr(), ListenPort: uint16(mixedPort)},
				},
			},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect, Tag: "test-direct-out", Options: &option.DirectOutboundOptions{}},
		},
	}

	b, err := box.New(box.Options{Context: newBoxContext(context.Background()), Options: opts})
	if err != nil {
		t.Fatalf("box.New: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("box.Start: %v", err)
	}
	return "127.0.0.1:" + strconv.Itoa(mixedPort), func() { b.Close() }
}

func TestUDPBridgeRoundTripsThroughSocks(t *testing.T) {
	mixedAddr, closeSocks := newTestSocksBox(t)
	defer closeSocks()

	echoAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0}
	echoConn, err := net.ListenUDP("udp", echoAddr)
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	defer echoConn.Close()
	echoPort := echoConn.LocalAddr().(*net.UDPAddr).Port
	go func() {
		buf := make([]byte, 2048)
		for {
			n, src, err := echoConn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			echoConn.WriteToUDP(buf[:n], src)
		}
	}()

	// Point the bridge's fixed relay destination at our echo server for
	// this test (production uses the package default, 127.0.0.1:51820).
	old := relayDestination
	relayDestination = "127.0.0.1:" + strconv.Itoa(echoPort)
	defer func() { relayDestination = old }()

	logs := state.NewLogStore(100)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	bridge, err := newUDPBridge(ctx, logs, 0, mixedAddr)
	if err != nil {
		t.Fatalf("newUDPBridge: %v", err)
	}
	defer bridge.Close()

	wgSocket, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: bridge.boundPort()})
	if err != nil {
		t.Fatalf("dial bridge: %v", err)
	}
	defer wgSocket.Close()

	payload := []byte("wireguard-datagram-through-bridge")
	if _, err := wgSocket.Write(payload); err != nil {
		t.Fatalf("write to bridge: %v", err)
	}

	wgSocket.SetReadDeadline(time.Now().Add(4 * time.Second))
	buf := make([]byte, 2048)
	n, err := wgSocket.Read(buf)
	if err != nil {
		t.Fatalf("read echoed reply: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Fatalf("round trip = %q, want %q", buf[:n], payload)
	}
}
