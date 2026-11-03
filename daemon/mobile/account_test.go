//go:build android || ios

package mobile

import (
	"errors"
	"io/fs"
	"net/http"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestAccountNumberIsEmptyWhenSignedOut(t *testing.T) {
	initForTest(t, map[string]string{keyIdentity: mustIdentity(t).marshal()})
	if got := AccountNumber(); got != "" {
		t.Fatalf("AccountNumber() = %q, want empty", got)
	}
}

func TestAccountNumberReturnsTheStoredKey(t *testing.T) {
	const key = "K7M2Q9X4C8V1B6N3R5T0W2Y7"
	initForTest(t, map[string]string{keyLicense: key, keyIdentity: mustIdentity(t).marshal()})
	if got := AccountNumber(); got != key {
		t.Fatalf("AccountNumber() = %q, want %q", got, key)
	}
}

func TestAccountNumberHidesAHalfFinishedLogin(t *testing.T) {
	s, _ := initForTest(t, map[string]string{keyIdentity: mustIdentity(t).marshal()})
	mu.Lock()
	licenseKey = "lk"
	mu.Unlock()
	s.Set(keyLicense, "")
	if got := AccountNumber(); got != "" {
		t.Fatalf("AccountNumber() = %q mid-login, want empty", got)
	}

	initForTest(t, map[string]string{keyLicense: "lk"})
	if got := AccountNumber(); got != "" {
		t.Fatalf("AccountNumber() = %q without an identity, want empty", got)
	}
}

func TestDeregisterRequiresSignIn(t *testing.T) {
	initForTest(t, map[string]string{keyIdentity: mustIdentity(t).marshal()})
	if err := Deregister(); err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("Deregister = %v, want not authenticated", err)
	}
	initForTest(t, map[string]string{keyLicense: "lk"})
	if err := Deregister(); err == nil || !strings.Contains(err.Error(), "no device identity") {
		t.Fatalf("Deregister = %v, want no device identity", err)
	}
	if currentHubRoute() != nil {
		t.Fatal("a refused Deregister reached for the hub")
	}
}

func TestDeregisterSendsTheLicenseInTheBody(t *testing.T) {
	_, ident, h := newSignedInHub(t)
	h.handle("/api/device/deregister", replyWith(http.StatusOK, map[string]bool{"ok": true}))

	if err := Deregister(); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	reqs := h.requestsTo("/api/device/deregister")
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost {
		t.Fatalf("deregister requests = %+v", reqs)
	}
	if got := string(reqs[0].Body); got != `{"identityPubkey":"`+ident.pubB64+`","licenseKey":"lk"}` {
		t.Fatalf("body = %s", got)
	}
}

func TestDeregisterForgetsTheIdentityOnSuccess(t *testing.T) {
	cases := []struct {
		name  string
		reply func(fakeRequest) (int, any)
	}{
		{"ok", replyWith(http.StatusOK, map[string]bool{"ok": true})},
		{"already gone", replyCode(404, "DEVICE_NOT_FOUND")},
		{"another account's", replyCode(403, "FORBIDDEN")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, h := newSignedInHub(t)
			h.handle("/api/device/deregister", tc.reply)
			if err := Deregister(); err != nil {
				t.Fatalf("Deregister = %v, want done", err)
			}
			if s.Get(keyIdentity) != "" {
				t.Fatal("the stored identity survived")
			}
			if _, _, _, ident := sessionState(); ident != nil {
				t.Fatal("the identity survived in memory")
			}
			if s.Get(keyLicense) != "lk" {
				t.Fatal("Deregister signed out; that is Logout's job")
			}
		})
	}
}

