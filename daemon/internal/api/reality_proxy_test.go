package api

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

type fakeRealityProxy struct {
	mu       sync.Mutex
	started  []state.RealityProfile
	stopped  int
	startErr error
	port     int
	onStart  func(state.RealityProfile)
}

func (f *fakeRealityProxy) Start(_ context.Context, profile state.RealityProfile) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.onStart != nil {
		f.onStart(profile)
	}
	f.started = append(f.started, profile)
	if f.startErr != nil {
		return 0, f.startErr
	}
	f.port = 41000
	return f.port, nil
}

func (f *fakeRealityProxy) Stop(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped++
	f.port = 0
	return nil
}

func (f *fakeRealityProxy) Port() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.port
}

func (f *fakeRealityProxy) Credentials() (string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.port == 0 {
		return "", ""
	}
	return "proxy-user", "proxy-pass"
}

func (f *fakeRealityProxy) startCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.started)
}

var testRealityPublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 32))

func validRealityProxyRequest() realityProxyStartRequest {
	return realityProxyStartRequest{
		RemoteHost: "203.0.113.10",
		RemotePort: 443,
		UUID:       "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
		PublicKey:  testRealityPublicKey,
		ShortID:    "ab12cd34",
		ServerName: "www.microsoft.com",
	}
}

func TestValidateRealityProxyStartRequest(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*realityProxyStartRequest)
		ok     bool
	}{
		{"valid", func(*realityProxyStartRequest) {}, true},
		{"hostname remote", func(r *realityProxyStartRequest) { r.RemoteHost = "node-1.pangea.example" }, true},
		{"empty short id", func(r *realityProxyStartRequest) { r.ShortID = "" }, false},
		{"16-char short id", func(r *realityProxyStartRequest) { r.ShortID = "0123456789abcdef" }, true},
		{"uppercase uuid", func(r *realityProxyStartRequest) { r.UUID = strings.ToUpper(r.UUID) }, true},
		{"port 1", func(r *realityProxyStartRequest) { r.RemotePort = 1 }, true},
		{"port 65535", func(r *realityProxyStartRequest) { r.RemotePort = 65535 }, true},

		{"empty remote", func(r *realityProxyStartRequest) { r.RemoteHost = "" }, false},
		{"remote with whitespace", func(r *realityProxyStartRequest) { r.RemoteHost = " 203.0.113.10" }, false},
		{"remote with control char", func(r *realityProxyStartRequest) { r.RemoteHost = "node\x00.example" }, false},
		{"remote with port", func(r *realityProxyStartRequest) { r.RemoteHost = "203.0.113.10:443" }, false},
		{"remote ipv6", func(r *realityProxyStartRequest) { r.RemoteHost = "2001:db8::1" }, false},
		{"remote empty label", func(r *realityProxyStartRequest) { r.RemoteHost = "node..example" }, false},
		{"remote leading hyphen", func(r *realityProxyStartRequest) { r.RemoteHost = "-node.example" }, false},
		{"remote bogus dotted quad", func(r *realityProxyStartRequest) { r.RemoteHost = "999.1.1.1" }, false},
		{"remote label too long", func(r *realityProxyStartRequest) { r.RemoteHost = strings.Repeat("a", 64) + ".example" }, false},
		{"port 0", func(r *realityProxyStartRequest) { r.RemotePort = 0 }, false},
		{"port 65536", func(r *realityProxyStartRequest) { r.RemotePort = 65536 }, false},
		{"empty uuid", func(r *realityProxyStartRequest) { r.UUID = "" }, false},
		{"uuid without hyphens", func(r *realityProxyStartRequest) { r.UUID = "6ba7b8109dad11d180b400c04fd430c8" }, false},
		{"uuid non-hex", func(r *realityProxyStartRequest) { r.UUID = "6ba7b810-9dad-11d1-80b4-00c04fd430cg" }, false},
		{"uuid braces", func(r *realityProxyStartRequest) { r.UUID = "{6ba7b810-9dad-11d1-80b4-00c04fd430c8}" }, false},
		{"empty public key", func(r *realityProxyStartRequest) { r.PublicKey = "" }, false},
		{"public key padded std base64", func(r *realityProxyStartRequest) {
			r.PublicKey = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{0xfb}, 32))
		}, false},
		{"public key 31 bytes", func(r *realityProxyStartRequest) {
			r.PublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 31))
		}, false},
		{"public key 33 bytes", func(r *realityProxyStartRequest) {
			r.PublicKey = base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{7}, 33))
		}, false},
		{"public key with newline", func(r *realityProxyStartRequest) { r.PublicKey = r.PublicKey[:20] + "\n" + r.PublicKey[20:] }, false},
		{"short id odd length", func(r *realityProxyStartRequest) { r.ShortID = "abc" }, false},
		{"short id non-hex", func(r *realityProxyStartRequest) { r.ShortID = "zz" }, false},
		{"short id too long", func(r *realityProxyStartRequest) { r.ShortID = "0123456789abcdef01" }, false},
		{"empty server name", func(r *realityProxyStartRequest) { r.ServerName = "" }, false},
		{"server name with space", func(r *realityProxyStartRequest) { r.ServerName = "www.microsoft .com" }, false},
		{"server name ip literal", func(r *realityProxyStartRequest) { r.ServerName = "203.0.113.10" }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := validRealityProxyRequest()
			tc.mutate(&req)
			err := validateRealityProxyStartRequest(req)
			if tc.ok && err != nil {
				t.Fatalf("validate(%+v) = %v, want nil", req, err)
			}
			if !tc.ok && err == nil {
				t.Fatalf("validate(%+v) = nil, want an error", req)
			}
		})
	}
}

