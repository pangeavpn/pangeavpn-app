//go:build android || ios

package mobile

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func answerDeleted() func(fakeRequest) (int, any) {
	return replyWith(http.StatusOK, map[string]bool{"deleted": true})
}

func TestDeleteAccountRequiresSignIn(t *testing.T) {
	initForTest(t, map[string]string{keyIdentity: mustIdentity(t).marshal()})
	if err := DeleteAccount(); err == nil || !strings.Contains(err.Error(), "not authenticated") {
		t.Fatalf("DeleteAccount = %v, want not authenticated", err)
	}
	if currentHubRoute() != nil {
		t.Fatal("a refused DeleteAccount reached for the hub")
	}
}

func TestDeleteAccountAuthenticatesWithTheStoredLicense(t *testing.T) {
	_, _, h := newSignedInHub(t)
	h.handle(accountDeleteRoute, answerDeleted())

	if err := DeleteAccount(); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	reqs := h.requestsTo(accountDeleteRoute)
	if len(reqs) != 1 || reqs[0].Method != http.MethodPost || reqs[0].Headers["X-License-Key"] != "lk" {
		t.Fatalf("delete requests = %+v, want one POST carrying the license", reqs)
	}
	if string(reqs[0].Body) != "{}" {
		t.Fatalf("body = %s, want an empty object", reqs[0].Body)
	}
	if got := len(h.requests()); got != 1 {
		t.Fatalf("hub requests = %d, want the delete alone", got)
	}
}

func TestDeleteAccountEndsTheSessionAndForgetsTheIdentity(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	owner := cacheOwner("lk", ident.pubB64)
	s.Set(keySubscription, encodeCachedSubscription(activeSub(), owner, time.Now()))
	list, _ := encodeServerCache(regionsWith(t, "mine"), owner)
	s.Set(keyServers, list)
	s.Set(keyHubIP, "203.0.113.7")
	rememberTransport("wifi:home", "cloak")
	dropped := keyedTunnel("fra")
	stagePrepared(dropped)
	h.handle(accountDeleteRoute, answerDeleted())

	if err := DeleteAccount(); err != nil {
		t.Fatalf("DeleteAccount: %v", err)
	}
	for _, key := range []string{keyLicense, keySubscription, keyServers, keyIdentity} {
		if s.Get(key) != "" {
			t.Errorf("%s survived the deletion", key)
		}
	}
	if s.Get(keyHubIP) == "" {
		t.Fatal("DeleteAccount dropped a hub route; only Wipe forgets those")
	}
	if lk, srvs, p, kept := sessionState(); lk != "" || srvs != nil || p != nil || kept != nil {
		t.Fatalf("memory kept lk=%q servers=%v prepared=%v identity=%v", lk, srvs, p, kept)
	}
	if !scrubbed(dropped) {
		t.Fatal("the dropped config's keys were left in memory")
	}
	mu.Lock()
	memory := transportMem
	mu.Unlock()
	if _, ok := memory.Lookup("wifi:home"); ok {
		t.Fatal("the transport history survived the deletion")
	}
	if AccountNumber() != "" {
		t.Fatal("AccountNumber still names the deleted account")
	}
	if _, err := RestoreSession(); err == nil || !strings.Contains(err.Error(), "no stored session") {
		t.Fatalf("RestoreSession = %v, want no stored session", err)
	}
}

func TestDeleteAccountKeepsTheSessionWhenItFails(t *testing.T) {
	cases := []struct {
		name string
		// reply is nil for a hub that cannot be reached.
		reply func(fakeRequest) (int, any)
		want  string
	}{
		{"unreachable", nil, "hub unreachable"},
		{"server error", replyCode(502, "UPSTREAM_FAILED"), "hub error (502)"},
		{"locked out", replyCode(429, "AUTH_LOCKED"), "hub error (429)"},
		{"not confirmed", replyWith(http.StatusOK, map[string]bool{"deleted": false}), "did not confirm"},
		{"an empty answer", replyWith(http.StatusOK, map[string]any{}), "did not confirm"},
		{"an answer that does not decode", replyWith(http.StatusOK, "deleted"), "decode hub response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, ident, h := newSignedInHub(t)
			if tc.reply == nil {
				h.offline()
			} else {
				h.handle(accountDeleteRoute, tc.reply)
			}
			if err := DeleteAccount(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("DeleteAccount = %v, want %q", err, tc.want)
			}
			if s.Get(keyLicense) != "lk" || s.Get(keyIdentity) == "" {
				t.Fatalf("store = %v, want the session kept", s.values)
			}
			if lk, _, _, kept := sessionState(); lk != "lk" || kept.publicKey() != ident.pubB64 {
				t.Fatalf("memory lk=%q, want the session and identity kept", lk)
			}
		})
	}
}