func TestDeregisterKeepsTheIdentityWhenItFails(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *fakeHub)
	}{
		{"unreachable", func(h *fakeHub) { h.offline() }},
		{"server error", func(h *fakeHub) { h.handle("/api/device/deregister", replyCode(500, "INTERNAL_ERROR")) }},
		{"unparsable error", func(h *fakeHub) { h.handle("/api/device/deregister", replyWith(502, "bad gateway")) }},
		{"invalid license", func(h *fakeHub) { h.handle("/api/device/deregister", replyCode(401, "INVALID_LICENSE")) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ident, h := newSignedInHub(t)
			tc.setup(h)
			if err := Deregister(); err == nil {
				t.Fatal("Deregister reported success")
			}
			if stored, _ := parseIdentity(s.Get(keyIdentity)); stored.publicKey() != ident.pubB64 {
				t.Fatal("a failed deregistration dropped the stored identity")
			}
			if _, _, _, kept := sessionState(); kept.publicKey() != ident.pubB64 {
				t.Fatal("a failed deregistration dropped the identity in memory")
			}
		})
	}
}

func TestDeregisterKeepsAnIdentityALoginReplaced(t *testing.T) {
	s, _, h := newSignedInHub(t)
	replacement := mustIdentity(t)
	h.handle("/api/device/deregister", func(fakeRequest) (int, any) {
		mu.Lock()
		identity = replacement
		mu.Unlock()
		s.Set(keyIdentity, replacement.marshal())
		return http.StatusOK, map[string]bool{"ok": true}
	})

	if err := Deregister(); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if _, _, _, ident := sessionState(); ident != replacement {
		t.Fatal("Deregister dropped the identity a Login installed meanwhile")
	}
	if stored, _ := parseIdentity(s.Get(keyIdentity)); stored.publicKey() != replacement.pubB64 {
		t.Fatal("Deregister cleared the replacement identity from the store")
	}
}

// The host gives Deregister 8s, then signs out and may sign straight back in,
// reusing the identity; the deregistration's late answer must not take it.
func TestALateDeregisterAnswerLeavesTheIdentityALoginReused(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{
		"vpnAccessToken": "lk2", "user": map[string]string{"email": "b@example.com"}, "servers": []serverInfo{},
	}))
	h.handle("/api/device/register", replyWith(http.StatusOK, map[string]string{"deviceId": "d2", "assignedIp": "10.0.0.3"}))
	h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "mine")))
	loginErr := make(chan error, 1)
	h.handle("/api/device/deregister", func(fakeRequest) (int, any) {
		Logout()
		_, err := Login("token")
		loginErr <- err
		return http.StatusOK, map[string]bool{"ok": true}
	})

	if err := Deregister(); !errors.Is(err, errSessionEnded) {
		t.Fatalf("Deregister = %v, want the session-ended error", err)
	}
	if err := <-loginErr; err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := bodyFields(t, h.requestsTo("/api/device/register")[0])["identityPubkey"]; got != ident.pubB64 {
		t.Fatalf("Login registered %q, want the reused identity", got)
	}
	if stored, _ := parseIdentity(s.Get(keyIdentity)); stored.publicKey() != ident.pubB64 || s.Get(keyLicense) != "lk2" {
		t.Fatalf("store = %v, want the new license beside the identity it registered", s.values)
	}
	if lk, _, _, kept := sessionState(); lk != "lk2" || kept.publicKey() != ident.pubB64 {
		t.Fatalf("memory lk=%q identity kept=%v", lk, kept.publicKey() == ident.pubB64)
	}
}

func TestForgetIdentityIfIgnoresALaterSession(t *testing.T) {
	s, ident, _ := newSignedInHub(t)
	stale := captureScope()
	Logout()

	forgetIdentityIf(stale.session, ident.pubB64)
	if _, _, _, kept := sessionState(); kept.publicKey() != ident.pubB64 {
		t.Fatal("a forget from an ended session dropped the identity in memory")
	}
	if stored, _ := parseIdentity(s.Get(keyIdentity)); stored.publicKey() != ident.pubB64 {
		t.Fatal("a forget from an ended session dropped the stored identity")
	}
}

func TestSignOutLocallyLeavesALaterSessionAlone(t *testing.T) {
	s, _, _ := newSignedInHub(t)
	stale := captureScope()
	Logout()
	s.Set(keyLicense, "lk2")
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	signOutLocally(stale)
	if lk, _, _, _ := sessionState(); lk != "lk2" || s.Get(keyLicense) != "lk2" {
		t.Fatalf("memory lk=%q store %q, want the later session kept", lk, s.Get(keyLicense))
	}
}

