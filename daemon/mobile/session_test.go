//go:build android || ios

package mobile

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// newSignedInHub starts a signed-in install, license "lk", against a fake hub.
func newSignedInHub(t *testing.T) (*memoryStore, *identityKeyPair, *fakeHub) {
	t.Helper()
	ident := mustIdentity(t)
	s, _ := initForTest(t, map[string]string{keyLicense: "lk", keyIdentity: ident.marshal()})
	return s, ident, newFakeHub(t)
}

func replyWith(status int, body any) func(fakeRequest) (int, any) {
	return func(fakeRequest) (int, any) { return status, body }
}

// replyCode answers the way the hub's error handler does.
func replyCode(status int, code string) func(fakeRequest) (int, any) {
	return replyWith(status, map[string]string{"error": "refused", "code": code, "requestId": "req-1"})
}

func bodyFields(t *testing.T, req fakeRequest) map[string]string {
	t.Helper()
	var fields map[string]string
	if err := json.Unmarshal(req.Body, &fields); err != nil {
		t.Fatalf("request body %s: %v", req.Body, err)
	}
	return fields
}

func regionsWith(t *testing.T, uid string) []serverInfo {
	t.Helper()
	s := serverWithEveryTransport(t)
	s.Cloak.UID = uid
	return []serverInfo{s}
}

func decodeSession(t *testing.T, raw string) (sessionJSON, map[string]any) {
	t.Helper()
	var session sessionJSON
	var loose map[string]any
	if err := json.Unmarshal([]byte(raw), &session); err != nil {
		t.Fatalf("session %s: %v", raw, err)
	}
	_ = json.Unmarshal([]byte(raw), &loose)
	return session, loose
}

func sessionState() (lk string, srvs []serverInfo, p *preparedTunnel, ident *identityKeyPair) {
	mu.Lock()
	defer mu.Unlock()
	return licenseKey, servers, prepared, identity
}

func TestRestoreSessionReturnsThePublicShapeAndCachesIt(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "mine")))

	raw, err := RestoreSession()
	if err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	session, _ := decodeSession(t, raw)
	if session.Cached || len(session.Servers) != 1 || session.Servers[0].ID != "fra" || !session.Servers[0].Cloak {
		t.Fatalf("session = %s", raw)
	}
	for _, leak := range []string{"uid", "password", "uuid", "mine"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("session leaks %q: %s", leak, raw)
		}
	}
	if got := h.requestsTo("/api/client/regions"); len(got) != 1 || !strings.Contains(got[0].Route, "identityPubkey=") {
		t.Fatalf("regions requests = %+v", got)
	}
	if cached := restoreCachedServers(s.Get(keyServers), cacheOwner("lk", ident.pubB64)); len(cached) != 1 || cached[0].Cloak.UID != "mine" {
		t.Fatalf("server cache = %+v", cached)
	}
}

func TestRestoreSessionFallsBackToTheCachedList(t *testing.T) {
	_, _, h := newSignedInHub(t)
	h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "mine")))
	if _, err := RestoreSession(); err != nil {
		t.Fatalf("online RestoreSession: %v", err)
	}
	mu.Lock()
	servers = nil
	mu.Unlock()

	h.offline()
	raw, err := RestoreSession()
	if err != nil {
		t.Fatalf("offline RestoreSession: %v", err)
	}
	session, loose := decodeSession(t, raw)
	if !session.Cached || loose["cached"] != true || len(session.Servers) != 1 || session.Servers[0].ID != "fra" {
		t.Fatalf("offline session = %s", raw)
	}
	if _, srvs, _, _ := sessionState(); len(srvs) != 1 || srvs[0].Cloak.UID != "mine" {
		t.Fatalf("Prepare could not find the cached server: %+v", srvs)
	}
}

func TestRestoreSessionWithoutACacheReportsTheFailure(t *testing.T) {
	_, _, h := newSignedInHub(t)
	h.offline()
	if _, err := RestoreSession(); err == nil || !strings.Contains(err.Error(), "hub unreachable") {
		t.Fatalf("RestoreSession = %v, want the unreachable error", err)
	}
}

