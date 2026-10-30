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

// fillEveryStoredKey writes every key the package knows plus what the normal
// paths write, so a key missing from storedKeys shows up after Wipe.
func fillEveryStoredKey(t *testing.T, h *fakeHub) {
	t.Helper()
	for _, key := range storedKeys {
		setStoredValue(key, "x")
	}
	ident := mustIdentity(t)
	setStoredValue(keyIdentity, ident.marshal())
	setStoredValue(keyLicense, "lk")
	mu.Lock()
	licenseKey, identity = "lk", ident
	mu.Unlock()
	setStoredValue(keyHubIP, "203.0.113.7")
	rememberHubDiscovery(discoverHubRoutes([]serverInfo{serverWithEveryTransport(t)}))
	if _, err := SetConfig(`{"mtu":1300,"preferredTransport":"reality"}`); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "mine")))
	h.handle("/api/client/subscription", replyWith(http.StatusOK, map[string]any{
		"subscription": map[string]any{"status": "active", "renews": true, "expiresAt": nil},
	}))
	if _, err := RestoreSession(); err != nil {
		t.Fatalf("RestoreSession: %v", err)
	}
	if _, err := GetSubscription(); err != nil {
		t.Fatalf("GetSubscription: %v", err)
	}
}

func TestWipeLeavesNothingInTheStore(t *testing.T) {
	s, _ := initForTest(t, nil)
	h := newFakeHub(t)
	fillEveryStoredKey(t, h)
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

	signOutLocally()

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
