//go:build ss_live

package shadowsocks

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func liveEnv(t *testing.T) (node, key, hub string) {
	t.Helper()
	node, key, hub = os.Getenv("SS_NODE"), os.Getenv("SS_HUB_KEY"), os.Getenv("SS_HUB_HOST")
	if node == "" || hub == "" {
		t.Skip("set SS_NODE, SS_HUB_KEY, SS_HUB_HOST")
	}
	return
}

func startLiveProxy(t *testing.T, node, key string) (int, string, string) {
	t.Helper()
	pm := NewProxyManager(state.NewLogStore(200))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	port, err := pm.Start(ctx, state.ShadowsocksProfile{
		RemoteHost: node,
		RemotePort: 8489,
		Method:     "2022-blake3-aes-128-gcm",
		Password:   key,
	})
	if err != nil {
		t.Fatalf("start ss proxy: %v", err)
	}
	t.Cleanup(func() { pm.Stop(context.Background()) })
	user, pass := pm.Credentials()
	return port, user, pass
}

func connectThrough(t *testing.T, proxyPort int, proxyUser, proxyPass, hub string, deadline time.Duration) (net.Conn, *bufio.Reader, error) {
	t.Helper()
	raw, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort)), 10*time.Second)
	if err != nil {
		t.Fatalf("dial local proxy: %v", err)
	}
	raw.SetDeadline(time.Now().Add(deadline))
	creds := base64.StdEncoding.EncodeToString([]byte(proxyUser + ":" + proxyPass))
	fmt.Fprintf(raw, "CONNECT %s:443 HTTP/1.1\r\nHost: %s:443\r\nProxy-Authorization: Basic %s\r\n\r\n", hub, hub, creds)
	br := bufio.NewReader(raw)
	status, err := br.ReadString('\n')
	if err != nil {
		return raw, nil, err
	}
	t.Logf("CONNECT -> %s", status[:len(status)-2])
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return raw, nil, err
		}
		if line == "\r\n" {
			return raw, br, nil
		}
	}
}

// Proves a deployed ssserver (shadowsocks-rust) speaks SS-2022 to the daemon's
// sing-box client, by fetching hub /health right through the relay.
func TestLiveControlPlaneListener(t *testing.T) {
	node, key, hub := liveEnv(t)
	if key == "" {
		t.Skip("set SS_HUB_KEY")
	}

	port, user, pass := startLiveProxy(t, node, key)
	raw, _, err := connectThrough(t, port, user, pass, hub, 25*time.Second)
	if err != nil {
		t.Fatalf("CONNECT through the relay (a wrong key looks exactly like this): %v", err)
	}
	defer raw.Close()

	tconn := tls.Client(raw, &tls.Config{ServerName: hub})
	if err := tconn.Handshake(); err != nil {
		t.Fatalf("TLS through the relay: %v", err)
	}
	t.Logf("TLS ok: %s, cert CN=%s", tls.VersionName(tconn.ConnectionState().Version),
		tconn.ConnectionState().PeerCertificates[0].Subject.CommonName)

	fmt.Fprintf(tconn, "GET /health HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", hub)
	resp, err := http.ReadResponse(bufio.NewReader(tconn), nil)
	if err != nil {
		t.Fatalf("read hub response: %v", err)
	}
	defer resp.Body.Close()
	t.Logf("hub /health through SS-2022 -> %s", resp.Status)
	if resp.StatusCode >= 500 {
		t.Fatalf("hub answered %s", resp.Status)
	}
}

// A wrong key must not reach the hub, so the success above came from the
// credentials rather than some path that ignores them.
func TestLiveControlPlaneRejectsAWrongKey(t *testing.T) {
	node, _, hub := liveEnv(t)

	port, user, pass := startLiveProxy(t, node, "AAAAAAAAAAAAAAAAAAAAAA==")
	raw, _, err := connectThrough(t, port, user, pass, hub, 15*time.Second)
	if raw != nil {
		defer raw.Close()
	}
	if err != nil {
		t.Logf("wrong key rejected before CONNECT completed: %v", err)
		return
	}
	// The CONNECT reply comes from the daemon's own inbound and says nothing
	// about the remote, so assert on the first byte that must cross the relay.
	if err := tls.Client(raw, &tls.Config{ServerName: hub}).Handshake(); err == nil {
		t.Fatal("a wrong SS-2022 key still reached the hub over TLS")
	} else {
		t.Logf("wrong key rejected as expected: %v", err)
	}
}
