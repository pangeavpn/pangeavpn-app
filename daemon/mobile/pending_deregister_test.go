//go:build android || ios

package mobile

import (
	"encoding/json"
	"net/http"
	"testing"
)

func pendingIn(t *testing.T, s *memoryStore) *pendingDeregister {
	t.Helper()
	raw := s.Get(keyPendingDeregister)
	if raw == "" {
		return nil
	}
	var p pendingDeregister
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("pending record %q: %v", raw, err)
	}
	return &p
}

func TestDeregisterRemembersASlotItCouldNotFree(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	h.offline()

	if err := Deregister(); err == nil {
		t.Fatal("Deregister reported success")
	}
	Logout()

	p := pendingIn(t, s)
	if p == nil || p.LicenseKey != "lk" || p.IdentityPubkey != ident.pubB64 {
		t.Fatalf("pending = %+v, want the signed-out license and identity", p)
	}
}

func TestDeregisterSettlesThePendingSlot(t *testing.T) {
	cases := []struct {
		name  string
		reply func(fakeRequest) (int, any)
	}{
		{"freed", replyWith(http.StatusOK, map[string]bool{"ok": true})},
		{"already gone", replyCode(404, "DEVICE_NOT_FOUND")},
		{"key refused for good", replyCode(401, "INVALID_LICENSE")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, _, h := newSignedInHub(t)
			h.handle("/api/device/deregister", tc.reply)
			_ = Deregister()
			if p := pendingIn(t, s); p != nil {
				t.Fatalf("pending = %+v, want nothing left to retry", p)
			}
		})
	}
}

func TestRetryPendingDeregisterFreesTheSlotAfterSignOut(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	h.offline()
	_ = Deregister()
	Logout()

	h.online()
	h.handle("/api/device/deregister", replyWith(http.StatusOK, map[string]bool{"ok": true}))
	if err := RetryPendingDeregister(); err != nil {
		t.Fatalf("RetryPendingDeregister: %v", err)
	}

	reqs := h.requestsTo("/api/device/deregister")
	if len(reqs) != 1 {
		t.Fatalf("deregister requests = %d, want 1", len(reqs))
	}
	if got := string(reqs[0].Body); got != `{"identityPubkey":"`+ident.pubB64+`","licenseKey":"lk"}` {
		t.Fatalf("body = %s", got)
	}
	if pendingIn(t, s) != nil {
		t.Fatal("the freed slot is still pending")
	}
	if s.Get(keyIdentity) != "" {
		t.Fatal("the deregistered identity survived in the store")
	}
}

func TestRetryPendingDeregisterKeepsTheSlotWhileTheHubFails(t *testing.T) {
	s, _, h := newSignedInHub(t)
	h.offline()
	_ = Deregister()
	Logout()

	h.online()
	h.handle("/api/device/deregister", replyCode(500, "INTERNAL_ERROR"))
	if err := RetryPendingDeregister(); err == nil {
		t.Fatal("RetryPendingDeregister reported success")
	}
	if pendingIn(t, s) == nil {
		t.Fatal("a failed retry dropped the pending slot")
	}
}

func TestRetryPendingDeregisterIsANoOpWithNothingPending(t *testing.T) {
	_, _, h := newSignedInHub(t)
	if err := RetryPendingDeregister(); err != nil {
		t.Fatalf("RetryPendingDeregister: %v", err)
	}
	if n := len(h.requestsTo("/api/device/deregister")); n != 0 {
		t.Fatalf("deregister requests = %d, want none", n)
	}
}

// Signing back in to the same account hands the identity its old registration,
// so the pending slot is now the live device and must not be retried.
func TestLoginDropsASlotItsIdentityNowHolds(t *testing.T) {
	s, ident, h := newSignedInHub(t)
	h.offline()
	_ = Deregister()
	Logout()

	h.online()
	h.handle("/api/device/deregister", replyCode(500, "INTERNAL_ERROR"))
	h.handle("/api/client/token-login", replyWith(http.StatusOK, map[string]any{
		"vpnAccessToken": "lk", "user": map[string]string{"email": "a@example.com"}, "servers": []serverInfo{},
	}))
	h.handle("/api/device/register", replyWith(http.StatusOK, map[string]string{"deviceId": "d1", "assignedIp": "10.0.0.2"}))
	h.handle("/api/client/regions", replyWith(http.StatusOK, regionsWith(t, "mine")))

	if _, err := Login("token"); err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got := bodyFields(t, h.requestsTo("/api/device/register")[0])["identityPubkey"]; got != ident.pubB64 {
		t.Fatalf("Login registered %q, want the kept identity", got)
	}
	if pendingIn(t, s) != nil {
		t.Fatal("the live device is still pending deregistration")
	}
}

func TestWipeForgetsAPendingSlot(t *testing.T) {
	s, _, h := newSignedInHub(t)
	h.offline()
	_ = Deregister()

	if err := Wipe(); err != nil {
		t.Fatalf("Wipe: %v", err)
	}
	if pendingIn(t, s) != nil {
		t.Fatal("a wipe left the signed-out license behind")
	}
}
