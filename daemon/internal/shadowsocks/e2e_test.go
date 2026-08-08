//go:build transport_e2e

package shadowsocks

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	sbshadowsocks "github.com/sagernet/sing-box/protocol/shadowsocks"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// serverBoxContext widens the production registry with the SS inbound and a
// direct outbound, which only the in-process test server needs.
func serverBoxContext(ctx context.Context) context.Context {
	inbounds := inbound.NewRegistry()
	sbshadowsocks.RegisterInbound(inbounds)
	outbounds := outbound.NewRegistry()
	direct.RegisterOutbound(outbounds)
	sbshadowsocks.RegisterOutbound(outbounds)
	return box.Context(ctx, inbounds, outbounds, endpoint.NewRegistry(), newDNSRegistry(), boxservice.NewRegistry())
}

func loopbackAddr() *badoption.Addr {
	addr := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	return &addr
}

// dualPortCursor walks candidates; seeded off the pid so two concurrent runs
// of this package start in different places.
var dualPortCursor = func() *atomic.Int32 {
	c := &atomic.Int32{}
	c.Store(int32(os.Getpid() % 20000))
	return c
}()

// pickFreeLoopbackDualPort returns a port free for BOTH protocols. The SS
// inbound binds tcp_and_udp on one number, so a port free for only one is
// useless. Candidates come from below Windows' dynamic range (49152+), where
// Hyper-V reserves blocks that reject binds with WSAEACCES even when the port
// looks free — asking the OS for an ephemeral port lands in exactly that
// window. Both protocols are held at once before releasing, so a number that
// passes really does accept both.
func pickFreeLoopbackDualPort(t *testing.T) int {
	t.Helper()
	for range 400 {
		port := 20000 + int(dualPortCursor.Add(1))%25000

		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err != nil {
			continue
		}
		u, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: port})
		if err != nil {
			l.Close()
			continue
		}
		l.Close()
		u.Close()
		return port
	}
	t.Fatal("no loopback port free for both TCP and UDP")
	return 0
}

// startShadowsocksTestServer runs a real sing-box Shadowsocks server that
// forwards to whatever destination the client's SS header names.
func startShadowsocksTestServer(t *testing.T, port int, method, password string) func() {
	t.Helper()
	opts := option.Options{
		Log: &option.LogOptions{Disabled: true},
		Inbounds: []option.Inbound{
			{
				Type: C.TypeShadowsocks,
				Tag:  "e2e-ss-in",
				Options: &option.ShadowsocksInboundOptions{
					ListenOptions: option.ListenOptions{Listen: loopbackAddr(), ListenPort: uint16(port)},
					Method:        method,
					Password:      password,
				},
			},
		},
		Outbounds: []option.Outbound{
			{Type: C.TypeDirect, Tag: "e2e-direct-out", Options: &option.DirectOutboundOptions{}},
		},
	}
	b, err := box.New(box.Options{Context: serverBoxContext(context.Background()), Options: opts})
	if err != nil {
		t.Fatalf("build test server box: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("start test server box: %v", err)
	}
	return func() { b.Close() }
}

// startUDPEcho stands in for the node's WireGuard listener behind the relay.
func startUDPEcho(t *testing.T) (port int, closeFn func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	go func() {
		buf := make([]byte, 2048)
		for {
			n, src, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			conn.WriteToUDP(buf[:n], src)
		}
	}()
	return conn.LocalAddr().(*net.UDPAddr).Port, func() { conn.Close() }
}

func roundTrip(t *testing.T, localPort int, payload []byte, timeout time.Duration) ([]byte, error) {
	t.Helper()
	sock, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort})
	if err != nil {
		t.Fatalf("dial bridge loopback: %v", err)
	}
	defer sock.Close()

	if _, err := sock.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}
	sock.SetReadDeadline(time.Now().Add(timeout))
	buf := make([]byte, 2048)
	n, err := sock.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

