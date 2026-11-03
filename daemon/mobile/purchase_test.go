package mobile

import (
	"errors"
	"strings"
	"testing"
)

// testJWS is shaped like a StoreKit 2 signed transaction; nothing here checks a signature.
const testJWS = "eyJhbGciOiJFUzI1NiJ9.eyJ0cmFuc2FjdGlvbklkIjoiMjAwMDAwMDEyMyJ9.c2lnbmF0dXJlLV8"

const testAccount = "K7M2Q9X4C8V1B6N3R5T0W2Y7"

func TestApplePurchaseBodyOmitsABlankAccountNumber(t *testing.T) {
	cases := []struct{ name, jws, account, want string }{
		{"new account", testJWS, "", `{"signedTransaction":"` + testJWS + `"}`},
		{"blank account", testJWS, " \t", `{"signedTransaction":"` + testJWS + `"}`},
		{"signed in", " " + testJWS + "\n", " " + testAccount + " ",
			`{"accountNumber":"` + testAccount + `","signedTransaction":"` + testJWS + `"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := applePurchaseBody(tc.jws, tc.account)
			if err != nil || string(got) != tc.want {
				t.Fatalf("got %s, %v, want %s", got, err, tc.want)
			}
		})
	}
}

func TestApplePurchaseBodyRefusesWhatIsNotACompactJWS(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"blank":         "  ",
		"two parts":     "abc.def",
		"four parts":    "a.b.c.d",
		"an empty part": "abc..def",
		"a space":       "abc.d ef.ghi",
		"padding":       "abc.def.ghi=",
		"too large":     "abc.def." + strings.Repeat("g", maxSignedTransactionBytes),
	}
	for name, jws := range cases {
		t.Run(name, func(t *testing.T) {
			if body, err := applePurchaseBody(jws, testAccount); !errors.Is(err, errPurchaseTransaction) {
				t.Fatalf("got %s, %v, want the invalid-transaction error", body, err)
			}
		})
	}
}

func TestParseApplePurchaseReturnsTheAnswerAsItCame(t *testing.T) {
	raw := `{"accountNumber":"` + testAccount + `","created":true,"expiresAt":"2026-11-09T12:00:00.000Z","productId":"org.pangeavpn.app.sub.1m","extra":1}`
	for _, status := range []int{200, 201} {
		if got, err := parseApplePurchase(status, []byte(raw)); err != nil || got != raw {
			t.Fatalf("status %d: got %s, %v, want the answer unchanged", status, got, err)
		}
	}
}

func TestParseApplePurchaseRefusesAnIncompleteAnswer(t *testing.T) {
	const rest = `"created":true,"expiresAt":"2026-11-09T12:00:00.000Z","productId":"p"`
	cases := map[string]string{
		"not JSON":             "<html>bad gateway</html>",
		"empty":                "",
		"null":                 "null",
		"a list":               "[]",
		"no account":           `{` + rest + `}`,
		"a blank account":      `{"accountNumber":"  ",` + rest + `}`,
		"a numeric account":    `{"accountNumber":7,` + rest + `}`,
		"created not a bool":   `{"accountNumber":"` + testAccount + `","created":"yes","expiresAt":"x","productId":"p"}`,
		"no expiry":            `{"accountNumber":"` + testAccount + `","created":true,"productId":"p"}`,
		"a null product":       `{"accountNumber":"` + testAccount + `","created":true,"expiresAt":"x","productId":null}`,
		"no created flag":      `{"accountNumber":"` + testAccount + `","expiresAt":"x","productId":"p"}`,
		"an account in a list": `[{"accountNumber":"` + testAccount + `",` + rest + `}]`,
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := parseApplePurchase(200, []byte(raw))
			if !errors.Is(err, errPurchaseAnswer) {
				t.Fatalf("got %s, %v, want the bad-answer error", got, err)
			}
			if strings.Contains(err.Error(), testAccount) {
				t.Fatalf("error %q carries the account number", err)
			}
		})
	}
}

// The bodies echo the transaction and an account number; neither may reach the error.
func TestParseApplePurchaseMapsTheHubsRefusals(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{400, `{"error":"unverifiable ` + testJWS + `","code":"INVALID_TRANSACTION"}`, "APPLE_PURCHASE_INVALID: the transaction could not be verified"},
		{409, `{"error":"linked to ` + testAccount + `","code":"ALREADY_LINKED"}`, "APPLE_PURCHASE_CONFLICT: this purchase belongs to another account"},
		{502, `{"error":"Bad gateway","code":"UPSTREAM_TIMEOUT"}`, "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: hub answered 502 (UPSTREAM_TIMEOUT)"},
		{503, `{"error":"Unavailable","code":"WEBSITE_API_UNCONFIGURED"}`, "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: hub answered 503 (WEBSITE_API_UNCONFIGURED)"},
		{500, `upstream down: ` + testJWS, "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server: hub answered 500"},
		{429, `{"error":"Too many requests","code":"RATE_LIMITED"}`, "APPLE_PURCHASE_REJECTED: hub answered 429 (RATE_LIMITED)"},
		{403, `{"code":"` + testJWS + `"}`, "APPLE_PURCHASE_REJECTED: hub answered 403"},
		{404, `{"code":"` + testAccount + `"}`, "APPLE_PURCHASE_REJECTED: hub answered 404"},
		{404, `{"code":"NOT_FOUND"}`, "APPLE_PURCHASE_REJECTED: hub answered 404 (NOT_FOUND)"},
	}
	for _, tc := range cases {
		got, err := parseApplePurchase(tc.status, []byte(tc.body))
		if err == nil || err.Error() != tc.want {
			t.Fatalf("status %d: got %s, %v, want %q", tc.status, got, err, tc.want)
		}
		if strings.Contains(err.Error(), testJWS) || strings.Contains(err.Error(), testAccount) {
			t.Fatalf("status %d: error %q carries the transaction or the account number", tc.status, err)
		}
	}
}
