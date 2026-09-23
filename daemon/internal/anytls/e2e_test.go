//go:build transport_e2e

package anytls

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"net/netip"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/certificate"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	sbanytls "github.com/sagernet/sing-box/protocol/anytls"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// serverBoxContext widens the production registry with the AnyTLS inbound and
// a direct outbound, which only the in-process test server needs.
func serverBoxContext(ctx context.Context) context.Context {
	inbounds := inbound.NewRegistry()
	sbanytls.RegisterInbound(inbounds)
	outbounds := outbound.NewRegistry()
	direct.RegisterOutbound(outbounds)
	return box.Context(ctx, inbounds, outbounds, endpoint.NewRegistry(), newDNSRegistry(), boxservice.NewRegistry(), certificate.NewRegistry())
}

func loopbackAddr() *badoption.Addr {
	addr := badoption.Addr(netip.MustParseAddr("127.0.0.1"))
	return &addr
}

// generateSelfSignedCert builds an in-memory ECDSA cert/key pair for the test
// server, the same self-signed shape a node presents behind a pin.
func generateSelfSignedCert(t *testing.T, commonName string) (certPEM, keyPEM string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: commonName},
		DNSNames:     []string{commonName},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certOut := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDer, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyOut := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDer})
	return string(certOut), string(keyOut)
}

// pinFor returns the base64 SPKI SHA-256 of the cert, the value the hub
// carries as anytls.pinSha256 and validateProfile requires with Insecure.
func pinFor(t *testing.T, certPEM string) string {
	t.Helper()
	block, _ := pem.Decode([]byte(certPEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("parse certificate: %v", err)
	}
	sum := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return base64.StdEncoding.EncodeToString(sum[:])
}

func pickFreeLoopbackTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("pick free tcp port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startAnyTLSTestServer runs a real sing-box AnyTLS server (TLS + padding +
// UDP-over-TCP) forwarding to whatever destination the client names.
func startAnyTLSTestServer(t *testing.T, port int, certPEM, keyPEM, serverName, password string) func() {
	t.Helper()
	opts := option.Options{
		Log: &option.LogOptions{Disabled: true},
		Inbounds: []option.Inbound{
			{
				Type: C.TypeAnyTLS,
				Tag:  "e2e-anytls-in",
				Options: &option.AnyTLSInboundOptions{
					ListenOptions: option.ListenOptions{Listen: loopbackAddr(), ListenPort: uint16(port)},
					Users:         []option.AnyTLSUser{{Name: "e2e", Password: password}},
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled:     true,
							ServerName:  serverName,
							Certificate: badoption.Listable[string]{certPEM},
							Key:         badoption.Listable[string]{keyPEM},
						},
					},
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
		buf := make([]byte, 65535)
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
	buf := make([]byte, 65535)
	n, err := sock.Read(buf)
	if err != nil {
		return nil, err
	}
	return buf[:n], nil
}

type e2eServer struct {
	port       int
	serverName string
	pin        string
	stop       func()
}

func startPinnedServer(t *testing.T, password string) e2eServer {
	t.Helper()
	const serverName = "anytls-e2e.example"
	certPEM, keyPEM := generateSelfSignedCert(t, serverName)
	port := pickFreeLoopbackTCPPort(t)
	stop := startAnyTLSTestServer(t, port, certPEM, keyPEM, serverName, password)
	return e2eServer{port: port, serverName: serverName, pin: pinFor(t, certPEM), stop: stop}
}

// TestE2ERoundTrip proves the full path: this package's Manager, a real
// AnyTLS server behind a pinned self-signed certificate, and an echo listener
// standing in for WireGuard, including a datagram larger than any UDP MTU.
func TestE2ERoundTrip(t *testing.T) {
	const password = "e2e-anytls-password"
	srv := startPinnedServer(t, password)
	defer srv.stop()
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	profile := state.AnyTLSProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: srv.port,
		Password:   password,
		ServerName: srv.serverName,
		Insecure:   true,
		PinSHA256:  srv.pin,
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

	if err := mgr.WaitForSession(ctx, time.Second); err != nil {
		t.Fatalf("WaitForSession after Start: %v", err)
	}
	localPort := mgr.BoundLocalPort()
	if localPort <= 0 {
		t.Fatalf("BoundLocalPort() = %d, want > 0", localPort)
	}

	// One socket for both exchanges: the bridge pins the WireGuard peer to the
	// first source it sees, so a second socket's datagrams would be dropped
	// unless they opened with a fresh handshake initiation.
	sock, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort})
	if err != nil {
		t.Fatalf("dial bridge loopback: %v", err)
	}
	defer sock.Close()
	for _, payload := range [][]byte{
		[]byte("real-wireguard-shaped-payload-through-an-anytls-session"),
		make([]byte, 1500), // a full-MTU WireGuard datagram, no clamp needed over TCP
	} {
		if _, err := sock.Write(payload); err != nil {
			t.Fatalf("write %d byte payload: %v", len(payload), err)
		}
		sock.SetReadDeadline(time.Now().Add(10 * time.Second))
		buf := make([]byte, 65535)
		n, err := sock.Read(buf)
		if err != nil {
			t.Fatalf("read round-tripped %d byte payload: %v", len(payload), err)
		}
		if string(buf[:n]) != string(payload) {
			t.Fatalf("round trip mismatch for %d bytes: got %d bytes", len(payload), n)
		}
	}
}

// A pin that does not match the node's certificate must fail inside Start:
// that is the whole point of carrying one alongside Insecure.
func TestE2EWrongPinFailsStart(t *testing.T) {
	const password = "e2e-pin-password"
	srv := startPinnedServer(t, password)
	defer srv.stop()

	wrongPin := base64.StdEncoding.EncodeToString(make([]byte, 32))
	mgr := NewManager(state.NewLogStore(200))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := mgr.Start(ctx, state.AnyTLSProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: srv.port,
		Password:   password,
		ServerName: srv.serverName,
		Insecure:   true,
		PinSHA256:  wrongPin,
	})
	if err == nil {
		mgr.Stop(context.Background())
		t.Fatal("Start with a mismatched pin = nil, want a handshake failure")
	}
	if mgr.Status().Running {
		t.Fatal("a rejected pin must leave the manager stopped")
	}
}

// TestE2EWrongPasswordStartsButCannotCarryTraffic documents the limit of what
// Start proves: the node accepts the TLS session and drops it silently on a
// bad password, so the WireGuard handshake gate is what catches it.
func TestE2EWrongPasswordStartsButCannotCarryTraffic(t *testing.T) {
	srv := startPinnedServer(t, "the-real-password")
	defer srv.stop()
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	mgr := NewManager(state.NewLogStore(200))
	err := mgr.Start(context.Background(), state.AnyTLSProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: srv.port,
		Password:   "not-the-real-password",
		ServerName: srv.serverName,
		Insecure:   true,
		PinSHA256:  srv.pin,
		TargetHost: "127.0.0.1",
		TargetPort: echoPort,
	})
	if err != nil {
		t.Fatalf("Manager.Start with a wrong password = %v, want nil (the server drops the session silently)", err)
	}
	defer mgr.Stop(context.Background())

	if got, err := roundTrip(t, mgr.BoundLocalPort(), []byte("should-not-arrive"), 3*time.Second); err == nil {
		t.Fatalf("round trip returned %q, want a timeout: the server must drop an unauthenticated session", got)
	}
}