const realityProxyTestToken = "0123456789abcdef"

func realityProxyHandler(t *testing.T, proxy *fakeRealityProxy) http.Handler {
	t.Helper()
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
	if proxy != nil {
		svc.SetRealityProxy(proxy)
	}
	return NewHandler(realityProxyTestToken, svc)
}

func serveRealityProxy(handler http.Handler, method, path, body string, authed bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://127.0.0.1"+path, strings.NewReader(body))
	if authed {
		req.Header.Set("Authorization", "Bearer "+realityProxyTestToken)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func realityProxyStartBody(t *testing.T, req realityProxyStartRequest) string {
	t.Helper()
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(raw)
}

func decodeJSONObject(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	return body
}

func TestRealityProxyStartRoute(t *testing.T) {
	t.Run("starts the proxy and returns its port and credential", func(t *testing.T) {
		proxy := &fakeRealityProxy{}
		rec := serveRealityProxy(realityProxyHandler(t, proxy), http.MethodPost, "/realityproxy/start",
			`{"remoteHost":"203.0.113.10","remotePort":443,"uuid":"6ba7b810-9dad-11d1-80b4-00c04fd430c8",`+
				`"publicKey":"`+testRealityPublicKey+`","shortId":"ab12cd34","serverName":"www.microsoft.com"}`, true)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", rec.Code, rec.Body.String())
		}
		body := decodeJSONObject(t, rec)
		if body["ok"] != true || body["port"] != float64(41000) ||
			body["proxyUsername"] != "proxy-user" || body["proxyPassword"] != "proxy-pass" {
			t.Fatalf("body = %v, want ok/port/proxyUsername/proxyPassword", body)
		}
		want := state.RealityProfile{
			RemoteHost: "203.0.113.10",
			RemotePort: 443,
			UUID:       "6ba7b810-9dad-11d1-80b4-00c04fd430c8",
			PublicKey:  testRealityPublicKey,
			ShortID:    "ab12cd34",
			ServerName: "www.microsoft.com",
		}
		if !slices.Equal(proxy.started, []state.RealityProfile{want}) {
			t.Fatalf("proxy started with %+v, want %+v", proxy.started, want)
		}
	})

	t.Run("refuses an unauthenticated post", func(t *testing.T) {
		proxy := &fakeRealityProxy{}
		rec := serveRealityProxy(realityProxyHandler(t, proxy), http.MethodPost, "/realityproxy/start",
			realityProxyStartBody(t, validRealityProxyRequest()), false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if proxy.startCount() != 0 {
			t.Fatal("an unauthenticated caller started the proxy")
		}
	})

	t.Run("refuses a GET", func(t *testing.T) {
		rec := serveRealityProxy(realityProxyHandler(t, &fakeRealityProxy{}), http.MethodGet, "/realityproxy/start", "", true)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("rejects invalid json", func(t *testing.T) {
		rec := serveRealityProxy(realityProxyHandler(t, &fakeRealityProxy{}), http.MethodPost, "/realityproxy/start", "{", true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if body := decodeJSONObject(t, rec); body["error"] != "invalid json" {
			t.Fatalf("body = %v, want the invalid json error", body)
		}
	})

	t.Run("rejects an invalid request before starting anything", func(t *testing.T) {
		proxy := &fakeRealityProxy{}
		req := validRealityProxyRequest()
		req.UUID = "not-a-uuid"
		rec := serveRealityProxy(realityProxyHandler(t, proxy), http.MethodPost, "/realityproxy/start", realityProxyStartBody(t, req), true)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rec.Code)
		}
		if body := decodeJSONObject(t, rec); !strings.Contains(body["error"].(string), "uuid") {
			t.Fatalf("body = %v, want the uuid reason", body)
		}
		if proxy.startCount() != 0 {
			t.Fatal("an invalid request reached the proxy")
		}
	})

	t.Run("a start failure carries its reason", func(t *testing.T) {
		proxy := &fakeRealityProxy{startErr: errors.New("reality hub proxy: handshake: reality verification failed")}
		rec := serveRealityProxy(realityProxyHandler(t, proxy), http.MethodPost, "/realityproxy/start",
			realityProxyStartBody(t, validRealityProxyRequest()), true)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		body := decodeJSONObject(t, rec)
		if body["ok"] != false || !strings.Contains(body["error"].(string), "reality verification failed") {
			t.Fatalf("body = %v, want ok=false with the start error", body)
		}
		if _, has := body["port"]; has {
			t.Fatalf("body = %v, a failure must not carry a port", body)
		}
	})

	t.Run("reports an unwired proxy", func(t *testing.T) {
		rec := serveRealityProxy(realityProxyHandler(t, nil), http.MethodPost, "/realityproxy/start",
			realityProxyStartBody(t, validRealityProxyRequest()), true)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rec.Code)
		}
		if body := decodeJSONObject(t, rec); body["error"] != "reality proxy is not available" {
			t.Fatalf("body = %v, want the unavailable error", body)
		}
	})
}

func TestRealityProxyStopRoute(t *testing.T) {
	t.Run("stops the proxy", func(t *testing.T) {
		proxy := &fakeRealityProxy{port: 41000}
		rec := serveRealityProxy(realityProxyHandler(t, proxy), http.MethodPost, "/realityproxy/stop", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		if body := decodeJSONObject(t, rec); body["ok"] != true {
			t.Fatalf("body = %v, want ok=true", body)
		}
		if proxy.stopped != 1 {
			t.Fatalf("Stop called %d times, want 1", proxy.stopped)
		}
	})

	t.Run("refuses an unauthenticated post", func(t *testing.T) {
		proxy := &fakeRealityProxy{port: 41000}
		rec := serveRealityProxy(realityProxyHandler(t, proxy), http.MethodPost, "/realityproxy/stop", "", false)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want 401", rec.Code)
		}
		if proxy.stopped != 0 {
			t.Fatal("an unauthenticated caller stopped the proxy")
		}
	})

	t.Run("refuses a GET", func(t *testing.T) {
		rec := serveRealityProxy(realityProxyHandler(t, &fakeRealityProxy{}), http.MethodGet, "/realityproxy/stop", "", true)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rec.Code)
		}
	})

	t.Run("is a no-op when unwired", func(t *testing.T) {
		rec := serveRealityProxy(realityProxyHandler(t, nil), http.MethodPost, "/realityproxy/stop", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
	})
}

