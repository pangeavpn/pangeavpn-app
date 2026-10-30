//go:build android || ios

package mobile

import (
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRequest is one inner request the fake hub opened.
type fakeRequest struct {
	Method string
	// Route keeps its query string; handlers are matched without it.
	Route   string
	Headers map[string]string
	Body    json.RawMessage
}

type fakeHandler func(fakeRequest) (status int, body any)

// fakeHub answers sealed requests the way the real hub does, so a test can
// drive an exported hub call end to end.
type fakeHub struct {
	t   *testing.T
	hub testHub
	srv *httptest.Server

	mu       sync.Mutex
	routes   map[string]fakeHandler
	recorded []fakeRequest
}

// newFakeHub installs itself as the live hub route until the test ends.
func newFakeHub(t *testing.T) *fakeHub {
	t.Helper()
	hub, keys := newTestHub(t)
	h := &fakeHub{t: t, hub: hub, routes: map[string]fakeHandler{}}
	h.srv = httptest.NewServer(http.HandlerFunc(h.serve))

	previous := hubChannelKeys
	hubChannelKeys = func() channelKeys { return keys }
	t.Cleanup(func() {
		resetGlobals()
		hubChannelKeys = previous
		h.srv.Close()
	})
	h.online()
	return h
}

// handle answers route (query stripped) with fn's status and JSON body. fn runs
// off the test goroutine, so it must use t.Errorf, never t.Fatalf.
func (h *fakeHub) handle(route string, fn func(fakeRequest) (status int, body any)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.routes[route] = fn
}

// requests returns every request opened so far, in arrival order.
func (h *fakeHub) requests() []fakeRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]fakeRequest(nil), h.recorded...)
}

// requestsTo returns the requests whose route, query stripped, is route.
func (h *fakeHub) requestsTo(route string) []fakeRequest {
	var out []fakeRequest
	for _, req := range h.requests() {
		if stripQuery(req.Route) == route {
			out = append(out, req)
		}
	}
	return out
}

// online installs the fake as the live route and lifts any search cooldown.
func (h *fakeHub) online() {
	route := &hubPath{kind: "normal", detail: "fake", base: h.srv.URL, client: h.srv.Client()}
	hubMu.Lock()
	activeHubPath = route
	hubTrack = hubTracker{}
	hubMu.Unlock()
}

// offline makes the hub unreachable without a network search; see hubUnreachable.
func (h *fakeHub) offline() { hubUnreachable() }

// hubUnreachable drops the route and starts the search cooldown, so the next
// request fails fast instead of searching the real network.
func hubUnreachable() {
	hubMu.Lock()
	route := activeHubPath
	activeHubPath = nil
	hubTrack.searchFailed(time.Now())
	hubMu.Unlock()
	if route != nil {
		route.client.CloseIdleConnections()
	}
}

func stripQuery(route string) string {
	path, _, _ := strings.Cut(route, "?")
	return path
}

// serve opens one envelope, dispatches its inner request and seals the answer.
// Unregistered routes get a 404 the way the hub's router would answer.
func (h *fakeHub) serve(w http.ResponseWriter, r *http.Request) {
	var env wireEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		h.fail(w, "envelope is not JSON: %v", err)
		return
	}
	open, seal, aad, err := h.channel(r.URL.Path, env)
	if err != nil {
		h.fail(w, "%v", err)
		return
	}
	plain, err := gcmOpenRaw(open, env.IV, env.CT, env.Tag, aad)
	if err != nil {
		h.fail(w, "fake hub could not open the envelope: %v", err)
		return
	}
	var inner wireInner
	if err := json.Unmarshal(plain, &inner); err != nil {
		h.fail(w, "inner request is not JSON: %v", err)
		return
	}

	req := fakeRequest{Method: inner.Method, Route: inner.Route, Headers: inner.Headers, Body: inner.Body}
	h.mu.Lock()
	h.recorded = append(h.recorded, req)
	fn := h.routes[stripQuery(inner.Route)]
	h.mu.Unlock()

	status, body := http.StatusNotFound, any(map[string]string{"error": "Not found", "code": "NOT_FOUND"})
	if fn != nil {
		status, body = fn(req)
	}
	answer, err := json.Marshal(map[string]any{"status": status, "body": body})
	if err != nil {
		h.fail(w, "marshal fake answer: %v", err)
		return
	}
	sealed, err := gcmSeal(seal, answer, aad)
	if err != nil {
		h.fail(w, "seal fake answer: %v", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(sealed)
}

// channel derives the request and reply keys for the envelope's route version.
func (h *fakeHub) channel(path string, env wireEnvelope) (open, seal, aad []byte, err error) {
	shared, err := h.sharedSecret(env.Eph)
	if err != nil {
		return nil, nil, nil, err
	}
	if path == routeSecureV1 {
		key, err := hkdf.Key(sha256.New, shared, hkdfSalt, hkdfInfo, 32)
		return key, key, nil, err
	}
	if path != routeSecureV2 {
		return nil, nil, nil, fmt.Errorf("unexpected secure route %q", path)
	}
	kemCT, err := base64.StdEncoding.DecodeString(env.Kem)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decode kem: %w", err)
	}
	kemSecret, err := h.hub.kem.Decapsulate(kemCT)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("decapsulate: %w", err)
	}
	c2s, s2c, err := deriveV2Keys(shared, kemSecret)
	return c2s, s2c, []byte(env.Eph + "." + env.Kem), err
}