func TestSignOutLocallyKeepsAnotherProcessesLicense(t *testing.T) {
	s, _, _ := newSignedInHub(t)
	s.Set(keyLicense, "lk2")
	s.Set(keyServers, "theirs")

	signOutLocally(captureScope())
	if lk, _, _, _ := sessionState(); lk != "" {
		t.Fatalf("memory lk=%q, want this process's refused session gone", lk)
	}
	if s.Get(keyLicense) != "lk2" || s.Get(keyServers) != "theirs" {
		t.Fatalf("store = %v, want the session another process stored kept", s.values)
	}
}

func TestStoreForSessionDropsAWriteFromAnEndedSession(t *testing.T) {
	s, _, _ := newSignedInHub(t)
	stale := captureScope()
	Logout()
	storeForSession(stale, keySubscription, "x")
	if s.Get(keySubscription) != "" {
		t.Fatal("an ended session's answer was cached")
	}

	// Signing back in with the same license is still another session.
	s.Set(keyLicense, "lk")
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}
	storeForSession(stale, keySubscription, "x")
	if s.Get(keySubscription) != "" {
		t.Fatal("an ended session's answer was cached into the next one")
	}
	storeForSession(captureScope(), keySubscription, "y")
	if s.Get(keySubscription) != "y" {
		t.Fatal("the current session could not cache its answer")
	}
}

func TestLoginRotatesTheIdentityOnConflict(t *testing.T) {
	old := mustIdentity(t)
	s, _ := initForTest(t, map[string]string{keyIdentity: old.marshal()})
	h := newFakeHub(t)
	h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{
		"vpnAccessToken": "lk", "user": map[string]string{"email": "b@example.com"}, "servers": []serverInfo{},
	}))
	var calls atomic.Int32
	h.handle("/api/device/register", func(fakeRequest) (int, any) {
		if calls.Add(1) == 1 {
			return 409, map[string]string{"error": "Device identity already registered to another account", "code": "IDENTITY_CONFLICT"}
		}
		return http.StatusOK, map[string]string{"deviceId": "d9", "assignedIp": "10.0.0.9"}
	})

	if _, err := Login("token"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	reqs := h.requestsTo("/api/device/register")
	if len(reqs) != 2 {
		t.Fatalf("register requests = %d, want a single retry", len(reqs))
	}
	first, second := bodyFields(t, reqs[0])["identityPubkey"], bodyFields(t, reqs[1])["identityPubkey"]
	if first != old.pubB64 || second == old.pubB64 || second == "" {
		t.Fatalf("register pubkeys = %q then %q, want the old one then a fresh one", first, second)
	}
	if stored, _ := parseIdentity(s.Get(keyIdentity)); stored.publicKey() != second {
		t.Fatal("the fresh identity was not persisted")
	}
	if _, _, _, ident := sessionState(); ident.publicKey() != second {
		t.Fatal("the fresh identity is not the one in use")
	}
	if s.Get(keyLicense) != "lk" {
		t.Fatal("the recovered login was not stored")
	}
}

func TestLoginRetriesAConflictOnlyOnce(t *testing.T) {
	s, _ := initForTest(t, map[string]string{keyIdentity: mustIdentity(t).marshal()})
	h := newFakeHub(t)
	h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{"vpnAccessToken": "lk", "user": map[string]string{}}))
	h.handle("/api/device/register", replyCode(409, "IDENTITY_CONFLICT"))

	if _, err := Login("token"); hubErrorCode(err) != "IDENTITY_CONFLICT" {
		t.Fatalf("Login = %v, want the conflict surfaced", err)
	}
	if got := len(h.requestsTo("/api/device/register")); got != 2 {
		t.Fatalf("register requests = %d, want 2", got)
	}
	if s.Get(keyLicense) != "" {
		t.Fatal("a failed login stored the license")
	}
}

