//go:build android || ios

package mobile

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func answerPurchase(created bool) func(fakeRequest) (int, any) {
	return replyWith(http.StatusOK, map[string]any{
		"accountNumber": testAccount, "created": created,
		"expiresAt": "2026-11-09T12:00:00.000Z", "productId": "org.pangeavpn.app.sub.1m",
	})
}

func purchaseAnswerJSON(created string) string {
	return `{"accountNumber":"` + testAccount + `","created":` + created +
		`,"expiresAt":"2026-11-09T12:00:00.000Z","productId":"org.pangeavpn.app.sub.1m"}`
}

func TestApplePurchaseSendsTheTransactionWithoutTheLicense(t *testing.T) {
	s, _, h := newSignedInHub(t)
	h.handle(applePurchaseRoute, answerPurchase(false))

	raw, err := ApplePurchase(testJWS, "lk")
	if err != nil || raw != purchaseAnswerJSON("false") {
		t.Fatalf("ApplePurchase = %s, %v", raw, err)
	}
	sent := h.requestsTo(applePurchaseRoute)
	if len(sent) != 1 || sent[0].Method != http.MethodPost {
		t.Fatalf("purchase requests = %+v, want one POST", sent)
	}
	if len(sent[0].Headers) != 2 || sent[0].Headers["Content-Type"] != "application/json" || sent[0].Headers["X-Pangea-App"] == "" {
		t.Fatalf("headers = %v, want only Content-Type and X-Pangea-App", sent[0].Headers)
	}
	if fields := bodyFields(t, sent[0]); len(fields) != 2 || fields["signedTransaction"] != testJWS || fields["accountNumber"] != "lk" {
		t.Fatalf("body = %s", sent[0].Body)
	}
	if lk, _, _, _ := sessionState(); lk != "lk" || s.Get(keyLicense) != "lk" {
		t.Fatal("a purchase changed the session")
	}
}

// Signing in to a new account is the host's Login, never the purchase's.
func TestApplePurchaseForANewAccountSendsNoAccountNumber(t *testing.T) {
	s, _ := initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(applePurchaseRoute, answerPurchase(true))

	raw, err := ApplePurchase(testJWS, "")
	if err != nil || raw != purchaseAnswerJSON("true") {
		t.Fatalf("ApplePurchase = %s, %v", raw, err)
	}
	if fields := bodyFields(t, h.requestsTo(applePurchaseRoute)[0]); len(fields) != 1 || fields["signedTransaction"] != testJWS {
		t.Fatalf("body = %v, want the transaction alone", fields)
	}
	if lk, _, _, _ := sessionState(); lk != "" || s.Get(keyLicense) != "" {
		t.Fatal("the purchase signed in to the new account")
	}
}

func TestApplePurchaseSurfacesTheHubsRefusals(t *testing.T) {
	cases := []struct {
		name  string
		reply func(fakeRequest) (int, any)
		want  string
	}{
		{"unverifiable", replyCode(400, "INVALID_TRANSACTION"), "APPLE_PURCHASE_INVALID: the transaction could not be verified"},
		{"linked elsewhere", replyCode(409, "ALREADY_LINKED"), "APPLE_PURCHASE_CONFLICT: this purchase belongs to another account"},
		{"website down", replyCode(502, "UPSTREAM_ERROR"), "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: hub answered 502 (UPSTREAM_ERROR)"},
		{"website unconfigured", replyCode(503, "WEBSITE_API_UNCONFIGURED"),
			"APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: hub answered 503 (WEBSITE_API_UNCONFIGURED)"},
		{"hub error", replyWith(500, "Internal error"), "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: hub answered 500"},
		{"hub without the route", replyCode(403, "ROUTE_NOT_ALLOWED"), "APPLE_PURCHASE_REJECTED: hub answered 403 (ROUTE_NOT_ALLOWED)"},
		{"a malformed answer", replyWith(http.StatusOK, map[string]any{"created": true}), errPurchaseAnswer.Error()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, h := newSignedInHub(t)
			h.handle(applePurchaseRoute, tc.reply)
			raw, err := ApplePurchase(testJWS, "lk")
			if err == nil || err.Error() != tc.want {
				t.Fatalf("ApplePurchase = %s, %v, want %q", raw, err, tc.want)
			}
			if strings.Contains(err.Error(), testJWS) {
				t.Fatalf("error %q carries the transaction", err)
			}
		})
	}
}

