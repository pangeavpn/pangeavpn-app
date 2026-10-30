package mobile

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestHubStatusErrorKeepsTodaysText(t *testing.T) {
	cases := []struct {
		name     string
		status   int
		body     string
		wantText string
		wantCode string
		wantAuth bool
	}{
		{"invalid license", 401, `{"error":"Invalid license key","code":"INVALID_LICENSE","requestId":"r1"}`,
			`hub auth error (401): {"error":"Invalid license key","code":"INVALID_LICENSE","requestId":"r1"}`, "INVALID_LICENSE", true},
		{"forbidden", 403, `{"error":"nope","code":"FORBIDDEN"}`,
			`hub auth error (403): {"error":"nope","code":"FORBIDDEN"}`, "FORBIDDEN", true},
		{"device not registered is auth at any status", 404, `{"error":"x","code":"DEVICE_NOT_REGISTERED"}`,
			`hub auth error (404): {"error":"x","code":"DEVICE_NOT_REGISTERED"}`, "DEVICE_NOT_REGISTERED", true},
		{"other answer", 503, `{"error":"no key","code":"DEVICE_NAMES_UNAVAILABLE"}`,
			`hub error (503): {"error":"no key","code":"DEVICE_NAMES_UNAVAILABLE"}`, "DEVICE_NAMES_UNAVAILABLE", false},
		{"unparsable body", 502, `<html>bad gateway</html>`, `hub error (502): <html>bad gateway</html>`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := hubStatusError(tc.status, []byte(tc.body))
			if err.Error() != tc.wantText {
				t.Fatalf("got %q, want %q", err.Error(), tc.wantText)
			}
			var answer *hubAnswerError
			if !errors.As(err, &answer) {
				t.Fatalf("got %T, want *hubAnswerError", err)
			}
			if answer.Status != tc.status || answer.Code != tc.wantCode || answer.Auth != tc.wantAuth || answer.Text != tc.body {
				t.Fatalf("got %+v", answer)
			}
			if got := hubErrorCode(err); got != tc.wantCode {
				t.Fatalf("hubErrorCode = %q, want %q", got, tc.wantCode)
			}
			if !hubAnswered(err) {
				t.Fatal("a status the hub sent must count as answered")
			}
		})
	}
}

func TestHubStatusErrorSeparatesTheSubscriptionAnswers(t *testing.T) {
	expired := hubStatusError(403, []byte(`{"error":"Subscription expired","code":"SUBSCRIPTION_EXPIRED"}`))
	if expired != errSubscriptionExpired {
		t.Fatalf("got %v, want errSubscriptionExpired", expired)
	}
	if expired.Error() != "SUBSCRIPTION_EXPIRED: this account's subscription has expired" {
		t.Fatalf("expired text changed: %q", expired.Error())
	}

	none := hubStatusError(403, []byte(`{"error":"No active subscription","code":"NO_ACTIVE_SUBSCRIPTION"}`))
	if none != errNoSubscription {
		t.Fatalf("got %v, want errNoSubscription", none)
	}
	if !strings.Contains(none.Error(), "NO_SUBSCRIPTION") {
		t.Fatalf("%q lacks the NO_SUBSCRIPTION marker", none.Error())
	}
	for _, marker := range []string{"hub auth error", "SUBSCRIPTION_EXPIRED", "not authenticated", "DEVICE_NOT_REGISTERED"} {
		if strings.Contains(none.Error(), marker) {
			t.Fatalf("%q contains %q, which the host maps to another case", none.Error(), marker)
		}
	}
	for _, err := range []error{expired, none, fmt.Errorf("prepare: %w", none)} {
		if !hubAnswered(err) {
			t.Fatalf("hubAnswered(%v) = false", err)
		}
		if isSignOutCode(err) {
			t.Fatalf("isSignOutCode(%v) = true; a subscription answer must keep the session", err)
		}
	}
}

func TestHubAnsweredIgnoresTransportFailures(t *testing.T) {
	for _, err := range []error{
		nil,
		errors.New("hub unreachable: retrying in 12s"),
		errors.New("secure channel error (502): bad gateway"),
		fmt.Errorf("hub request failed: %w", errors.New("dial tcp: timeout")),
	} {
		if hubAnswered(err) || hubErrorCode(err) != "" || isSignOutCode(err) {
			t.Fatalf("%v was treated as a hub answer", err)
		}
	}
	wrapped := fmt.Errorf("list: %w", hubStatusError(404, []byte(`{"code":"DEVICE_NOT_FOUND"}`)))
	if !hubAnswered(wrapped) || hubErrorCode(wrapped) != "DEVICE_NOT_FOUND" {
		t.Fatalf("a wrapped answer lost its code: %v", wrapped)
	}
}

func TestIsSignOutCodeMatchesCodesNotStatuses(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   bool
	}{
		{401, "INVALID_LICENSE", true},
		{401, "MISSING_LICENSE", true},
		{401, "MISSING_LICENSE_KEY", true},
		{404, "DEVICE_NOT_REGISTERED", true},
		{403, "FORBIDDEN", true},
		{429, "AUTH_LOCKED", false},
		{401, "", false},
		{403, "SOMETHING_NEW", false},
		{404, "DEVICE_NOT_FOUND", false},
		{500, "INTERNAL_ERROR", false},
	}
	for _, tc := range cases {
		body := fmt.Sprintf(`{"error":"x","code":%q}`, tc.code)
		if got := isSignOutCode(hubStatusError(tc.status, []byte(body))); got != tc.want {
			t.Errorf("isSignOutCode(%d %s) = %v, want %v", tc.status, tc.code, got, tc.want)
		}
	}
}
