package mobile

// Typed hub answers, so callers can tell "the hub said no" from "the hub
// could not be reached" without matching message text.

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// errSubscriptionExpired is distinct so the host shows a top-up prompt rather
// than treating it as a failed sign-in.
var errSubscriptionExpired = errors.New("SUBSCRIPTION_EXPIRED: this account's subscription has expired")

// errNoSubscription is an account that never had a plan. The host maps the
// NO_SUBSCRIPTION marker, so the text must avoid every sign-out marker.
var errNoSubscription = errors.New("NO_SUBSCRIPTION: this account has no active subscription")

// errUndecodableAnswer is a 2xx whose body did not decode: the hub was reached,
// so no cache may stand in for it.
var errUndecodableAnswer = errors.New("decode hub response")

func undecodableAnswer(err error) error {
	return fmt.Errorf("%w: %w", errUndecodableAnswer, err)
}

// hubAnswerError is a status the hub itself sent through the sealed channel.
type hubAnswerError struct {
	Status int
	// Code is the hub's {"error","code","requestId"} code, "" if unparsable.
	Code string
	Text string
	Auth bool
}

func (e *hubAnswerError) Error() string {
	if e.Auth {
		return fmt.Sprintf("hub auth error (%d): %s", e.Status, e.Text)
	}
	return fmt.Sprintf("hub error (%d): %s", e.Status, e.Text)
}

// hubStatusError classifies a non-2xx inner answer.
func hubStatusError(status int, body []byte) error {
	text := string(body)
	// Checked before the auth branch: a lapsed subscription is also a 403,
	// but signing the user out would discard the token they still need.
	if strings.Contains(text, "SUBSCRIPTION_EXPIRED") {
		return errSubscriptionExpired
	}
	code := hubBodyCode(body)
	if code == "NO_ACTIVE_SUBSCRIPTION" {
		return errNoSubscription
	}
	return &hubAnswerError{
		Status: status,
		Code:   code,
		Text:   text,
		Auth:   status == 401 || status == 403 || strings.Contains(text, "DEVICE_NOT_REGISTERED"),
	}
}

func hubBodyCode(body []byte) string {
	var parsed struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &parsed) != nil {
		return ""
	}
	return parsed.Code
}

// hubErrorCode is the hub's code for err, "" when the hub did not answer.
func hubErrorCode(err error) string {
	var answer *hubAnswerError
	if errors.As(err, &answer) {
		return answer.Code
	}
	return ""
}

// hubAnswered reports whether the hub itself refused, as opposed to never
// being reached; only the latter may fall back to a cache.
func hubAnswered(err error) bool {
	var answer *hubAnswerError
	return errors.As(err, &answer) || errors.Is(err, errSubscriptionExpired) ||
		errors.Is(err, errNoSubscription) || errors.Is(err, errUndecodableAnswer)
}

// signOutCodes are the answers that say the stored session is dead for good.
var signOutCodes = map[string]bool{
	"INVALID_LICENSE":       true,
	"MISSING_LICENSE":       true,
	"MISSING_LICENSE_KEY":   true,
	"DEVICE_NOT_REGISTERED": true,
	"FORBIDDEN":             true,
}

// isSignOutCode matches codes rather than statuses: a 401/403 can also be a
// lockout or a subscription answer, and neither ends the session.
func isSignOutCode(err error) bool {
	return signOutCodes[hubErrorCode(err)]
}