func TestRealityProxyService_UnwiredProxy(t *testing.T) {
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})

	if _, err := svc.StartRealityProxy(context.Background(), state.RealityProfile{RemoteHost: "203.0.113.10"}); err == nil ||
		err.Error() != "reality proxy is not available" {
		t.Fatalf("StartRealityProxy() error = %v, want the unavailable error", err)
	}
	if err := svc.StopRealityProxy(context.Background()); err != nil {
		t.Fatalf("StopRealityProxy() = %v, want nil", err)
	}
	if user, pass := svc.RealityProxyCredentials(); user != "" || pass != "" {
		t.Fatal("RealityProxyCredentials() returned a credential with no proxy wired")
	}
}

// The permit must land before the dial, or a Lockdown lock blocks the very
// connection the app needs to reach the hub through.
func TestRealityProxyService_PermitsRemoteBeforeStarting(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{"198.51.100.9"}, Locked: true})

	var permittedAtStart []string
	proxy := &fakeRealityProxy{onStart: func(state.RealityProfile) {
		ks.mu.Lock()
		defer ks.mu.Unlock()
		permittedAtStart = slices.Clone(ks.enableEndpoints)
	}}
	svc.SetRealityProxy(proxy)

	port, err := svc.StartRealityProxy(context.Background(), state.RealityProfile{RemoteHost: "203.0.113.10", RemotePort: 443})
	if err != nil {
		t.Fatalf("StartRealityProxy() error = %v", err)
	}
	if port != 41000 {
		t.Fatalf("port = %d, want the proxy's 41000", port)
	}
	if !slices.Contains(permittedAtStart, "203.0.113.10") {
		t.Fatalf("lock permits when the proxy started = %v, want the node 203.0.113.10 already permitted", permittedAtStart)
	}
	if user, pass := svc.RealityProxyCredentials(); user != "proxy-user" || pass != "proxy-pass" {
		t.Fatal("RealityProxyCredentials() did not come from the live proxy")
	}
}

func TestRealityProxyService_StartsEvenWhenThePermitFails(t *testing.T) {
	ks := &fakeKillSwitch{active: true, enableErr: errors.New("wfp: access denied")}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, testProfile())
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true})
	proxy := &fakeRealityProxy{}
	svc.SetRealityProxy(proxy)

	if _, err := svc.StartRealityProxy(context.Background(), state.RealityProfile{RemoteHost: "203.0.113.10", RemotePort: 443}); err != nil {
		t.Fatalf("StartRealityProxy() error = %v, want the proxy started anyway", err)
	}
	if proxy.startCount() != 1 {
		t.Fatalf("proxy started %d times, want 1", proxy.startCount())
	}
	warned := slices.ContainsFunc(svc.Logs(0), func(e state.LogEntry) bool {
		return e.Level == state.LogWarn && strings.Contains(e.Msg, "could not permit reality hub proxy remote 203.0.113.10")
	})
	if !warned {
		t.Fatal("a failed permit was not logged as a warning")
	}
}
