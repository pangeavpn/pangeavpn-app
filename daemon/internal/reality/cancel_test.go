//go:build with_utls

package reality

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// Disconnect (and a user op preempting a rebuild) cancel the caller's context;
// the REALITY dial used to ignore it and run out its own 10s budget instead.
func TestStart_CallerCancelAbortsHandshake(t *testing.T) {
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
	keys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("keys: %v", err)
	}
	profile := state.RealityProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: listener.Addr().(*net.TCPAddr).Port,
		UUID:       "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		PublicKey:  keys.PublicKey,
		ShortID:    "ab12",
		ServerName: "www.example.com",
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	started := time.Now()
	err = NewManager(state.NewLogStore(10)).Start(ctx, profile)
	took := time.Since(started)
	t.Logf("Start returned %v after %s", err, took)
	if err == nil {
		t.Fatal("Start succeeded against a server that never answers")
	}
	if took > 3*time.Second {
		t.Fatalf("Start ignored the cancelled context and took %s", took)
	}
}