// TestE2EClientToServerRoundTrip proves the full path: this package's Manager,
// a real SS server, and an echo listener standing in for WireGuard.
func TestE2EClientToServerRoundTrip(t *testing.T) {
	const method = "chacha20-ietf-poly1305"
	const password = "e2e-shadowsocks-password"

	ssPort := pickFreeLoopbackDualPort(t)
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	stopServer := startShadowsocksTestServer(t, ssPort, method, password)
	defer stopServer()

	profile := state.ShadowsocksProfile{
		LocalPort:  0,
		RemoteHost: "127.0.0.1",
		RemotePort: ssPort,
		Method:     method,
		Password:   password,
		TargetHost: "127.0.0.1",
		TargetPort: echoPort,
	}

	mgr := NewManager(state.NewLogStore(200))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := mgr.Start(ctx, profile); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	localPort := mgr.BoundLocalPort()
	if localPort <= 0 {
		t.Fatalf("BoundLocalPort() = %d, want > 0", localPort)
	}

	payload := []byte("real-wireguard-shaped-payload-through-shadowsocks-aead-tunnel")
	got, err := roundTrip(t, localPort, payload, 10*time.Second)
	if err != nil {
		t.Fatalf("read round-tripped payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("round trip mismatch: got %q, want %q", got, payload)
	}
	t.Logf("client-to-server round trip OK: %d bytes through the shadowsocks tunnel", len(got))
}

func TestE2ESS2022RoundTrip(t *testing.T) {
	// 2022-blake3-aes-128-gcm keys are base64 of exactly 16 bytes, not a passphrase.
	const method = "2022-blake3-aes-128-gcm"
	const password = "MTIzNDU2Nzg5MGFiY2RlZg=="

	ssPort := pickFreeLoopbackDualPort(t)
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	stopServer := startShadowsocksTestServer(t, ssPort, method, password)
	defer stopServer()

	profile := state.ShadowsocksProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: ssPort,
		Method:     method,
		Password:   password,
		TargetHost: "127.0.0.1",
		TargetPort: echoPort,
	}

	mgr := NewManager(state.NewLogStore(200))
	if err := mgr.Start(context.Background(), profile); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	payload := []byte("ss-2022-payload")
	got, err := roundTrip(t, mgr.BoundLocalPort(), payload, 10*time.Second)
	if err != nil {
		t.Fatalf("read round-tripped payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("round trip mismatch: got %q, want %q", got, payload)
	}
}

func TestE2EUDPOverTCPRoundTrip(t *testing.T) {
	const method = "chacha20-ietf-poly1305"
	const password = "e2e-uot-password"

	ssPort := pickFreeLoopbackDualPort(t)
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	stopServer := startShadowsocksTestServer(t, ssPort, method, password)
	defer stopServer()

	profile := state.ShadowsocksProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: ssPort,
		Method:     method,
		Password:   password,
		TargetHost: "127.0.0.1",
		TargetPort: echoPort,
		UDPOverTCP: true,
	}

	mgr := NewManager(state.NewLogStore(200))
	if err := mgr.Start(context.Background(), profile); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	payload := []byte("wireguard-udp-inside-the-ss-tcp-stream")
	got, err := roundTrip(t, mgr.BoundLocalPort(), payload, 10*time.Second)
	if err != nil {
		t.Fatalf("read round-tripped payload: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("round trip mismatch: got %q, want %q", got, payload)
	}
}

// TestE2EProxyCarriesHTTPConnect proves the hub control-plane path: the app
// speaks HTTP CONNECT to ProxyManager's loopback port, the SS server relays to
// an ordinary TCP listener, and bytes flow both ways.
func TestE2EProxyCarriesHTTPConnect(t *testing.T) {
	const method = "chacha20-ietf-poly1305"
	const password = "e2e-proxy-password"

	ssPort := pickFreeLoopbackDualPort(t)
	stopServer := startShadowsocksTestServer(t, ssPort, method, password)
	defer stopServer()

	// Stands in for the hub: greets, then echoes one line.
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen origin: %v", err)
	}
	defer origin.Close()
	go func() {
		conn, acceptErr := origin.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("HELLO\n"))
		buf := make([]byte, 64)
		n, readErr := conn.Read(buf)
		if readErr == nil {
			conn.Write(buf[:n])
		}
	}()
	originPort := origin.Addr().(*net.TCPAddr).Port

	mgr := NewProxyManager(state.NewLogStore(200))
	proxyPort, err := mgr.Start(context.Background(), state.ShadowsocksProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: ssPort,
		Method:     method,
		Password:   password,
	})
	if err != nil {
		t.Fatalf("ProxyManager.Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort)), 5*time.Second)
	if err != nil {
		t.Fatalf("dial proxy: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))

	target := net.JoinHostPort("127.0.0.1", strconv.Itoa(originPort))
	fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", target, target)

	reader := bufio.NewReader(conn)
	statusLine, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read CONNECT status: %v", err)
	}
	if !strings.Contains(statusLine, " 200") {
		t.Fatalf("CONNECT status = %q, want 2xx", strings.TrimSpace(statusLine))
	}
	// Drain the remaining response headers.
	for {
		line, headerErr := reader.ReadString('\n')
		if headerErr != nil {
			t.Fatalf("read CONNECT headers: %v", headerErr)
		}
		if strings.TrimSpace(line) == "" {
			break
		}
	}

	greeting, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read origin greeting through the tunnel: %v", err)
	}
	if strings.TrimSpace(greeting) != "HELLO" {
		t.Fatalf("greeting = %q, want HELLO", strings.TrimSpace(greeting))
	}

	if _, err := conn.Write([]byte("PING\n")); err != nil {
		t.Fatalf("write through the tunnel: %v", err)
	}
	echo, err := reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read echo: %v", err)
	}
	if strings.TrimSpace(echo) != "PING" {
		t.Fatalf("echo = %q, want PING", strings.TrimSpace(echo))
	}
}

// TestE2EWrongPasswordStartsButCannotCarryTraffic documents exactly why this
// package implements no SessionWaiter: Start succeeds on a bad credential.
func TestE2EWrongPasswordStartsButCannotCarryTraffic(t *testing.T) {
	const method = "chacha20-ietf-poly1305"

	ssPort := pickFreeLoopbackDualPort(t)
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	stopServer := startShadowsocksTestServer(t, ssPort, method, "the-real-password")
	defer stopServer()

	profile := state.ShadowsocksProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: ssPort,
		Method:     method,
		Password:   "not-the-real-password",
		TargetHost: "127.0.0.1",
		TargetPort: echoPort,
	}

	mgr := NewManager(state.NewLogStore(200))
	if err := mgr.Start(context.Background(), profile); err != nil {
		t.Fatalf("Manager.Start with a wrong password = %v, want nil (SS has no client-visible handshake)", err)
	}
	defer mgr.Stop(context.Background())

	if !mgr.Status().Running {
		t.Fatal("Status().Running = false, want true: a bad PSK still leaves the transport 'started'")
	}

	if got, err := roundTrip(t, mgr.BoundLocalPort(), []byte("should-not-arrive"), 3*time.Second); err == nil {
		t.Fatalf("round trip returned %q, want a timeout: the server must drop an undecryptable datagram", got)
	}
}
