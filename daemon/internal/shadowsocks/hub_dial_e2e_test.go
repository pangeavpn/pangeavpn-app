//go:build transport_e2e

package shadowsocks

import (
	"bufio"
	"context"
	"net"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestE2EDialHubReachesTheTarget(t *testing.T) {
	const method = "chacha20-ietf-poly1305"
	const password = "e2e-dial-hub-password"
	ssPort := pickFreeLoopbackDualPort(t)
	stopServer := startShadowsocksTestServer(t, ssPort, method, password)
	defer stopServer()

	// Stands in for the hub: reads one line, answers one line.
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
		if _, readErr := bufio.NewReader(conn).ReadString('\n'); readErr == nil {
			conn.Write([]byte("HUB\n"))
		}
	}()

	mgr := NewProxyManager(state.NewLogStore(200))
	if _, err := mgr.Start(context.Background(), state.ShadowsocksProfile{
		RemoteHost: "127.0.0.1", RemotePort: ssPort, Method: method, Password: password,
	}); err != nil {
		t.Fatalf("ProxyManager.Start: %v", err)
	}
	defer mgr.Stop(context.Background())
	if got := mgr.HubRemote(); got != "127.0.0.1" {
		t.Fatalf("HubRemote = %q, want 127.0.0.1", got)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := mgr.DialHub(ctx, "", "127.0.0.1", origin.Addr().(*net.TCPAddr).Port)
	if err != nil {
		t.Fatalf("DialHub: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	if _, err := conn.Write([]byte("ping\n")); err != nil {
		t.Fatalf("write through DialHub: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil || line != "HUB\n" {
		t.Fatalf("read through DialHub = %q, %v; want HUB", line, err)
	}
}
