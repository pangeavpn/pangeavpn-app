//go:build with_utls

package reality

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func unreachableHubProfile(t *testing.T, port int) state.RealityProfile {
	t.Helper()
	keys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	return state.RealityProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: port,
		UUID:       "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		PublicKey:  keys.PublicKey,
		ShortID:    "ab12",
		ServerName: "www.example.com",
	}
}

// Start must prove the REALITY handshake, not just bind, or the app gets a
// proxy port that fails every request with no reason attached.
func TestProxyManager_StartFailsWhenNothingListens(t *testing.T) {
	mgr := NewProxyManager(state.NewLogStore(16))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if _, err := mgr.Start(ctx, unreachableHubProfile(t, freeLoopbackPort(t))); err == nil {
		mgr.Stop(context.Background())
		t.Fatal("Start succeeded with no node listening")
	}
	if got := mgr.Port(); got != 0 {
		t.Fatalf("Port() after a failed Start = %d, want 0", got)
	}
	if user, pass := mgr.Credentials(); user != "" || pass != "" {
		t.Fatal("Credentials() after a failed Start returned a credential")
	}
}

func TestProxyManager_CallerCancelAbortsHandshake(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()

	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	started := time.Now()
	_, err = NewProxyManager(state.NewLogStore(16)).Start(ctx, unreachableHubProfile(t, listener.Addr().(*net.TCPAddr).Port))
	took := time.Since(started)
	if err == nil {
		t.Fatal("Start succeeded against a node that never answers")
	}
	if took > 3*time.Second {
		t.Fatalf("Start ignored the cancelled context and took %s", took)
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}