func TestRestoreSessionNeverServesTheCacheOverAHubAnswer(t *testing.T) {
	cases := []struct {
		name     string
		reply    func(fakeRequest) (int, any)
		signsOut bool
	}{
		{"invalid license", replyCode(401, "INVALID_LICENSE"), true},
		{"device not registered", replyCode(404, "DEVICE_NOT_REGISTERED"), true},
		{"server error", replyCode(500, "INTERNAL_ERROR"), false},
		{"locked out", replyCode(429, "AUTH_LOCKED"), false},
		{"an answer that does not decode", replyWith(http.StatusOK, "not a list"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, h := newSignedInHub(t)
			h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "mine")))
			if _, err := RestoreSession(); err != nil {
				t.Fatalf("online RestoreSession: %v", err)
			}
			cache := s.Get(keyServers)

			h.handle("/api/client/regions", tc.reply)
			raw, err := RestoreSession()
			if err == nil {
				t.Fatalf("a hub answer was papered over with the cache: %s", raw)
			}
			if tc.signsOut {
				if s.Get(keyLicense) != "" || s.Get(keyServers) != "" {
					t.Fatalf("store %v, want the session and its cache gone", s.values)
				}
				return
			}
			if s.Get(keyLicense) != "lk" || cache == "" || s.Get(keyServers) != cache {
				t.Fatalf("store %v, want the session and its cache kept", s.values)
			}
		})
	}
}

func TestRestoreSessionSignsOutOnAnInvalidLicense(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	s.Set(keySubscription, encodeCachedSubscription(activeSub(), cacheOwner("lk", ident.pubB64), time.Now()))
	mu.Lock()
	servers = regionsWith(t, "mine")
	prepared = &preparedTunnel{serverID: "fra"}
	mu.Unlock()
	h.handle("/api/client/regions", replyCode(401, "INVALID_LICENSE"))

	_, err := RestoreSession()
	if err == nil || !strings.Contains(err.Error(), "hub auth error (401)") {
		t.Fatalf("RestoreSession = %v, want the auth error text the host maps", err)
	}
	for _, key := range []string{keyLicense, keySubscription, keyServers} {
		if s.Get(key) != "" {
			t.Errorf("%s survived a sign-out answer", key)
		}
	}
	lk, srvs, p, kept := sessionState()
	if lk != "" || srvs != nil || p != nil {
		t.Fatalf("memory kept lk=%q servers=%v prepared=%v", lk, srvs, p)
	}
	if kept.publicKey() != ident.pubB64 || s.Get(keyIdentity) == "" {
		t.Fatal("a sign-out answer dropped the identity; its device slot would be orphaned")
	}
	if _, err := RestoreSession(); err == nil || !strings.Contains(err.Error(), "no stored session") {
		t.Fatalf("second RestoreSession = %v, want no stored session without a hub call", err)
	}
	if got := len(h.requestsTo("/api/client/regions")); got != 1 {
		t.Fatalf("regions requests = %d, want 1: a dead key must not be retried", got)
	}
}

func TestNoActiveSubscriptionIsNotASignOut(t *testing.T) {
	s, _, h := newSignedInHub(t)
	h.handle("/api/client/regions", replyCode(403, "NO_ACTIVE_SUBSCRIPTION"))

	_, err := ListServers()
	if err == nil || !strings.Contains(err.Error(), "NO_SUBSCRIPTION") || strings.Contains(err.Error(), "hub auth error") {
		t.Fatalf("ListServers = %v, want the NO_SUBSCRIPTION error", err)
	}
	if s.Get(keyLicense) != "lk" {
		t.Fatal("an account without a plan was signed out")
	}
}

func TestListServersReturnsPublicServersAndFallsBack(t *testing.T) {
	_, _, h := newSignedInHub(t)
	h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "mine")))

	raw, err := ListServers()
	if err != nil {
		t.Fatalf("ListServers: %v", err)
	}
	var list []publicServer
	if err := json.Unmarshal([]byte(raw), &list); err != nil || len(list) != 1 || !list[0].Multihop {
		t.Fatalf("ListServers = %s (%v)", raw, err)
	}
	if strings.Contains(raw, "password") || strings.Contains(raw, "uid") {
		t.Fatalf("ListServers leaks credentials: %s", raw)
	}

	h.offline()
	if cached, err := ListServers(); err != nil || cached != raw {
		t.Fatalf("offline ListServers = %s, %v; want the cached list", cached, err)
	}
}

func TestRestoreSessionIgnoresAnotherSessionsCache(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	stale, _ := encodeServerCache(regionsWith(t, "theirs"), cacheOwner("someone-else", ident.pubB64))
	s.Set(keyServers, stale)
	h.offline()

	if raw, err := RestoreSession(); err == nil {
		t.Fatalf("served another session's cache: %s", raw)
	}
}

func TestPublicServersFlagOnlyWhatThisBuildRuns(t *testing.T) {
	initForTest(t, nil)
	got := publicServers([]serverInfo{serverWithEveryTransport(t)})[0]
	if !got.Cloak || !got.Multihop {
		t.Fatalf("cloak and multihop must pass through: %+v", got)
	}
	if got.Naive != naiveLinked {
		t.Fatalf("naive = %v on a build where naiveLinked = %v", got.Naive, naiveLinked)
	}
	if got.Snowflake {
		t.Fatal("snowflake is flagged, but no mobile build can run it")
	}
}