// The route is behind licenseKeyAuth, so its 401 means the stored session is dead.
func TestDeleteAccountSignsOutOnAnInvalidLicense(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	s.Set(keyServers, "cached")
	h.handle(accountDeleteRoute, replyCode(401, "INVALID_LICENSE"))

	if err := DeleteAccount(); err == nil || !strings.Contains(err.Error(), "hub auth error (401)") {
		t.Fatalf("DeleteAccount = %v, want the auth error the host maps", err)
	}
	if s.Get(keyLicense) != "" || s.Get(keyServers) != "" {
		t.Fatalf("store = %v, want the dead session gone", s.values)
	}
	if lk, _, _, kept := sessionState(); lk != "" || kept.publicKey() != ident.pubB64 {
		t.Fatalf("memory lk=%q, want signed out with the identity kept", lk)
	}
	if s.Get(keyIdentity) == "" {
		t.Fatal("an unconfirmed deletion dropped the identity")
	}
}

// A confirmation for a session that ended meanwhile must not sign out the next one.
func TestDeleteAccountLeavesASessionThatStartedMeanwhile(t *testing.T) {
	s, _, h := newSignedInHub(t)
	next := mustIdentity(t)
	h.handle(accountDeleteRoute, func(req fakeRequest) (int, any) {
		Logout()
		s.Set(keyLicense, "lk2")
		s.Set(keyIdentity, next.marshal())
		if err := Reload(); err != nil {
			t.Errorf("Reload: %v", err)
		}
		return answerDeleted()(req)
	})

	if err := DeleteAccount(); !errors.Is(err, errSessionEnded) {
		t.Fatalf("DeleteAccount = %v, want the session-ended error", err)
	}
	if lk, _, _, kept := sessionState(); lk != "lk2" || kept.publicKey() != next.pubB64 {
		t.Fatalf("memory lk=%q, want the next session untouched", lk)
	}
	if s.Get(keyLicense) != "lk2" || s.Get(keyIdentity) != next.marshal() {
		t.Fatalf("store = %v, want the next session untouched", s.values)
	}
}

func TestDeleteAccountDropsAnAnswerThatLandsAfterAWipe(t *testing.T) {
	s, _, h := newSignedInHub(t)
	h.handle(accountDeleteRoute, func(req fakeRequest) (int, any) {
		if err := Wipe(); err != nil {
			t.Errorf("Wipe: %v", err)
		}
		return answerDeleted()(req)
	})

	if err := DeleteAccount(); !errors.Is(err, errSessionEnded) {
		t.Fatalf("DeleteAccount = %v, want the session-ended error", err)
	}
	assertWiped(t, s)
}

func TestEndDeletedAccountIgnoresALaterSession(t *testing.T) {
	s, _, _ := newSignedInHub(t)
	stale := captureScope()
	Logout()
	s.Set(keyLicense, "lk2")
	if err := Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	endDeletedAccount(stale)
	if lk, _, _, kept := sessionState(); lk != "lk2" || kept == nil {
		t.Fatalf("memory lk=%q identity kept=%v, want the later session untouched", lk, kept != nil)
	}
	if s.Get(keyLicense) != "lk2" || s.Get(keyIdentity) == "" {
		t.Fatalf("store = %v, want the later session untouched", s.values)
	}
}

func TestEndDeletedAccountKeepsAnotherProcessesSession(t *testing.T) {
	s, _, _ := newSignedInHub(t)
	theirs := mustIdentity(t).marshal()
	s.Set(keyLicense, "lk2")
	s.Set(keyIdentity, theirs)
	s.Set(keyServers, "theirs")

	endDeletedAccount(captureScope())
	if lk, _, _, kept := sessionState(); lk != "" || kept != nil {
		t.Fatalf("memory lk=%q identity kept=%v, want the deleted session gone", lk, kept != nil)
	}
	if s.Get(keyLicense) != "lk2" || s.Get(keyIdentity) != theirs || s.Get(keyServers) != "theirs" {
		t.Fatalf("store = %v, want the session another process stored kept", s.values)
	}
}
