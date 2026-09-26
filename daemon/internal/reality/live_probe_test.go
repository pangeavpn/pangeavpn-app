//go:build reality_live && with_utls

package reality

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
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

// liveHubProfile reads a deployed node's control-plane user from the environment.
func liveHubProfile(t *testing.T) (state.RealityProfile, string) {
	t.Helper()
	port, _ := strconv.Atoi(os.Getenv("RL_PORT"))
	profile := state.RealityProfile{
		RemoteHost: os.Getenv("RL_NODE"),
		RemotePort: port,
		UUID:       os.Getenv("RL_UUID"),
		PublicKey:  os.Getenv("RL_PUBKEY"),
		ShortID:    os.Getenv("RL_SHORT_ID"),
		ServerName: os.Getenv("RL_SNI"),
	}
	hub := os.Getenv("RL_HUB_HOST")
	if profile.RemoteHost == "" || port == 0 || profile.UUID == "" || profile.PublicKey == "" || hub == "" {
		t.Skip("set RL_NODE, RL_PORT, RL_UUID, RL_PUBKEY, RL_SHORT_ID, RL_SNI, RL_HUB_HOST")
	}
	return profile, hub
}

func startLiveHubProxy(t *testing.T, profile state.RealityProfile) (int, string, string) {
	t.Helper()
	pm := NewProxyManager(state.NewLogStore(200))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	port, err := pm.Start(ctx, profile)
	if err != nil {
		t.Fatalf("start reality hub proxy: %v", err)
	}
	t.Cleanup(func() { pm.Stop(context.Background()) })
	user, pass := pm.Credentials()
	return port, user, pass
}

// tlsThrough CONNECTs to target and completes TLS presenting the hub's name.
func tlsThrough(t *testing.T, proxyPort int, user, pass, target, hub string, verify bool) (*tls.Conn, error) {
	t.Helper()
	raw, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort)), 10*time.Second)
	if err != nil {
		t.Fatalf("dial local proxy: %v", err)
	}
	raw.SetDeadline(time.Now().Add(25 * time.Second))
	creds := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
	fmt.Fprintf(raw, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", target, target, creds)
	br := bufio.NewReader(raw)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			raw.Close()
			return nil, fmt.Errorf("CONNECT %s: %w", target, err)
		}
		if line == "\r\n" {
			break
		}
	}
	conn := tls.Client(raw, &tls.Config{ServerName: hub, InsecureSkipVerify: !verify})
	if err := conn.Handshake(); err != nil {
		raw.Close()
		return nil, err
	}
	return conn, nil
}

func leafFingerprint(conn *tls.Conn) [32]byte {
	return sha256.Sum256(conn.ConnectionState().PeerCertificates[0].Raw)
}

func getHealth(t *testing.T, conn *tls.Conn, hub string) *http.Response {
	t.Helper()
	fmt.Fprintf(conn, "GET /health HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", hub)
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("read hub response: %v", err)
	}
	return resp
}

// Every destination the hub user asks for must be answered by the hub itself,
// or shipping these credentials in the client hands out an open proxy.
func TestLiveHubRealityPinsEveryTargetToTheHub(t *testing.T) {
	profile, hub := liveHubProfile(t)
	verify := os.Getenv("RL_INSECURE") == ""
	port, user, pass := startLiveHubProxy(t, profile)

	hubConn, err := tlsThrough(t, port, user, pass, net.JoinHostPort(hub, "443"), hub, verify)
	if err != nil {
		t.Fatalf("TLS to the hub through REALITY: %v", err)
	}
	want := leafFingerprint(hubConn)
	resp := getHealth(t, hubConn, hub)
	resp.Body.Close()
	hubConn.Close()
	t.Logf("hub /health through REALITY -> %s", resp.Status)
	if resp.StatusCode >= 500 {
		t.Fatalf("hub answered %s", resp.Status)
	}

	for _, target := range []string{"1.1.1.1:443", "www.google.com:443", "example.com:80", "127.0.0.1:3000", "127.0.0.1:22"} {
		t.Run(target, func(t *testing.T) {
			conn, err := tlsThrough(t, port, user, pass, target, hub, false)
			if err != nil {
				t.Logf("no TLS at all: %v", err)
				return
			}
			defer conn.Close()
			got := leafFingerprint(conn)
			if !bytes.Equal(got[:], want[:]) {
				t.Fatalf("%s answered with a certificate that is not the hub's", target)
			}
			t.Logf("pinned to the hub, as expected")
		})
	}
}

// A user the node does not know must get nowhere, so success above came from
// the credentials rather than a node that ignores them.
func TestLiveHubRealityRejectsAnUnknownUser(t *testing.T) {
	profile, hub := liveHubProfile(t)
	profile.UUID = "00000000-0000-4000-8000-000000000000"
	port, user, pass := startLiveHubProxy(t, profile)

	conn, err := tlsThrough(t, port, user, pass, net.JoinHostPort(hub, "443"), hub, false)
	if err == nil {
		conn.Close()
		t.Fatal("an unknown uuid still reached the hub over TLS")
	}
	t.Logf("unknown user rejected as expected: %v", err)
}