// answerALogin answers every request a Login makes, with a server that
// advertises every hub route.
func answerALogin(t *testing.T, h *fakeHub) {
	t.Helper()
	server := serverWithEveryTransport(t)
	server.ControlPlaneReality = &controlPlaneRealityInfo{RemoteHost: "203.0.113.20", RemotePort: 443,
		UUID: "123e4567-e89b-12d3-a456-426614174000", PublicKey: strings.Repeat("k", 43), ShortID: "abcd",
		ServerName: "www.example.com"}
	h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{
		"vpnAccessToken": "lk", "user": map[string]string{"email": "a@example.com"},
		"servers": []serverInfo{server}, "frontedEndpoints": []string{"relay.example"},
	}))
	h.handle("/api/device/register", replyWith(http.StatusOK, map[string]string{"deviceId": "d1", "assignedIp": "10.0.0.2"}))
	h.handle("/api/client/regions", replyWith(http.StatusOK, []serverInfo{server}))
	h.handle("/api/client/subscription", replyWith(http.StatusOK, map[string]any{
		"subscription": map[string]any{"status": "active", "renews": true, "expiresAt": nil},
	}))
}

// fillEveryStoredKey runs every writer the package has, so a key one of them
// writes but storedKeys lacks shows up after Wipe.
func fillEveryStoredKey(t *testing.T, s *memoryStore, h *fakeHub) {
	t.Helper()
	answerALogin(t, h)
	if _, err := Login("token"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := GetSubscription(); err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
	if _, err := SetConfig(`{"mtu":1300,"preferredTransport":"reality"}`); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	wipe := currentWipeEpoch()
	rememberHubIP(wipe, "203.0.113.7")
	applyDeadDrop(wipe, deadDropPayload{Seq: 7, HubIPs: []string{"203.0.113.8"}, FrontedEndpoints: []string{"drop.example"}})
	previous := deadDropURLs
	deadDropURLs = nil
	t.Cleanup(func() { deadDropURLs = previous })
	reseedFromDeadDrop(wipe, nil)

	for _, key := range storedKeys {
		if s.Get(key) == "" {
			t.Fatalf("no writer filled %s", key)
		}
	}
}

func TestWipeLeavesNothingInTheStore(t *testing.T) {
	s, _ := initForTest(t, nil)
	h := newFakeHub(t)
	fillEveryStoredKey(t, s, h)
	before := len(h.requests())

	if err := Wipe(); err != nil {
		t.Fatalf("Wipe: %v", err)
	}

	s.mu.Lock()
	for key, value := range s.values {
		if value != "" {
			t.Errorf("%s survived Wipe: %q", key, value)
		}
	}
	s.mu.Unlock()
	if len(h.requests()) != before {
		t.Fatal("Wipe called the hub")
	}
	lk, srvs, p, ident := sessionState()
	if lk != "" || srvs != nil || p != nil || ident != nil {
		t.Fatalf("memory kept lk=%q servers=%v prepared=%v identity=%v", lk, srvs, p, ident)
	}
	if PreferredTransport() != "auto" {
		t.Fatalf("PreferredTransport() = %q, want the default", PreferredTransport())
	}
	mu.Lock()
	reset := reflect.DeepEqual(settings, defaultConfig())
	mu.Unlock()
	if !reset {
		t.Fatal("Wipe kept the settings")
	}
}

func assertWiped(t *testing.T, s *memoryStore) {
	t.Helper()
	s.mu.Lock()
	for key, value := range s.values {
		if value != "" {
			t.Errorf("%s was written after Wipe: %q", key, value)
		}
	}
	s.mu.Unlock()
	if lk, srvs, p, ident := sessionState(); lk != "" || srvs != nil || p != nil || ident != nil {
		t.Fatalf("memory after Wipe: lk=%q servers=%v prepared=%v identity=%v", lk, srvs, p, ident)
	}
}

// A hub answer that lands after a Wipe must write nothing back, in memory or
// in the store, and no call may report a session from before it.
func TestWipeDropsAnAnswerStillInFlight(t *testing.T) {
	wipeThen := func(t *testing.T, h *fakeHub, route string) {
		h.mu.Lock()
		answer := h.routes[route]
		h.mu.Unlock()
		h.handle(route, func(req fakeRequest) (int, any) {
			if err := Wipe(); err != nil {
				t.Errorf("Wipe: %v", err)
			}
			return answer(req)
		})
	}
	login := func() (string, error) { return Login("token") }
	cases := []struct {
		name     string
		route    string
		call     func() (string, error)
		signedIn bool
	}{
		{"Login at token login", "/api/client/token-login", login, false},
		{"Login at device register", "/api/device/register", login, false},
		{"Login at the refetch", "/api/client/regions", login, false},
		{"RestoreSession", "/api/client/regions", RestoreSession, true},
		{"ListServers", "/api/client/regions", ListServers, true},
		{"GetSubscription", "/api/client/subscription", GetSubscription, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var s *memoryStore
			var h *fakeHub
			if tc.signedIn {
				s, _, h = newSignedInHub(t)
			} else {
				s, _ = initForTest(t, nil)
				h = newFakeHub(t)
			}
			answerALogin(t, h)
			wipeThen(t, h, tc.route)

			raw, err := tc.call()
			if !errors.Is(err, errSessionEnded) {
				t.Fatalf("got %s, %v, want the session-ended error", raw, err)
			}
			assertWiped(t, s)
		})
	}
}

