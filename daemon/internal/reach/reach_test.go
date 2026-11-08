package reach

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/securechannel"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/securechannel/securechanneltest"
)

// hubStub is a TLS server standing in for the hub. It holds the hub's keys,
// opens each sealed probe, records its SNI and nonce, and answers with handler.
type hubStub struct {
	mu     sync.Mutex
	snis   []string
	nonces []string
	addr   string
}

// hubHandler sees the outer request plus the opened inner one; req.Route is
// empty and reply nil when the envelope did not open.
type hubHandler func(w http.ResponseWriter, r *http.Request, req securechanneltest.Request, reply securechanneltest.Reply)

func startHubStub(t *testing.T, handler hubHandler) *hubStub {
	t.Helper()
	hub, keys, err := securechanneltest.NewHub()
	if err != nil {
		t.Fatal(err)
	}
	previous := hubKeys
	hubKeys = func() securechannel.Keys { return keys }
	t.Cleanup(func() { hubKeys = previous })

	stub := &hubStub{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		envelope, _ := io.ReadAll(r.Body)
		req, reply, err := hub.Open(envelope)
		if err != nil {
			req, reply = securechanneltest.Request{}, nil
		}
		if u, err := url.Parse(req.Route); err == nil {
			stub.mu.Lock()
			stub.nonces = append(stub.nonces, u.Query().Get("n"))
			stub.mu.Unlock()
		}
		handler(w, r, req, reply)
	}))
	srv.TLS = &tls.Config{GetConfigForClient: func(hello *tls.ClientHelloInfo) (*tls.Config, error) {
		stub.mu.Lock()
		stub.snis = append(stub.snis, hello.ServerName)
		stub.mu.Unlock()
		return nil, nil
	}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	stub.addr = srv.Listener.Addr().String()
	return stub
}

func (s *hubStub) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, "tcp", s.addr)
}

func (s *hubStub) seen() (snis, nonces []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.snis...), append([]string(nil), s.nonces...)
}