func (h *fakeHub) sharedSecret(ephB64 string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(ephB64)
	if err != nil {
		return nil, fmt.Errorf("decode eph: %w", err)
	}
	pub, err := h.hub.x25519.Curve().NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("parse eph: %w", err)
	}
	return h.hub.x25519.ECDH(pub)
}

func (h *fakeHub) fail(w http.ResponseWriter, format string, args ...any) {
	h.t.Errorf("fake hub: "+format, args...)
	http.Error(w, "fake hub failure", http.StatusInternalServerError)
}

// gcmOpenRaw is gcmOpenB64 without the Fatalf, safe off the test goroutine.
func gcmOpenRaw(key []byte, iv, ct, tag string, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	rawIV, err := base64.StdEncoding.DecodeString(iv)
	if err != nil {
		return nil, err
	}
	rawCT, err := base64.StdEncoding.DecodeString(ct)
	if err != nil {
		return nil, err
	}
	rawTag, err := base64.StdEncoding.DecodeString(tag)
	if err != nil {
		return nil, err
	}
	if len(rawIV) != gcm.NonceSize() {
		return nil, fmt.Errorf("nonce is %d bytes", len(rawIV))
	}
	return gcm.Open(nil, rawIV, append(rawCT, rawTag...), aad)
}

func TestFakeHubAnswersSealedRequests(t *testing.T) {
	initForTest(t, map[string]string{keyLicense: "lk"})
	h := newFakeHub(t)
	h.handle("/api/device/list", func(req fakeRequest) (int, any) {
		return http.StatusOK, map[string]any{"devices": []any{}}
	})

	for name, kem := range map[string]bool{"v2": true, "v1": false} {
		t.Run(name, func(t *testing.T) {
			keys := hubChannelKeys()
			if !kem {
				keys.kemPublic = ""
			}
			previous := hubChannelKeys
			hubChannelKeys = func() channelKeys { return keys }
			defer func() { hubChannelKeys = previous }()

			var out struct {
				Devices []any `json:"devices"`
			}
			if err := hubRequest(http.MethodGet, "/api/device/list?page=1", nil, &out); err != nil || out.Devices == nil {
				t.Fatalf("hubRequest = %v, devices %v", err, out.Devices)
			}
		})
	}
	got := h.requestsTo("/api/device/list")
	if len(got) != 2 || got[0].Route != "/api/device/list?page=1" || got[0].Headers["X-License-Key"] != "lk" {
		t.Fatalf("recorded %+v", got)
	}

	if err := hubRequest(http.MethodGet, "/api/unknown", nil, nil); hubErrorCode(err) != "NOT_FOUND" {
		t.Fatalf("unregistered route = %v, want a NOT_FOUND answer", err)
	}

	h.offline()
	err := hubRequest(http.MethodGet, "/api/device/list", nil, nil)
	if err == nil || !strings.Contains(err.Error(), "hub unreachable: retrying in") || hubAnswered(err) {
		t.Fatalf("offline = %v, want a fast unreachable error", err)
	}
	h.online()
	if err := hubRequest(http.MethodGet, "/api/device/list", nil, nil); err != nil {
		t.Fatalf("back online = %v", err)
	}
}
