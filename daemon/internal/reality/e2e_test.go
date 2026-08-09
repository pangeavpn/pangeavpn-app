//go:build transport_e2e

// Client-to-server proof: starts a real sing-box VLESS+REALITY server locally
// (with its own REALITY keypair and a local TLS "dest"/handshake target),
// starts this package's Manager against it, and pushes a real UDP payload
// through to a local echo listener reached via the server's implicit direct
// outbound. Requires -tags with_utls (REALITY needs uTLS; sing-box compiles
// it out by default — see manager.go's package doc).
//
// Run: go test -tags "transport_e2e with_utls" ./internal/reality/... -run E2E -v
package reality

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	"github.com/sagernet/sing-box/adapter/endpoint"
	"github.com/sagernet/sing-box/adapter/inbound"
	"github.com/sagernet/sing-box/adapter/outbound"
	boxservice "github.com/sagernet/sing-box/adapter/service"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/dns"
	dnslocal "github.com/sagernet/sing-box/dns/transport/local"
	"github.com/sagernet/sing-box/option"
	"github.com/sagernet/sing-box/protocol/direct"
	"github.com/sagernet/sing-box/protocol/vless"
	"github.com/sagernet/sing/common/json/badoption"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// randomUUID returns an RFC 4122 v4 UUID string, self-contained so the test
