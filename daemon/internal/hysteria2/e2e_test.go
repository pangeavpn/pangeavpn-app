//go:build transport_e2e

package hysteria2

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"strconv"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// generateSelfSignedCert builds an in-memory ECDSA cert/key pair for the
// test's Hysteria2 server, so no filesystem temp files or external CA are
// needed.
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

// startHysteria2TestServer runs a real sing-box Hysteria2 server (Salamander
// obfs + TLS) that forwards everything through a direct outbound, exactly
// like a production node's transport server would forward decrypted traffic
// to its co-located WireGuard listener.
func startHysteria2TestServer(t *testing.T, port int, certPEM, keyPEM, serverName, authPassword, obfsPassword string) func() {
	t.Helper()
	opts := option.Options{
		Inbounds: []option.Inbound{
			{
				Type: C.TypeHysteria2,
				Tag:  "e2e-hy2-in",
				Options: &option.Hysteria2InboundOptions{
					ListenOptions: option.ListenOptions{Listen: loopbackAddr(), ListenPort: uint16(port)},
					Obfs:          &option.Hysteria2Obfs{Type: obfsTypeSalamander, Password: obfsPassword},
					Users:         []option.Hysteria2User{{Password: authPassword}},
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
	b, err := box.New(box.Options{Context: newBoxContext(context.Background()), Options: opts})
	if err != nil {
		t.Fatalf("build test server box: %v", err)
	}
	if err := b.Start(); err != nil {
		t.Fatalf("start test server box: %v", err)
	}
	return func() { b.Close() }
}

// startUDPEcho runs a plain UDP echo listener representing the real
// WireGuard endpoint on the far side of the tunnel.
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

// TestE2EClientToServerRoundTrip proves the full client-to-server path:
// real self-signed cert, real Hysteria2 server with Salamander obfs, this
// package's Manager (mixed inbound + hysteria2 outbound + UDP bridge)
// talking to it, a byte payload pushed through the bridge's loopback UDP
// socket (exactly where WireGuard's peer Endpoint would point), relayed by
// the server to an echo listener, and read back.
func TestE2EClientToServerRoundTrip(t *testing.T) {
	const serverName = "hysteria2-e2e.pangeavpn.test"
	certPEM, keyPEM := generateSelfSignedCert(t, serverName)

	hyPort, err := pickFreeLoopbackUDPPort()
	if err != nil {
		t.Fatalf("pick hysteria2 server port: %v", err)
	}
	echoPort, closeEcho := startUDPEcho(t)
	defer closeEcho()

	const authPassword = "e2e-auth-password"
	const obfsPassword = "e2e-salamander-password"

	stopServer := startHysteria2TestServer(t, hyPort, certPEM, keyPEM, serverName, authPassword, obfsPassword)
	defer stopServer()

	// Server forwards to the echo listener regardless of what destination
	// the client requests (route.Final defaults to the sole "direct"
	// outbound), so point the bridge's fixed relay target at it.
	old := relayDestinationOverride
	relayDestinationOverride = net.JoinHostPort("127.0.0.1", strconv.Itoa(echoPort))
	defer func() { relayDestinationOverride = old }()

	profile := state.Hysteria2Profile{
		LocalPort:    0,
		RemoteHost:   "127.0.0.1",
		RemotePort:   hyPort,
		ServerName:   serverName,
		Password:     authPassword,
		ObfsPassword: obfsPassword,
		Insecure:     true, // self-signed test cert
	}

	logs := state.NewLogStore(200)
	mgr := NewManager(logs)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := mgr.Start(ctx, profile); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	defer mgr.Stop(context.Background())

	if err := mgr.WaitForSession(ctx, 10*time.Second); err != nil {
		t.Fatalf("Manager.WaitForSession: %v", err)
	}

	localPort := mgr.BoundLocalPort()
	if localPort <= 0 {
		t.Fatalf("BoundLocalPort() = %d, want > 0", localPort)
	}

	wgSocket, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort})
	if err != nil {
		t.Fatalf("dial bridge loopback: %v", err)
	}
	defer wgSocket.Close()

	payload := []byte("real-wireguard-shaped-payload-through-hysteria2-salamander-tunnel")
	if _, err := wgSocket.Write(payload); err != nil {
		t.Fatalf("write payload: %v", err)
	}

	wgSocket.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 2048)
	n, err := wgSocket.Read(buf)
	if err != nil {
		t.Fatalf("read round-tripped payload: %v", err)
	}
	if string(buf[:n]) != string(payload) {
		t.Fatalf("round trip mismatch: got %q, want %q", buf[:n], payload)
	}

	t.Logf("client-to-server round trip OK: %d bytes through hysteria2+salamander tunnel", n)
}
