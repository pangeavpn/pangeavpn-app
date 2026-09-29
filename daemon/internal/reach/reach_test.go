package reach

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// hubStub is a TLS server standing in for the hub; it records the SNI and
// nonce of every probe and answers with handler.
type hubStub struct {
	mu     sync.Mutex
	snis   []string
	nonces []string
	addr   string
}

func startHubStub(t *testing.T, handler http.HandlerFunc) *hubStub {
	t.Helper()
	stub := &hubStub{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.nonces = append(stub.nonces, r.URL.Query().Get("n"))
		stub.mu.Unlock()
		handler(w, r)
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

// echo is the hub's real behaviour, refusing anything not aimed at the route.
func echo(w http.ResponseWriter, r *http.Request) {
	if r.Host != HubHost || r.URL.Path != Path {
		http.Error(w, "wrong target", http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"n": r.URL.Query().Get("n")})
}

func TestProbe_EchoedNonceIsAnswered(t *testing.T) {
	stub := startHubStub(t, echo)
	if got := Probe(context.Background(), stub.dial); got != Answered {
		t.Fatalf("Probe = %s, want answered", got)
	}
	if snis, _ := stub.seen(); len(snis) != 1 || snis[0] != "" {
		t.Fatalf("probe sent SNI %q; a filter that blocks the hub by name would drop it", snis)
	}
}

func TestProbe_SendsAFreshNonceEachTime(t *testing.T) {
	stub := startHubStub(t, echo)
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
	cases := map[string]http.HandlerFunc{
		"wrong echo": func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]string{"n": "deadbeef"})
		},
		"rate limited": func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, `{"code":"RATE_LIMITED"}`, http.StatusTooManyRequests)
		},
		"block page": func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("<html>This site is blocked</html>"))
		},
		"upstream error": func(w http.ResponseWriter, _ *http.Request) {
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
	stub := startHubStub(t, func(w http.ResponseWriter, _ *http.Request) {
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
	stub := startHubStub(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Pad", strings.Repeat("a", 64<<10))
		echo(w, r)
	})
	if got := Probe(context.Background(), stub.dial); got != Silent {
		t.Fatalf("Probe = %s, want silent: a reply past the size cap must not be trusted", got)
	}
}