// sealedJSON answers 200 with an inner response sealed for the request.
func sealedJSON(t *testing.T, w http.ResponseWriter, reply securechanneltest.Reply, status int, body string) {
	raw, err := reply(securechannel.InnerResponse{Status: status, Body: json.RawMessage(body)})
	if err != nil {
		t.Error(err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(raw)
}

// echo is the hub's real behaviour: the sealed route only, the nonce sealed back.
func echo(t *testing.T) hubHandler {
	return func(w http.ResponseWriter, r *http.Request, req securechanneltest.Request, reply securechanneltest.Reply) {
		u, err := url.Parse(req.Route)
		if r.Host != HubHost || r.Method != http.MethodPost || r.URL.Path != securechannel.Route ||
			reply == nil || err != nil || req.Method != http.MethodGet || u.Path != Path {
			http.Error(w, "wrong target", http.StatusBadRequest)
			return
		}
		body, _ := json.Marshal(map[string]string{"n": u.Query().Get("n")})
		sealedJSON(t, w, reply, http.StatusOK, string(body))
	}
}

func TestProbe_EchoedNonceIsAnswered(t *testing.T) {
	stub := startHubStub(t, echo(t))
	if got := Probe(context.Background(), stub.dial); got != Answered {
		t.Fatalf("Probe = %s, want answered", got)
	}
	if snis, _ := stub.seen(); len(snis) != 1 || snis[0] != "" {
		t.Fatalf("probe sent SNI %q; a filter that blocks the hub by name would drop it", snis)
	}
}

func TestProbe_SendsAFreshNonceEachTime(t *testing.T) {
	stub := startHubStub(t, echo(t))
	Probe(context.Background(), stub.dial)
	Probe(context.Background(), stub.dial)
	_, nonces := stub.seen()
	pattern := regexp.MustCompile(`^[0-9a-f]{32}$`)
	for _, n := range nonces {
		if !pattern.MatchString(n) {
			t.Fatalf("nonce %q is not 32 lowercase hex chars", n)
		}
	}
	if len(nonces) != 2 || nonces[0] == nonces[1] {
		t.Fatalf("nonces %v: two probes must send two different nonces", nonces)
	}
}

func TestProbe_Refusals(t *testing.T) {
	cases := map[string]hubHandler{
		"wrong echo": func(w http.ResponseWriter, _ *http.Request, _ securechanneltest.Request, reply securechanneltest.Reply) {
			sealedJSON(t, w, reply, http.StatusOK, `{"n":"deadbeef"}`)
		},
		"inner refusal": func(w http.ResponseWriter, _ *http.Request, _ securechanneltest.Request, reply securechanneltest.Reply) {
			sealedJSON(t, w, reply, http.StatusForbidden, `{"code":"ROUTE_NOT_ALLOWED"}`)
		},
		"unsealed echo": func(w http.ResponseWriter, _ *http.Request, req securechanneltest.Request, _ securechanneltest.Reply) {
			u, _ := url.Parse(req.Route)
			_ = json.NewEncoder(w).Encode(map[string]string{"n": u.Query().Get("n")})
		},
		"rate limited": func(w http.ResponseWriter, _ *http.Request, _ securechanneltest.Request, _ securechanneltest.Reply) {
			http.Error(w, `{"code":"RATE_LIMITED"}`, http.StatusTooManyRequests)
		},
		"block page": func(w http.ResponseWriter, _ *http.Request, _ securechanneltest.Request, _ securechanneltest.Reply) {
			_, _ = w.Write([]byte("<html>This site is blocked</html>"))
		},
		"upstream error": func(w http.ResponseWriter, _ *http.Request, _ securechanneltest.Request, _ securechanneltest.Reply) {
			http.Error(w, "bad gateway", http.StatusBadGateway)
		},
	}
	for name, handler := range cases {
		t.Run(name, func(t *testing.T) {
			stub := startHubStub(t, handler)
			if got := Probe(context.Background(), stub.dial); got != Refused {
				t.Fatalf("Probe = %s, want refused: something answered, just not our hub", got)
			}
		})
	}
}

// The plain GET this replaced put the route and nonce on the wire for anyone
// who intercepts the unverified TLS session; the request must be the envelope alone.
func TestProbe_SendsOnlyASealedEnvelope(t *testing.T) {
	var outer *http.Request
	stub := startHubStub(t, func(w http.ResponseWriter, r *http.Request, req securechanneltest.Request, reply securechanneltest.Reply) {
		outer = r
		echo(t)(w, r, req, reply)
	})
	if got := Probe(context.Background(), stub.dial); got != Answered {
		t.Fatalf("Probe = %s, want answered", got)
	}
	if outer.URL.RawQuery != "" || strings.Contains(outer.URL.String(), "reach") {
		t.Fatalf("outer request %s %s leaks the probe", outer.Method, outer.URL)
	}
}

func TestProbe_DialFailureIsSilent(t *testing.T) {
	dial := func(context.Context) (net.Conn, error) { return nil, errors.New("connect: i/o timeout") }
	if got := Probe(context.Background(), dial); got != Silent {
		t.Fatalf("Probe = %s, want silent", got)
	}
}

func TestProbe_NoReplyIsSilentWithinTheDeadline(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range held {
			conn.Close()
		}
	})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	dial := func(ctx context.Context) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", ln.Addr().String())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	if got := Probe(ctx, dial); got != Silent {
		t.Fatalf("Probe = %s, want silent", got)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Probe took %s; it must honour the caller's deadline", elapsed)
	}
}

func TestProbe_ResetBeforeReplyIsSilent(t *testing.T) {
	stub := startHubStub(t, func(w http.ResponseWriter, _ *http.Request, _ securechanneltest.Request, _ securechanneltest.Reply) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			conn.Close()
		}
	})
	if got := Probe(context.Background(), stub.dial); got != Silent {
		t.Fatalf("Probe = %s, want silent", got)
	}
}

func TestDirectDialer_UnknownInterfaceFails(t *testing.T) {
	_, err := DirectDialer("192.0.2.1", "no-such-interface-pangea")(context.Background())
	if err == nil {
		t.Fatal("dialing from a missing interface succeeded; it must never fall back to the default route")
	}
}

// TestProbe_OversizedReplyIsNotReadInFull: with no certificate check an on-path
// box can stream headers at the SYSTEM daemon, so the read must stop early.
func TestProbe_OversizedReplyIsNotReadInFull(t *testing.T) {
	stub := startHubStub(t, func(w http.ResponseWriter, r *http.Request, req securechanneltest.Request, reply securechanneltest.Reply) {
		w.Header().Set("X-Pad", strings.Repeat("a", 64<<10))
		echo(t)(w, r, req, reply)
	})
	if got := Probe(context.Background(), stub.dial); got != Silent {
		t.Fatalf("Probe = %s, want silent: a reply past the size cap must not be trusted", got)
	}
}