// doesn't need an extra module dependency just for this.
func randomUUID(t *testing.T) string {
	t.Helper()
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("generate uuid: %v", err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// freeTCPPort asks the OS for a currently-unused loopback TCP port.
func freeTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free tcp port: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// serverRegistryContext registers just what the test's VLESS+REALITY server
// needs: the vless inbound and (explicitly, for clarity) the direct
// outbound sing-box already wires in as the implicit default.
func serverRegistryContext(ctx context.Context) context.Context {
	inboundRegistry := inbound.NewRegistry()
	vless.RegisterInbound(inboundRegistry)
	outboundRegistry := outbound.NewRegistry()
	direct.RegisterOutbound(outboundRegistry)
	dnsRegistry := dns.NewTransportRegistry()
	dnslocal.RegisterTransport(dnsRegistry)
	return box.Context(ctx, inboundRegistry, outboundRegistry, endpoint.NewRegistry(), dnsRegistry, boxservice.NewRegistry())
}

// startEchoServer starts a UDP listener that echoes every datagram back to
// its sender. Stands in for "the node's local WireGuard listener" that the
// REALITY server's implicit direct outbound relays decoded traffic to.
func startEchoServer(t *testing.T) (port int, stop func()) {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		t.Fatalf("listen echo udp: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 65535)
		for {
			n, addr, err := conn.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = conn.WriteToUDP(buf[:n], addr)
		}
	}()
	addr := conn.LocalAddr().(*net.UDPAddr)
	return addr.Port, func() {
		conn.Close()
		<-done
	}
}

// startRealityServer builds and starts a real sing-box VLESS+REALITY server
// on 127.0.0.1:serverPort, authenticating uuid/shortID against privateKey,
// with destPort as its REALITY camouflage handshake target.
func startRealityServer(t *testing.T, serverPort int, uuid, sni, privateKey, shortID string, destPort int) *box.Box {
	t.Helper()
	listenAddr := badoption.Addr(netip.MustParseAddr("127.0.0.1"))

	engine, err := box.New(box.Options{
		Context: serverRegistryContext(context.Background()),
		Options: option.Options{
			Log: &option.LogOptions{Level: "debug"},
			Inbounds: []option.Inbound{{
				Type: C.TypeVLESS,
				Tag:  "vless-in",
				Options: &option.VLESSInboundOptions{
					ListenOptions: option.ListenOptions{
						Listen:     &listenAddr,
						ListenPort: uint16(serverPort),
					},
					Users: []option.VLESSUser{{Name: "e2e", UUID: uuid}},
					InboundTLSOptionsContainer: option.InboundTLSOptionsContainer{
						TLS: &option.InboundTLSOptions{
							Enabled:    true,
							ServerName: sni,
							Reality: &option.InboundRealityOptions{
								Enabled: true,
								Handshake: option.InboundRealityHandshakeOptions{
									ServerOptions: option.ServerOptions{Server: "127.0.0.1", ServerPort: uint16(destPort)},
								},
								PrivateKey: privateKey,
								ShortID:    badoption.Listable[string]{shortID},
							},
						},
					},
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("build reality server: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("start reality server: %v", err)
	}
	return engine
}

func TestE2EClientToServerRoundTrip(t *testing.T) {
	// 1. REALITY keypair + short ID.
	keys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	shortID, err := GenerateShortID(8)
	if err != nil {
		t.Fatalf("GenerateShortID: %v", err)
	}
	shortIDBytes, err := hex.DecodeString(shortID)
	if err != nil || len(shortIDBytes) == 0 {
		t.Fatalf("generated short id %q is not valid hex", shortID)
	}

	// 2. Local TLS "dest" / handshake camouflage target.
	destServer := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer destServer.Close()
	destPort := destServer.Listener.Addr().(*net.TCPAddr).Port

	// 3. Echo listener standing in for the remote node's local WireGuard port.
	echoPort, stopEcho := startEchoServer(t)
	defer stopEcho()

	uuid := randomUUID(t)
	sni := "reality-e2e.internal.test"
	serverPort := freeTCPPort(t)

	// 4. Real sing-box VLESS+REALITY server, known config.
	server := startRealityServer(t, serverPort, uuid, sni, keys.PrivateKey, shortID, destPort)
	defer server.Close()

	// 5. This package's Manager, pointed at the server above.
	logs := state.NewLogStore(256)
	mgr := NewManager(logs)

	profile := state.RealityProfile{
		LocalPort:  0,
		RemoteHost: "127.0.0.1",
		RemotePort: serverPort,
		UUID:       uuid,
		PublicKey:  keys.PublicKey,
		ShortID:    shortID,
		ServerName: sni,
		TargetPort: echoPort,
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer startCancel()
	if err := mgr.Start(startCtx, profile); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		if err := mgr.Stop(stopCtx); err != nil {
			t.Errorf("Manager.Stop: %v", err)
		}
	}()

	if err := mgr.WaitForSession(startCtx, 5*time.Second); err != nil {
		t.Fatalf("WaitForSession: %v", err)
	}

	localPort := mgr.BoundLocalPort()
	if localPort == 0 {
		t.Fatal("BoundLocalPort returned 0 after successful Start")
	}
	if !mgr.Status().Running {
		t.Fatal("Status().Running == false after successful Start")
	}

	// 6. Push a real payload through: local UDP (standing in for WireGuard)
	// -> Manager's bridge -> VLESS+REALITY -> server -> echo listener -> back.
	client, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort})
	if err != nil {
		t.Fatalf("dial local bridge: %v", err)
	}
	defer client.Close()

	payload := []byte("pangeavpn reality e2e round trip payload")

	var (
		respN    int
		respErr  error
		response = make([]byte, 4096)
	)
	// A few retries: the very first datagram can race the REALITY handshake
	// settling / server's route wiring on a loaded CI box.
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := client.Write(payload); err != nil {
			t.Fatalf("write to local bridge: %v", err)
		}
		client.SetReadDeadline(time.Now().Add(3 * time.Second))
		respN, respErr = client.Read(response)
		if respErr == nil {
			break
		}
	}
	if respErr != nil {
		t.Fatalf("no echo response received after retries: %v", respErr)
	}

	if string(response[:respN]) != string(payload) {
		t.Fatalf("round trip payload mismatch: got %q, want %q", response[:respN], payload)
	}

	t.Logf("round trip OK: %d bytes echoed through 127.0.0.1:%d -> reality(%s:%d) -> echo(127.0.0.1:%d)",
		respN, localPort, profile.RemoteHost, profile.RemotePort, echoPort)
}
