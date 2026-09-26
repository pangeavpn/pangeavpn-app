//go:build transport_e2e

// Hub fence proof: a node shaped like production (a data-plane user plus a Vision
// "hub" user pinned to the hub by auth_user + override), driven through ProxyManager.
package reality

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	box "github.com/sagernet/sing-box"
	C "github.com/sagernet/sing-box/constant"
	"github.com/sagernet/sing-box/option"
	sjson "github.com/sagernet/sing/common/json"
	M "github.com/sagernet/sing/common/metadata"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const hubFenceNodeConfig = `{
  "log": {"level": "warn"},
  "inbounds": [{
    "type": "vless", "tag": "vless-in", "listen": "127.0.0.1", "listen_port": %d,
    "users": [
      {"name": "device", "uuid": %q, "flow": ""},
      {"name": "hub", "uuid": %q, "flow": "xtls-rprx-vision"}
    ],
    "tls": {
      "enabled": true, "server_name": %q,
      "reality": {
        "enabled": true,
        "handshake": {"server": "127.0.0.1", "server_port": %d},
        "private_key": %q, "short_id": [%q]
      }
    }
  }],
  "outbounds": [
    {"type": "direct", "tag": "wg-out"},
    {"type": "direct", "tag": "hub-out"}
  ],
  "route": {
    "rules": [
      {"inbound": ["vless-in"], "auth_user": ["hub"], "network": ["tcp"], "action": "route",
       "outbound": "hub-out", "override_address": "127.0.0.1", "override_port": %d},
      {"inbound": ["vless-in"], "auth_user": ["hub"], "action": "reject"},
      {"inbound": ["vless-in"], "action": "route",
       "outbound": "wg-out", "override_address": "127.0.0.1", "override_port": %d}
    ],
    "final": "wg-out"
  }
}`

type hubFenceNode struct {
	hub      *namedTLSServer
	other    *namedTLSServer
	wg       *namedTLSServer
	hubUser  state.RealityProfile
	dataUser state.RealityProfile
}

// namedTLSServer answers every request with its own name and counts hits, so
// a test can tell exactly which backend a relayed connection reached.
type namedTLSServer struct {
	*httptest.Server
	hits atomic.Int64
}

func startNamedTLSServer(t *testing.T, name string) *namedTLSServer {
	t.Helper()
	return startNamedTLSServerOn(t, name, "127.0.0.1")
}

// startNamedTLSServerOn binds ip when the OS allows it (macOS only answers on
// 127.0.0.1), so an address override is observable apart from a port override.
func startNamedTLSServerOn(t *testing.T, name, ip string) *namedTLSServer {
	t.Helper()
	s := &namedTLSServer{}
	s.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		s.hits.Add(1)
		_, _ = io.WriteString(w, name)
	}))
	if l, err := net.Listen("tcp", net.JoinHostPort(ip, "0")); err == nil {
		s.Listener.Close()
		s.Listener = l
	} else {
		t.Logf("%s server stays on %s: %v", name, s.Listener.Addr(), err)
	}
	s.StartTLS()
	t.Cleanup(s.Close)
	return s
}

func (s *namedTLSServer) port() int {
	return s.Listener.Addr().(*net.TCPAddr).Port
}

func (s *namedTLSServer) addr() string {
	return s.Listener.Addr().String()
}

func startHubFenceNode(t *testing.T) *hubFenceNode {
	t.Helper()
	keys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	shortID, err := GenerateShortID(8)
	if err != nil {
		t.Fatalf("GenerateShortID: %v", err)
	}
	cover := startNamedTLSServer(t, "cover")
	node := &hubFenceNode{
		hub:   startNamedTLSServer(t, "hub"),
		other: startNamedTLSServerOn(t, "other", "127.0.0.2"),
		wg:    startNamedTLSServer(t, "wg"),
	}
	const sni = "reality-hub-e2e.internal.test"
	serverPort := freeTCPPort(t)
	hubUUID, dataUUID := randomUUID(t), randomUUID(t)

	config := fmt.Sprintf(hubFenceNodeConfig, serverPort, dataUUID, hubUUID, sni, cover.port(),
		keys.PrivateKey, shortID, node.hub.port(), node.wg.port())
	ctx := serverRegistryContext(context.Background())
	options, err := sjson.UnmarshalExtendedContext[option.Options](ctx, []byte(config))
	if err != nil {
		t.Fatalf("parse node config: %v", err)
	}
	engine, err := box.New(box.Options{Context: ctx, Options: options})
	if err != nil {
		t.Fatalf("build node: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("start node: %v", err)
	}
	t.Cleanup(func() { engine.Close() })

	node.hubUser = state.RealityProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: serverPort,
		UUID:       hubUUID,
		PublicKey:  keys.PublicKey,
		ShortID:    shortID,
		ServerName: sni,
	}
	node.dataUser = node.hubUser
	node.dataUser.UUID = dataUUID
	return node
}