func TestLoginRefetchesServersForThisDevice(t *testing.T) {
	s, _ := initForTest(t, nil)
	h := newFakeHub(t)
	h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{
		"vpnAccessToken": "lk", "user": map[string]string{"email": "a@example.com", "name": "A"},
		"servers": regionsWith(t, "another-device"),
	}))
	h.handle("/api/device/register", replyWith(http.StatusOK, map[string]string{"deviceId": "d1", "assignedIp": "10.0.0.2"}))
	h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "this-device")))

	raw, err := Login("token")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	session, _ := decodeSession(t, raw)
	if session.Email != "a@example.com" || len(session.Servers) != 1 || strings.Contains(raw, "device") {
		t.Fatalf("session = %s", raw)
	}
	_, srvs, _, ident := sessionState()
	if len(srvs) != 1 || srvs[0].Cloak.UID != "this-device" {
		t.Fatalf("servers = %+v, want this device's list", srvs)
	}
	regions := h.requestsTo("/api/client/regions")
	if len(regions) != 1 || !strings.Contains(regions[0].Route, "identityPubkey=") {
		t.Fatalf("regions requests = %+v", regions)
	}
	if s.Get(keyLicense) != "lk" || restoreCachedServers(s.Get(keyServers), cacheOwner("lk", ident.pubB64)) == nil {
		t.Fatalf("store after Login = %v", s.values)
	}
}

func TestLoginKeepsTheLoginListWhenTheRefetchFails(t *testing.T) {
	s, _ := initForTest(t, nil)
	h := newFakeHub(t)
	h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{
		"vpnAccessToken": "lk", "user": map[string]string{}, "servers": regionsWith(t, "another-device"),
	}))
	h.handle("/api/device/register", replyWith(http.StatusOK, map[string]string{"deviceId": "d1"}))
	h.handle("/api/client/regions", replyCode(502, "BAD_GATEWAY"))

	if _, err := Login("token"); err != nil {
		t.Fatalf("Login failed on a refetch error: %v", err)
	}
	if _, srvs, _, _ := sessionState(); len(srvs) != 1 || srvs[0].Cloak.UID != "another-device" {
		t.Fatalf("servers = %+v, want the login list", srvs)
	}
	if cached := s.Get(keyServers); cached != "" {
		t.Fatalf("cached %s: the login list may carry another device's Cloak UID", cached)
	}
}