// A second Start with a different node is a switch: the old session goes and
// traffic flows through the new one.
func TestE2EStartWithDifferentProfileSwitchesSession(t *testing.T) {
	const password = "e2e-switch-password"
	first := startPinnedServer(t, password)
	defer first.stop()
	second := startPinnedServer(t, password)
	defer second.stop()
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	profileFor := func(srv e2eServer) state.AnyTLSProfile {
		return state.AnyTLSProfile{
			RemoteHost: "127.0.0.1",
			RemotePort: srv.port,
			Password:   password,
			ServerName: srv.serverName,
			Insecure:   true,
			PinSHA256:  srv.pin,
			TargetHost: "127.0.0.1",
			TargetPort: echoPort,
		}
	}

	mgr := NewManager(state.NewLogStore(200))
	if err := mgr.Start(context.Background(), profileFor(first)); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	defer mgr.Stop(context.Background())
	firstPort := mgr.BoundLocalPort()

	if err := mgr.Start(context.Background(), profileFor(second)); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if got := mgr.BoundLocalPort(); got == 0 || got == firstPort {
		t.Fatalf("BoundLocalPort() after switch = %d, want a fresh port (first was %d)", got, firstPort)
	}
	first.stop()

	got, err := roundTrip(t, mgr.BoundLocalPort(), []byte("via-the-second-node"), 10*time.Second)
	if err != nil {
		t.Fatalf("round trip through the second node: %v", err)
	}
	if string(got) != "via-the-second-node" {
		t.Fatalf("round trip mismatch: %q", got)
	}
}
