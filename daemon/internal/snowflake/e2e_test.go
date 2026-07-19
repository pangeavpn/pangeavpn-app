//go:build transport_e2e

// E2E harness: runs a real local Snowflake broker (subprocess), an
// embedded proxy (proxy/lib) and relay/server (server/lib), then drives
// this package's Manager as the client. Proves a real byte round-trip
// through the full rendezvous+WebRTC+KCP/smux path — see the package
// doc comment on TestSnowflakeE2E for what "real" means here.
package snowflake

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/common/event"
	sfproxy "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/proxy/lib"
	sfserver "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/server/lib"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// testFingerprint must be exactly 40 hex chars (20-byte SHA1), matching a
// real Tor relay fingerprint's shape; the broker rejects odd-length hex.
const testFingerprint = "B7C1A2D3E4F5061728394A5B6C7D8E9F0A1B2C3D"

const brokerPkgPath = "gitlab.torproject.org/tpo/anti-censorship/pluggable-transports/snowflake/v2/broker"

// freePort asks the kernel for an ephemeral TCP port, then releases it.
// Small race window between release and reuse, standard/acceptable for
// local test harnesses.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// buildBrokerBinary compiles the real snowflake broker (a package main —
// not importable as a library, unlike proxy/lib and server/lib) into a
// temp dir. The broker module is already a dependency of this module via
// client/lib, so `go build` resolves it without any extra setup.
func buildBrokerBinary(t *testing.T) string {
	t.Helper()
	binPath := filepath.Join(t.TempDir(), "snowflake-broker.exe")
	cmd := exec.Command("go", "build", "-o", binPath, brokerPkgPath)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	t.Logf("building broker binary: %s", cmd.String())
	if err := cmd.Run(); err != nil {
		t.Fatalf("go build broker: %v", err)
	}
	return binPath
}

// bridgeListEntry mirrors broker's BridgeInfo JSON-lines format
// (gitlab.../broker/bridge-list.go).
type bridgeListEntry struct {
	DisplayName      string `json:"displayName"`
	WebSocketAddress string `json:"webSocketAddress"`
	Fingerprint      string `json:"fingerprint"`
}

func writeBridgeList(t *testing.T, relayURL string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bridge-list.jsonl")
	entry := bridgeListEntry{
		DisplayName:      "local-e2e",
		WebSocketAddress: relayURL,
		Fingerprint:      testFingerprint,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal bridge list entry: %v", err)
	}
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatalf("write bridge list: %v", err)
	}
	return path
}

// startBroker launches the real broker binary against a local, TLS-free
// HTTP listener with geoip disabled (no external DB dependency) and our
// single-entry bridge list. Its logs go straight to this process's
// stderr so `go test -v` output captures them verbatim.
func startBroker(t *testing.T, binPath, addr, bridgeListPath string) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(binPath,
		"-disable-tls",
		"-disable-geoip",
		"-addr", addr,
		"-bridge-list-path", bridgeListPath,
	)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start broker: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	return cmd
}

// waitForHTTP polls url until it responds (any status) or timeout elapses.
func waitForHTTP(t *testing.T, url string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			return
		}
		lastErr = err
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("broker did not become ready at %s within %s: %v", url, timeout, lastErr)
}

// startEmbeddedRelay runs the real Snowflake server-side transport
// (server/lib — a genuine importable library, unlike the broker) and
// echoes every framed payload it receives back to the sender. This is the
// "snowflake server / bridge" side of the real protocol: it terminates
// the KCP/smux session the proxy relays over WebSocket, and whatever
// bytes arrive on an accepted stream are exactly what our Manager wrote
// via WriteFrame on the client end.
func startEmbeddedRelay(t *testing.T, port int) {
	t.Helper()
	transport := sfserver.NewSnowflakeServer(nil) // nil cert getter = plain HTTP, no TLS
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	ln, err := transport.Listen(addr, 1)
	if err != nil {
		t.Fatalf("relay Listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				for {
					payload, err := ReadFrame(conn)
					if err != nil {
						return
					}
					if err := WriteFrame(conn, payload); err != nil {
						return
					}
				}
			}()
		}
	}()
}