// Login re-asserts the identity it registered, so whatever dropped it while
// the login was in flight cannot leave a license with no identity beside it.
func TestLoginStoresTheIdentityItRegistered(t *testing.T) {
	ident := mustIdentity(t)
	s, _ := initForTest(t, map[string]string{keyIdentity: ident.marshal()})
	h := newFakeHub(t)
	answerALogin(t, h)
	h.handle("/api/device/register", func(fakeRequest) (int, any) {
		mu.Lock()
		identity = nil
		mu.Unlock()
		s.Set(keyIdentity, "")
		return http.StatusOK, map[string]string{"deviceId": "d1", "assignedIp": "10.0.0.2"}
	})

	if _, err := Login("token"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if stored, _ := parseIdentity(s.Get(keyIdentity)); stored.publicKey() != ident.pubB64 || s.Get(keyLicense) != "lk" {
		t.Fatalf("store = %v, want the license beside the identity Login registered", s.values)
	}
	if _, _, _, kept := sessionState(); kept.publicKey() != ident.pubB64 {
		t.Fatal("Login left a different identity in memory than the one it registered")
	}
}

func TestGetSubscriptionCachesAndServesWhenUnreachable(t *testing.T) {
	s, _, h := newSignedInHub(t)
	h.handle("/api/client/subscription", replyWith(http.StatusOK, map[string]any{
		"subscription": map[string]any{"status": "active", "entitled": false, "renews": true, "expiresAt": "2099-01-01T00:00:00.000Z"},
	}))

	live, err := GetSubscription()
	if err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if live != `{"status":"active","renews":true,"expiresAt":"2099-01-01T00:00:00.000Z","entitled":false,"cached":false}` {
		t.Fatalf("live = %s", live)
	}
	if s.Get(keySubscription) == "" {
		t.Fatal("the answer was not cached")
	}

	h.offline()
	cached, err := GetSubscription()
	if err != nil {
		t.Fatalf("offline GetSubscription: %v", err)
	}
	if cached != `{"status":"active","renews":true,"expiresAt":"2099-01-01T00:00:00.000Z","entitled":false,"cached":true}` {
		t.Fatalf("cached = %s", cached)
	}
}

func TestGetSubscriptionNeverServesTheCacheOverAHubAnswer(t *testing.T) {
	cases := []struct {
		name   string
		reply  func(fakeRequest) (int, any)
		clears bool
	}{
		{"invalid license", replyCode(401, "INVALID_LICENSE"), true},
		{"locked out", replyCode(429, "AUTH_LOCKED"), false},
		{"server error", replyCode(500, "INTERNAL_ERROR"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, h := newSignedInHub(t)
			h.handle("/api/client/subscription", replyWith(http.StatusOK, map[string]any{
				"subscription": map[string]any{"status": "active", "renews": true, "expiresAt": nil},
			}))
			if _, err := GetSubscription(); err != nil {
				t.Fatalf("GetSubscription: %v", err)
			}

			h.handle("/api/client/subscription", tc.reply)
			if raw, err := GetSubscription(); err == nil {
				t.Fatalf("a hub answer was papered over with the cache: %s", raw)
			}
			if cleared := s.Get(keySubscription) == ""; cleared != tc.clears {
				t.Fatalf("cache cleared = %v, want %v", cleared, tc.clears)
			}
		})
	}
}

func TestGetSubscriptionNullClearsTheCache(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	s.Set(keySubscription, encodeCachedSubscription(activeSub(), cacheOwner("lk", ident.pubB64), time.Now()))
	h.handle("/api/client/subscription", replyWith(http.StatusOK, map[string]any{"subscription": nil}))

	if raw, err := GetSubscription(); err != nil || raw != "null" {
		t.Fatalf("GetSubscription = %s, %v; want null", raw, err)
	}
	if s.Get(keySubscription) != "" {
		t.Fatal("a null subscription left the old answer cached")
	}
}

func TestLogoutClearsTheSubscriptionCache(t *testing.T) {
	s, ident, _ := newSignedInHub(t)
	owner := cacheOwner("lk", ident.pubB64)
	s.Set(keySubscription, encodeCachedSubscription(activeSub(), owner, time.Now()))
	list, _ := encodeServerCache(regionsWith(t, "mine"), owner)
	s.Set(keyServers, list)

	Logout()

	for _, key := range []string{keyLicense, keySubscription, keyServers} {
		if s.Get(key) != "" {
			t.Errorf("%s survived Logout", key)
		}
	}
	if s.Get(keyIdentity) == "" {
		t.Fatal("Logout dropped the identity; only Deregister may")
	}
}

func TestReloadDropsTheServersOfAnotherSession(t *testing.T) {
	s, _, _ := newSignedInHub(t)
	mu.Lock()
	servers = regionsWith(t, "old-device")
	mu.Unlock()

	s.Set(keyIdentity, mustIdentity(t).marshal())
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if _, srvs, _, _ := sessionState(); srvs != nil {
		t.Fatalf("servers = %+v, want them dropped for the new device", srvs)
	}
}

// keyedTunnel carries non-zero key material, so a scrub is visible.
func keyedTunnel(serverID string) *preparedTunnel {
	return &preparedTunnel{
		serverID: serverID, profile: &state.Profile{},
		wgPrivateKeyRaw: bytes.Repeat([]byte{1}, 32), serverPubKeyRaw: bytes.Repeat([]byte{2}, 32),
		presharedKeyRaw: bytes.Repeat([]byte{3}, 32),
	}
}

func scrubbed(p *preparedTunnel) bool {
	return bytes.Equal(p.wgPrivateKeyRaw, make([]byte, 32)) && bytes.Equal(p.presharedKeyRaw, make([]byte, 32))
}

func TestEndingASessionDropsThePreparedKey(t *testing.T) {
	for name, end := range map[string]func(*memoryStore){
		"Logout":         func(*memoryStore) { Logout() },
		"signOutLocally": func(*memoryStore) { signOutLocally(captureScope()) },
		"Wipe":           func(*memoryStore) { _ = Wipe() },
		"Reload":         func(s *memoryStore) { s.Set(keyLicense, "lk2"); _ = Reload() },
	} {
		t.Run(name, func(t *testing.T) {
			s, _, _ := newSignedInHub(t)
			dropped := keyedTunnel("fra")
			stagePrepared(dropped)

			end(s)
			if _, _, p, _ := sessionState(); p != nil {
				t.Fatalf("prepared = %+v, want it and its pre-shared key gone", p)
			}
			if !scrubbed(dropped) {
				t.Fatal("the dropped config's private and pre-shared keys were left in memory")
			}
		})
	}
}
