//go:build android || ios

package mobile

import (
	"errors"
	"fmt"
	"net/http"
)

// applePurchaseTimeout lets the hub's 502 arrive when the website is slow,
// rather than a local timeout; a var so tests can shorten it.
var applePurchaseTimeout = hubSlowRequestTimeout

// ApplePurchase hands a StoreKit 2 signed transaction to the hub, crediting
// accountNumber, or a new account when it is "", and returns the answer JSON.
func ApplePurchase(signedTransaction, accountNumber string) (string, error) {
	body, err := applePurchaseBody(signedTransaction, accountNumber)
	if err != nil {
		return "", err
	}
	// The hub links by the body's account number, so no license header goes.
	answer, status, err := hubFetchWithin(currentWipeEpoch(), applePurchaseTimeout, applePurchaseRoute,
		http.MethodPost, map[string]string{"Content-Type": "application/json"}, body)
	if errors.Is(err, errSessionEnded) {
		return "", err
	}
	if err != nil {
		return "", fmt.Errorf("%s: %w", purchaseUnreachable, err)
	}
	return parseApplePurchase(status, answer)
}
