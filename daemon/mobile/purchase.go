package mobile

// App Store purchases credited through the hub, which forwards them to the
// website; the website verifies each transaction with Apple and owns billing.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const (
	applePurchaseRoute = "/api/client/apple/transaction"
	// maxSignedTransactionBytes is far above a StoreKit 2 JWS, which is a few KiB.
	maxSignedTransactionBytes = 64 << 10
)

// The host maps these markers, so it can tell a refused purchase from an outage.
var (
	errPurchaseTransaction = errors.New("APPLE_PURCHASE_INVALID: not a signed App Store transaction")
	errPurchaseUnverified  = errors.New("APPLE_PURCHASE_INVALID: the transaction could not be verified")
	errPurchaseConflict    = errors.New("APPLE_PURCHASE_CONFLICT: this purchase belongs to another account")
	errPurchaseAnswer      = errors.New("APPLE_PURCHASE_BAD_ANSWER: the hub's answer did not decode")
)

// purchaseUnreachable is the one retryable marker, whether the hub or the
// website behind it could not be reached.
const purchaseUnreachable = "APPLE_PURCHASE_UNREACHABLE: couldn't reach the server"

// applePurchaseAnswer uses pointers to tell a missing field from a zero one:
// the host decodes every field as required.
type applePurchaseAnswer struct {
	AccountNumber *string `json:"accountNumber"`
	Created       *bool   `json:"created"`
	ExpiresAt     *string `json:"expiresAt"`
	ProductID     *string `json:"productId"`
}

func (a applePurchaseAnswer) complete() bool {
	return a.AccountNumber != nil && strings.TrimSpace(*a.AccountNumber) != "" &&
		a.Created != nil && a.ExpiresAt != nil && a.ProductID != nil
}

// applePurchaseBody leaves accountNumber out when blank, which asks the website
// for a new account.
func applePurchaseBody(signedTransaction, accountNumber string) ([]byte, error) {
	jws := strings.TrimSpace(signedTransaction)
	if !isCompactJWS(jws) {
		return nil, errPurchaseTransaction
	}
	body := map[string]string{"signedTransaction": jws}
	if account := strings.TrimSpace(accountNumber); account != "" {
		body["accountNumber"] = account
	}
	return json.Marshal(body)
}

// isCompactJWS checks the shape only, so junk never costs a request; the
// website checks the signature.
func isCompactJWS(s string) bool {
	if s == "" || len(s) > maxSignedTransactionBytes {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" || strings.IndexFunc(part, isNotBase64URL) >= 0 {
			return false
		}
	}
	return true
}

func isNotBase64URL(r rune) bool {
	return !isASCIIAlnum(r) && r != '-' && r != '_'
}

// parseApplePurchase returns the hub's answer as it came. No error carries the
// body, since it holds the account number.
func parseApplePurchase(status int, body []byte) (string, error) {
	if status < 200 || status >= 300 {
		return "", applePurchaseRefusal(status, body)
	}
	var answer applePurchaseAnswer
	if json.Unmarshal(body, &answer) != nil || !answer.complete() {
		return "", errPurchaseAnswer
	}
	return string(body), nil
}

// applePurchaseRefusal names the status and the hub's code, never its text,
// which may echo what was sent.
func applePurchaseRefusal(status int, body []byte) error {
	switch {
	case status == 400:
		return errPurchaseUnverified
	case status == 409:
		return errPurchaseConflict
	}
	// The hub sends 502 and 503 when the website is down or unconfigured.
	marker := "APPLE_PURCHASE_REJECTED"
	if status >= 500 {
		marker = purchaseUnreachable
	}
	if code := refusalCode(body); code != "" {
		return fmt.Errorf("%s: hub answered %d (%s)", marker, status, code)
	}
	return fmt.Errorf("%s: hub answered %d", marker, status)
}

// refusalCode is the hub's error code, "" when it is not one or when redaction
// would cut it, as it would an account number.
func refusalCode(body []byte) string {
	code := hubBodyCode(body)
	if !hubCodePattern.MatchString(code) || redactDiagnostics(code, "") != code {
		return ""
	}
	return code
}