type liveProxy struct {
	mgr        *ProxyManager
	port       int
	user, pass string
}

func startHubProxy(t *testing.T, profile state.RealityProfile) *liveProxy {
	t.Helper()
	mgr := NewProxyManager(state.NewLogStore(256))
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	port, err := mgr.Start(ctx, profile)
	if err != nil {
		t.Fatalf("ProxyManager.Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })
	user, pass := mgr.Credentials()
	return &liveProxy{mgr: mgr, port: port, user: user, pass: pass}
}

// connectVia opens an HTTP CONNECT tunnel through the proxy; a non-200 reply
// comes back as an error carrying the status line.
func connectVia(t *testing.T, proxyPort int, user, pass, target string) (net.Conn, error) {
	t.Helper()
	raw, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(proxyPort)), 5*time.Second)
	if err != nil {
		return nil, err
	}
	raw.SetDeadline(time.Now().Add(15 * time.Second))
	request := fmt.Sprintf("CONNECT %s HTTP/1.1\r\nHost: %s\r\n", target, target)
	if user != "" {
		creds := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
		request += "Proxy-Authorization: Basic " + creds + "\r\n"
	}
	if _, err := io.WriteString(raw, request+"\r\n"); err != nil {
		raw.Close()
		return nil, err
	}
	reader := bufio.NewReader(raw)
	resp, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil {
		raw.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		raw.Close()
		return nil, fmt.Errorf("CONNECT %s: %s", target, resp.Status)
	}
	if reader.Buffered() > 0 {
		raw.Close()
		return nil, errors.New("proxy sent bytes before the tunnel opened")
	}
	return raw, nil
}

// fetchThrough speaks TLS + GET over an open tunnel and returns the body,
// i.e. the name of whichever backend the node actually delivered us to.
func fetchThrough(conn net.Conn, serverName string) (string, error) {
	defer conn.Close()
	tconn := tls.Client(conn, &tls.Config{ServerName: serverName, InsecureSkipVerify: true})
	if err := tconn.Handshake(); err != nil {
		return "", fmt.Errorf("tls through tunnel: %w", err)
	}
	fmt.Fprintf(tconn, "GET / HTTP/1.1\r\nHost: %s\r\nConnection: close\r\n\r\n", serverName)
	resp, err := http.ReadResponse(bufio.NewReader(tconn), nil)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
}

func reachThrough(t *testing.T, proxy *liveProxy, target, serverName string) (string, error) {
	t.Helper()
	conn, err := connectVia(t, proxy.port, proxy.user, proxy.pass, target)
	if err != nil {
		return "", err
	}
	return fetchThrough(conn, serverName)
}

// dialDataPlaneTCP opens a TCP stream as the data-plane user with the
// production data-plane builder, i.e. no Vision.
func dialDataPlaneTCP(t *testing.T, profile state.RealityProfile, target string) net.Conn {
	t.Helper()
	engine, err := box.New(box.Options{
		Context: registryContext(context.Background()),
		Options: option.Options{
			Log: &option.LogOptions{Level: "warn"},
			Outbounds: []option.Outbound{{
				Type:    C.TypeVLESS,
				Tag:     outboundTag,
				Options: buildOutboundOptions(profile, profile.RemoteHost, profile.RemotePort, profile.ServerName),
			}},
		},
	})
	if err != nil {
		t.Fatalf("build data-plane client: %v", err)
	}
	if err := engine.Start(); err != nil {
		t.Fatalf("start data-plane client: %v", err)
	}
	t.Cleanup(func() { engine.Close() })
	out, _ := engine.Outbound().Outbound(outboundTag)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := out.DialContext(ctx, "tcp", M.ParseSocksaddr(target))
	if err != nil {
		t.Fatalf("data-plane dial: %v", err)
	}
	conn.SetDeadline(time.Now().Add(15 * time.Second))
	return conn
}

