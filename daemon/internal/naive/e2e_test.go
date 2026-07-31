//go:build naive_cgo && transport_e2e

// Proves the naive tunnel reaches a real node's bridge. The skips below name
// the env vars. Run: go test -tags "naive_cgo transport_e2e" -run E2E -v ./...
package naive

import (
	"context"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const sessionTimeout = 30 * time.Second

type nodeEnv struct {
	host string
	ip   string
	port int
	user string
	pass string
}

func loadNodeEnv(t *testing.T) nodeEnv {
	t.Helper()
	host := os.Getenv("PANGEA_NAIVE_HOST")
	user := os.Getenv("PANGEA_NAIVE_USER")
	pass := os.Getenv("PANGEA_NAIVE_PASS")
	rawPort := os.Getenv("PANGEA_NAIVE_PORT")
	if host == "" || user == "" || pass == "" || rawPort == "" {
		t.Skip("set PANGEA_NAIVE_{HOST,PORT,USER,PASS} to run the naive e2e test")
	}
	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("PANGEA_NAIVE_PORT %q is not a number: %v", rawPort, err)
	}
	return nodeEnv{host: host, ip: os.Getenv("PANGEA_NAIVE_IP"), port: port, user: user, pass: pass}
}

// A session means TLS, HTTP/2 CONNECT, proxy auth and the SOCKS5 hop to the
// node-side bridge all worked. The engine is global, so keep these sequential.
func runSession(t *testing.T, profile state.NaiveProfile) {
	t.Helper()
	logs := state.NewLogStore(0)
	m := NewManager(logs)

	ctx, cancel := context.WithTimeout(context.Background(), sessionTimeout)
	defer cancel()

	if err := m.Start(ctx, profile); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()
		_ = m.Stop(stopCtx)
	})

	if err := m.WaitForSession(ctx, sessionTimeout); err != nil {
		t.Fatalf("no session against %s:%d (sni=%q): %v", profile.RemoteHost, profile.RemotePort, profile.ServerName, err)
	}
	if !m.Status().Running {
		t.Fatal("session established but Status reports not running")
	}
	if got := m.BoundLocalPort(); got <= 0 {
		t.Fatalf("BoundLocalPort = %d, want a bound port", got)
	}
}

// Baseline: resolve the hostname normally, then connect.
func TestE2ERealNodeByHostname(t *testing.T) {
	env := loadNodeEnv(t)
	runSession(t, state.NaiveProfile{
		LocalPort:  0,
		RemoteHost: env.host,
		RemotePort: env.port,
		Username:   env.user,
		Password:   env.pass,
		ServerName: env.host,
	})
}

// The kill switch blocks DNS, so the engine must dial the literal IP while
// still validating TLS against the hostname it carries as SNI.
func TestE2ERealNodeByIPWithSNI(t *testing.T) {
	env := loadNodeEnv(t)
	if env.ip == "" {
		t.Skip("set PANGEA_NAIVE_IP to run the IP-dial case")
	}
	runSession(t, state.NaiveProfile{
		LocalPort:  0,
		RemoteHost: env.ip,
		RemotePort: env.port,
		Username:   env.user,
		Password:   env.pass,
		ServerName: env.host,
	})
}