func TestAnAnswerThatLandsAfterAWipeIsNotRead(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(diagnosticsRoute, func(fakeRequest) (int, any) {
		if err := Wipe(); err != nil {
			t.Errorf("Wipe: %v", err)
		}
		return http.StatusCreated, map[string]string{"reportCode": "K7M2-9QXD"}
	})
	if body, _, err := hubFetch(diagnosticsRoute, http.MethodPost, nil, nil); !errors.Is(err, errSessionEnded) {
		t.Fatalf("hubFetch = %s, %v, want the session-ended error", body, err)
	}
}

func TestARefusalOfAnEndedSessionIsNotReadAsTheNextOnes(t *testing.T) {
	s, _, h := newSignedInHub(t)
	h.handle("/api/client/regions", func(fakeRequest) (int, any) {
		Logout()
		s.Set(keyLicense, "lk2")
		if err := Reload(); err != nil {
			t.Errorf("Reload: %v", err)
		}
		return http.StatusUnauthorized, map[string]string{"error": "Invalid license key", "code": "INVALID_LICENSE"}
	})

	if _, err := RestoreSession(); !errors.Is(err, errSessionEnded) {
		t.Fatalf("RestoreSession = %v, want the session-ended error, not the old session's refusal", err)
	}
	if lk, _, _, _ := sessionState(); lk != "lk2" || s.Get(keyLicense) != "lk2" {
		t.Fatalf("memory lk=%q store %q, want the next session untouched", lk, s.Get(keyLicense))
	}
}

func TestARouteWriteFromBeforeAWipeIsDropped(t *testing.T) {
	s, _ := initForTest(t, nil)
	stale := currentWipeEpoch()
	if err := Wipe(); err != nil {
		t.Fatalf("Wipe: %v", err)
	}

	rememberHubIP(stale, "203.0.113.7")
	saveFrontedEndpoints(stale, []string{"relay.example"})
	rememberHubDiscovery(stale, discoverHubRoutes([]serverInfo{serverWithEveryTransport(t)}))
	applyDeadDrop(stale, deadDropPayload{Seq: 7, HubIPs: []string{"203.0.113.8"}})
	assertWiped(t, s)

	rememberHubIP(currentWipeEpoch(), "203.0.113.7")
	if s.Get(keyHubIP) != "203.0.113.7" {
		t.Fatal("a write after the Wipe was dropped too")
	}
}