// startEmbeddedProxy runs the real Snowflake proxy-side transport
// (proxy/lib), pointed at our local broker and relay. This is the actual
// WebRTC party that answers the client's SDP offer and bridges the
// resulting DataChannel to the relay's WebSocket — no mocking.
func startEmbeddedProxy(t *testing.T, brokerURL, relayURL string) {
	t.Helper()
	proxy := &sfproxy.SnowflakeProxy{
		EventDispatcher:                 event.NewSnowflakeEventDispatcher(),
		BrokerURL:                       brokerURL,
		KeepLocalAddresses:              true,
		RelayURL:                        relayURL,
		RelayDomainNamePattern:          "$", // universal match; namematcher requires a rule ending in "$"
		AllowProxyingToPrivateAddresses: true,
		AllowNonTLSRelay:                true,
		PollInterval:                    2 * time.Second,
		// Both point at closed local ports so STUN/NAT-probe fail fast
		// instead of depending on (or waiting on) real internet services —
		// harmless since host/loopback ICE candidates (KeepLocalAddresses)
		// are enough for two same-machine peers to connect directly.
		STUNURL:     "stun:127.0.0.1:1",
		NATProbeURL: "http://127.0.0.1:1/probe",
		Capacity:    1,
	}
	t.Cleanup(proxy.Stop)
	go func() {
		if err := proxy.Start(); err != nil {
			t.Logf("proxy.Start returned: %v", err)
		}
	}()
}

// TestSnowflakeE2E drives this package's Manager as a real Snowflake
// client against a fully local stack: a real broker binary, a real
// proxy/lib WebRTC proxy, and a real server/lib relay — the same three
// roles the production Tor infrastructure plays, all running on
// 127.0.0.1. No component of the rendezvous, WebRTC negotiation, or
// KCP/smux session is mocked; only the "bridge" backend (normally a
// TCP ORPort) is replaced with an echo loop so the test can assert a
// round trip without a real WireGuard peer on the other end.
//
// Run: cd daemon && go test -tags transport_e2e ./internal/snowflake/... -run E2E -v
func TestSnowflakeE2E(t *testing.T) {
	brokerBin := buildBrokerBinary(t)

	relayPort := freePort(t)
	relayURL := fmt.Sprintf("ws://127.0.0.1:%d/", relayPort)
	startEmbeddedRelay(t, relayPort)

	bridgeListPath := writeBridgeList(t, relayURL)

	brokerPort := freePort(t)
	brokerAddr := fmt.Sprintf("127.0.0.1:%d", brokerPort)
	brokerURL := fmt.Sprintf("http://%s/", brokerAddr)
	startBroker(t, brokerBin, brokerAddr, bridgeListPath)
	waitForHTTP(t, brokerURL+"robots.txt", 15*time.Second)
	t.Logf("broker ready at %s", brokerURL)

	startEmbeddedProxy(t, brokerURL, relayURL)

	logs := state.NewLogStore(1000)
	mgr := NewManager(logs)
	profile := state.SnowflakeProfile{
		LocalPort:          0,
		BrokerURL:          brokerURL,
		BridgeFingerprint:  testFingerprint,
		KeepLocalAddresses: true,
	}

	startCtx, startCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer startCancel()
	if err := mgr.Start(startCtx, profile); err != nil {
		t.Fatalf("Manager.Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer stopCancel()
		_ = mgr.Stop(stopCtx)
	})

	// Real WebRTC rendezvous: broker poll <-> proxy poll match, SDP
	// offer/answer exchange, ICE connectivity checks, DTLS + SCTP
	// handshake, then KCP session bring-up over the resulting DataChannel.
	sessionCtx, sessionCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer sessionCancel()
	waitStart := time.Now()
	if err := mgr.WaitForSession(sessionCtx, 60*time.Second); err != nil {
		t.Fatalf("WaitForSession: %v (waited %s)", err, time.Since(waitStart))
	}
	t.Logf("snowflake session established in %s", time.Since(waitStart))

	port := mgr.BoundLocalPort()
	if port <= 0 {
		t.Fatalf("BoundLocalPort() = %d, want > 0", port)
	}

	wgSocket, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Fatalf("ListenUDP: %v", err)
	}
	defer wgSocket.Close()

	target := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	outbound := []byte("pangea snowflake e2e round trip payload")
	if _, err := wgSocket.WriteToUDP(outbound, target); err != nil {
		t.Fatalf("WriteToUDP: %v", err)
	}

	wgSocket.SetReadDeadline(time.Now().Add(20 * time.Second))
	buf := make([]byte, 1500)
	n, _, err := wgSocket.ReadFromUDP(buf)
	if err != nil {
		t.Fatalf("ReadFromUDP (waiting for echo through broker+webrtc+proxy+relay): %v", err)
	}
	if !bytes.Equal(buf[:n], outbound) {
		t.Fatalf("round trip payload mismatch: got %q, want %q", buf[:n], outbound)
	}

	t.Logf("PASS: %d bytes round-tripped client -> broker/webrtc rendezvous -> proxy -> relay -> echo -> back", n)
}