func TestApplePurchaseChecksTheTransactionBeforeTheHub(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	if _, err := ApplePurchase("not a transaction", ""); !errors.Is(err, errPurchaseTransaction) {
		t.Fatalf("ApplePurchase = %v, want the invalid-transaction error", err)
	}
	if got := len(h.requests()); got != 0 {
		t.Fatalf("hub requests = %d, want none", got)
	}
}

func TestApplePurchaseReportsUnreachable(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(applePurchaseRoute, answerPurchase(true))
	h.offline()

	_, err := ApplePurchase(testJWS, "")
	if err == nil || !strings.HasPrefix(err.Error(), purchaseUnreachable+": hub unreachable: retrying in") {
		t.Fatalf("ApplePurchase = %v, want APPLE_PURCHASE_UNREACHABLE", err)
	}
	if got := len(h.requestsTo(applePurchaseRoute)); got != 0 {
		t.Fatalf("purchase requests = %d, want none", got)
	}
}

// The hub waits 15s on the website before its 502, so the purchase must wait longer.
func TestApplePurchaseOutwaitsTheHubsWaitOnTheWebsite(t *testing.T) {
	if applePurchaseTimeout <= hubRequestTimeout {
		t.Fatalf("purchase timeout %v, want more than the hub's 15s wait", applePurchaseTimeout)
	}
	paths := map[string]*hubPath{
		"directIp": newDirectIPPath("203.0.113.7"),
		"normal":   newNormalPath("hub.example"),
		"fronted":  newFrontedPath("relay.example"),
		"proxy":    newProxyPath("reality", "node", 1080, "user", "pass"),
	}
	for kind, path := range paths {
		if path.client.Timeout < applePurchaseTimeout {
			t.Errorf("%s client gives up after %v, before the purchase's %v", kind, path.client.Timeout, applePurchaseTimeout)
		}
	}
}

func TestApplePurchaseUsesItsOwnTimeout(t *testing.T) {
	initForTest(t, nil)
	h := newFakeHub(t)
	release := make(chan struct{})
	h.handle(applePurchaseRoute, func(req fakeRequest) (int, any) {
		<-release
		return answerPurchase(true)(req)
	})
	previous := applePurchaseTimeout
	applePurchaseTimeout = 50 * time.Millisecond
	t.Cleanup(func() { applePurchaseTimeout = previous })

	_, err := ApplePurchase(testJWS, "")
	close(release)
	if !errors.Is(err, context.DeadlineExceeded) || !strings.HasPrefix(err.Error(), purchaseUnreachable) {
		t.Fatalf("ApplePurchase = %v, want a retryable timeout", err)
	}
}

// The host finishes a transaction only on success, so a dropped answer is
// retried, not lost.
func TestApplePurchaseDropsAnAnswerThatLandsAfterAWipe(t *testing.T) {
	s, _ := initForTest(t, nil)
	h := newFakeHub(t)
	h.handle(applePurchaseRoute, func(req fakeRequest) (int, any) {
		if err := Wipe(); err != nil {
			t.Errorf("Wipe: %v", err)
		}
		return answerPurchase(true)(req)
	})

	raw, err := ApplePurchase(testJWS, "")
	if !errors.Is(err, errSessionEnded) || raw != "" {
		t.Fatalf("ApplePurchase = %s, %v, want the session-ended error", raw, err)
	}
	if strings.Contains(err.Error(), "UNREACHABLE") {
		t.Fatalf("error %q reads as an outage", err)
	}
	assertWiped(t, s)
}