func TestHubProxyFence(t *testing.T) {
	node := startHubFenceNode(t)
	proxy := startHubProxy(t, node.hubUser)
	t.Cleanup(func() {
		if hits := node.other.hits.Load(); hits != 0 {
			t.Errorf("the non-hub server was reached %d times; the node fence leaked", hits)
		}
	})

	t.Run("hub by name reaches the hub", func(t *testing.T) {
		body, err := reachThrough(t, proxy, net.JoinHostPort("hub.pangea.test", strconv.Itoa(node.hub.port())), "hub.pangea.test")
		if err != nil {
			t.Fatalf("reach hub: %v", err)
		}
		if body != "hub" {
			t.Fatalf("body = %q, want hub", body)
		}
	})

	t.Run("another loopback server is overridden to the hub", func(t *testing.T) {
		body, err := reachThrough(t, proxy, node.other.addr(), "other.pangea.test")
		if err != nil {
			t.Fatalf("reach through override: %v", err)
		}
		if body != "hub" {
			t.Fatalf("body = %q, want hub: the override must win over the requested destination", body)
		}
	})

	t.Run("an internet address is overridden to the hub", func(t *testing.T) {
		body, err := reachThrough(t, proxy, "1.1.1.1:443", "one.one.one.one")
		if err != nil {
			t.Fatalf("reach through override: %v", err)
		}
		if body != "hub" {
			t.Fatalf("body = %q, want hub", body)
		}
	})

	t.Run("the data-plane user cannot reach the hub", func(t *testing.T) {
		dataProxy := startHubProxy(t, node.dataUser)
		hubHitsBefore := node.hub.hits.Load()
		body, err := reachThrough(t, dataProxy, net.JoinHostPort("127.0.0.1", strconv.Itoa(node.hub.port())), "hub.pangea.test")
		t.Logf("data-plane uuid through the hub proxy: body=%q err=%v", body, err)
		if err == nil && body == "hub" {
			t.Fatal("the data-plane user reached the hub")
		}
		if hits := node.hub.hits.Load(); hits != hubHitsBefore {
			t.Fatalf("hub hit count moved %d -> %d for the data-plane user", hubHitsBefore, hits)
		}
	})

	t.Run("the data-plane user with its own flow lands on the WireGuard stand-in", func(t *testing.T) {
		conn := dialDataPlaneTCP(t, node.dataUser, net.JoinHostPort("127.0.0.1", strconv.Itoa(node.hub.port())))
		body, err := fetchThrough(conn, "hub.pangea.test")
		if err != nil {
			t.Fatalf("reach through the data-plane user: %v", err)
		}
		if body != "wg" {
			t.Fatalf("body = %q, want wg: auth_user must single out the hub user only", body)
		}
	})

	t.Run("the proxy demands its credential", func(t *testing.T) {
		conn, err := connectVia(t, proxy.port, "", "", net.JoinHostPort("127.0.0.1", strconv.Itoa(node.hub.port())))
		if err == nil {
			conn.Close()
			t.Fatal("CONNECT without Proxy-Authorization was accepted")
		}
	})
}

func TestHubProxyLifecycle(t *testing.T) {
	node := startHubFenceNode(t)
	proxy := startHubProxy(t, node.hubUser)
	ctx := context.Background()

	noisy := node.hubUser
	noisy.Flow, noisy.LocalPort, noisy.TargetPort = "", 4444, 51820
	again, err := proxy.mgr.Start(ctx, noisy)
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if again != proxy.port {
		t.Fatalf("second Start with the same hub profile rebound to %d, want %d", again, proxy.port)
	}

	rotated := node.hubUser
	rotated.UUID = node.dataUser.UUID
	port, err := proxy.mgr.Start(ctx, rotated)
	if err != nil {
		t.Fatalf("Start with a rotated uuid: %v", err)
	}
	if user, pass := proxy.mgr.Credentials(); port <= 0 || user == proxy.user || pass == proxy.pass {
		t.Fatalf("rotation kept the old session: port=%d, credential rotated=%v", port, user != proxy.user)
	}

	port, err = proxy.mgr.Start(ctx, node.hubUser)
	if err != nil {
		t.Fatalf("Start back on the hub user: %v", err)
	}
	user, pass := proxy.mgr.Credentials()
	body, err := reachThrough(t, &liveProxy{port: port, user: user, pass: pass}, "hub.pangea.test:443", "hub.pangea.test")
	if err != nil || body != "hub" {
		t.Fatalf("after rebinding: body=%q err=%v, want hub", body, err)
	}

	if err := proxy.mgr.Stop(ctx); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := proxy.mgr.Port(); got != 0 {
		t.Fatalf("Port() after Stop = %d, want 0", got)
	}
	if conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second); err == nil {
		conn.Close()
		t.Fatal("the proxy port still accepts connections after Stop")
	}
}