func TestASearchThatSpansAWipeIsNotRunAgain(t *testing.T) {
	s, _ := initForTest(t, nil)
	searches := 0
	previous := hubSearch
	hubSearch = func(wipe uint64) (*hubPath, error) {
		searches++
		if err := Wipe(); err != nil {
			t.Errorf("Wipe: %v", err)
		}
		rememberHubIP(wipe, "203.0.113.9")
		return &hubPath{kind: "normal", client: &http.Client{}}, nil
	}
	t.Cleanup(func() { hubSearch = previous })

	if err := ensureHubPath(currentWipeEpoch()); !errors.Is(err, errSessionEnded) {
		t.Fatalf("ensureHubPath = %v, want the session-ended error", err)
	}
	if searches != 1 || currentHubRoute() != nil {
		t.Fatalf("searches = %d, route %v: a wipe must end the search, not restart it", searches, currentHubRoute())
	}
	assertWiped(t, s)
}

func TestWipeRemovesTransportHistoryFromDisk(t *testing.T) {
	_, dir := initForTest(t, nil)
	rememberTransport("wifi:home", "cloak")
	if _, err := os.Stat(transportMemoryPath(dir)); err != nil {
		t.Fatalf("history was never written: %v", err)
	}

	if err := Wipe(); err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if _, err := os.Stat(transportMemoryPath(dir)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("history file after Wipe: %v", err)
	}
	mu.Lock()
	memory := transportMem
	mu.Unlock()
	if _, ok := memory.Lookup("wifi:home"); ok {
		t.Fatal("history survived Wipe in memory")
	}
}

func TestWipeDropsTheHubRouteAndCooldown(t *testing.T) {
	initForTest(t, nil)
	installHubRoute()
	hubMu.Lock()
	hubTrack.searchFailed(time.Now())
	generation := hubTrack.generation
	hubMu.Unlock()

	if err := Wipe(); err != nil {
		t.Fatalf("Wipe: %v", err)
	}

	hubMu.Lock()
	wait, gotGeneration := hubTrack.retryIn(time.Now()), hubTrack.generation
	hubMu.Unlock()
	if currentHubRoute() != nil || wait != 0 || gotGeneration == generation {
		t.Fatalf("after Wipe: route=%v cooldown=%v generation moved=%v", currentHubRoute(), wait, gotGeneration != generation)
	}
}

func TestWipeClearsLogsInPlace(t *testing.T) {
	initForTest(t, nil)
	mu.Lock()
	logs := wgLogs
	mu.Unlock()
	logs.Add(state.LogInfo, state.SourceDaemon, "connected to fra")

	if err := Wipe(); err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	mu.Lock()
	after := wgLogs
	mu.Unlock()
	if after != logs {
		t.Fatal("Wipe swapped the log store; the WireGuard logger reads the old pointer unlocked")
	}
	if got := logs.Since(0); len(got) != 0 {
		t.Fatalf("logs after Wipe = %+v", got)
	}
}

func TestWipeStopsTheTunnel(t *testing.T) {
	initForTest(t, nil)
	mu.Lock()
	activeTunnel = &tunnelRuntime{dev: newTestDevice(), serverID: "fra"}
	mu.Unlock()

	if err := Wipe(); err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	mu.Lock()
	active, status := activeTunnel, currentStatus.State
	mu.Unlock()
	if active != nil || status != string(state.StateDisconnected) {
		t.Fatalf("after Wipe: active=%v state=%s", active, status)
	}
}

func TestSignOutLocallyKeepsTheIdentity(t *testing.T) {
	s, ident, _ := newSignedInHub(t)
	s.Set(keySubscription, "x")
	s.Set(keyServers, "y")
	s.Set(keyHubIP, "203.0.113.7")

	signOutLocally(captureScope())

	for _, key := range []string{keyLicense, keySubscription, keyServers} {
		if s.Get(key) != "" {
			t.Errorf("%s survived signOutLocally", key)
		}
	}
	if s.Get(keyIdentity) == "" || s.Get(keyHubIP) == "" {
		t.Fatal("signOutLocally dropped the identity or a hub route")
	}
	if lk, _, _, kept := sessionState(); lk != "" || kept.publicKey() != ident.pubB64 {
		t.Fatalf("memory after signOutLocally: lk=%q identity kept=%v", lk, kept.publicKey() == ident.pubB64)
	}
}
