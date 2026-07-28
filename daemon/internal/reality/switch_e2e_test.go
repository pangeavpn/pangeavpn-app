//go:build transport_e2e

// Server-switch proof: the daemon's Switch stops the active transport and
// starts it again against a different node inside the same process. This
// exercises that Stop/Start cycle against two real, independently-keyed
// VLESS+REALITY servers and proves traffic actually reaches the second one.
//
// Run: go test -tags "transport_e2e with_utls" ./internal/reality/... -run Switch -v
package reality

import (
	"context"
	"encoding/hex"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// realityNode is one running VLESS+REALITY server plus the profile that
// reaches it — a stand-in for a node the user can pick in the server list.
type realityNode struct {
	profile state.RealityProfile
	echo    int
	close   func()
}

func startRealityNode(t *testing.T, sni string) *realityNode {
	t.Helper()

	keys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	shortID, err := GenerateShortID(8)
	if err != nil {
		t.Fatalf("GenerateShortID: %v", err)
	}
	if _, err := hex.DecodeString(shortID); err != nil {
		t.Fatalf("short id %q is not valid hex: %v", shortID, err)
	}

	dest := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	destPort := dest.Listener.Addr().(*net.TCPAddr).Port

	echoPort, stopEcho := startEchoServer(t)
	uuid := randomUUID(t)
	serverPort := freeTCPPort(t)
	server := startRealityServer(t, serverPort, uuid, sni, keys.PrivateKey, shortID, destPort)

	return &realityNode{
		profile: state.RealityProfile{
			LocalPort:  0,
			RemoteHost: "127.0.0.1",
			RemotePort: serverPort,
			UUID:       uuid,
			PublicKey:  keys.PublicKey,
			ShortID:    shortID,
			ServerName: sni,
			TargetPort: echoPort,
		},
		echo:  echoPort,
		close: func() { server.Close(); stopEcho(); dest.Close() },
	}
}

// roundTrip pushes a payload through the manager's loopback listener and
// returns what came back, proving the tunnel carries traffic end to end.
func roundTrip(t *testing.T, mgr *Manager, payload string) string {
	t.Helper()

	localPort := mgr.BoundLocalPort()
	if localPort == 0 {
		t.Fatal("BoundLocalPort returned 0 while running")
	}
	client, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: localPort})
	if err != nil {
		t.Fatalf("dial local bridge: %v", err)
	}
	defer client.Close()

	response := make([]byte, 4096)
	for attempt := 0; attempt < 5; attempt++ {
		if _, err := client.Write([]byte(payload)); err != nil {
			t.Fatalf("write to local bridge: %v", err)
		}
		client.SetReadDeadline(time.Now().Add(3 * time.Second))
		n, err := client.Read(response)
		if err == nil {
			return string(response[:n])
		}
	}
	t.Fatalf("no echo response from 127.0.0.1:%d after retries", localPort)
	return ""
}

// TestSwitchBetweenNodesInOneProcess is the Switch flow: connected to node A,
// user picks node B. The manager must tear down cleanly and come back up
// against B's completely different keys, UUID and port.
func TestSwitchBetweenNodesInOneProcess(t *testing.T) {
	nodeA := startRealityNode(t, "reality-a.internal.test")
	defer nodeA.close()
	nodeB := startRealityNode(t, "reality-b.internal.test")
	defer nodeB.close()

	logs := state.NewLogStore(256)
	mgr := NewManager(logs)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connected to node A.
	if err := mgr.Start(ctx, nodeA.profile); err != nil {
		t.Fatalf("Start(nodeA): %v", err)
	}
	if err := mgr.WaitForSession(ctx, 5*time.Second); err != nil {
		t.Fatalf("WaitForSession(nodeA): %v", err)
	}
	if got := roundTrip(t, mgr, "payload via node A"); got != "payload via node A" {
		t.Fatalf("node A round trip: got %q", got)
	}
	portA := mgr.BoundLocalPort()

	// The Switch flow: stop the active transport, then start the new one.
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 4*time.Second)
	if err := mgr.Stop(stopCtx); err != nil {
		t.Fatalf("Stop after nodeA: %v", err)
	}
	stopCancel()
	if mgr.Status().Running {
		t.Fatal("Status().Running still true after Stop — Start would no-op and stay on the old node")
	}

	if err := mgr.Start(ctx, nodeB.profile); err != nil {
		t.Fatalf("Start(nodeB) after switch: %v", err)
	}
	if err := mgr.WaitForSession(ctx, 5*time.Second); err != nil {
		t.Fatalf("WaitForSession(nodeB) after switch: %v", err)
	}
	if !mgr.Status().Running {
		t.Fatal("Status().Running false after Start(nodeB)")
	}

	portB := mgr.BoundLocalPort()
	if portB == portA {
		t.Logf("note: rebound the same loopback port %d", portB)
	}
	if got := roundTrip(t, mgr, "payload via node B"); got != "payload via node B" {
		t.Fatalf("node B round trip after switch: got %q", got)
	}

	stopCtx2, stopCancel2 := context.WithTimeout(context.Background(), 4*time.Second)
	defer stopCancel2()
	if err := mgr.Stop(stopCtx2); err != nil {
		t.Errorf("final Stop: %v", err)
	}
}

// TestSwitchRepeatedly walks several nodes in a row — a user hopping through
// the server list — to catch state that only leaks after the first cycle.
func TestSwitchRepeatedly(t *testing.T) {
	nodes := []*realityNode{
		startRealityNode(t, "reality-1.internal.test"),
		startRealityNode(t, "reality-2.internal.test"),
		startRealityNode(t, "reality-3.internal.test")}
	for _, n := range nodes {
		defer n.close()
	}

	logs := state.NewLogStore(256)
	mgr := NewManager(logs)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	for i, node := range nodes {
		if err := mgr.Start(ctx, node.profile); err != nil {
			t.Fatalf("hop %d: Start: %v", i, err)
		}
		if err := mgr.WaitForSession(ctx, 5*time.Second); err != nil {
			t.Fatalf("hop %d: WaitForSession: %v", i, err)
		}
		want := "payload hop"
		if got := roundTrip(t, mgr, want); got != want {
			t.Fatalf("hop %d: round trip got %q", i, got)
		}
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 4*time.Second)
		if err := mgr.Stop(stopCtx); err != nil {
			t.Fatalf("hop %d: Stop: %v", i, err)
		}
		stopCancel()
	}
}
