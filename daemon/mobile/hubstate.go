package mobile

// Bookkeeping the desktop hub client keeps beside its resolved path, out of
// the android-only files so it is testable on the host.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// hubRetryCooldown is how long a fully failed search is believed.
	hubRetryCooldown = 20 * time.Second
	// hubFailureLimit is how many failed requests a resolved path survives.
	hubFailureLimit = 2
	// dohStagger overlaps the providers: taken in turn, a network that drops
	// DoH held every later hub method back by 12s.
	dohStagger = time.Second
)

// hubTracker is guarded by hubMu.
type hubTracker struct {
	failedAt time.Time
	streak   int
	// generation moves on every deliberate change, so a search that started
	// under the old settings is discarded rather than published.
	generation uint64
}

func (t *hubTracker) retryIn(now time.Time) time.Duration {
	if t.failedAt.IsZero() {
		return 0
	}
	if left := hubRetryCooldown - now.Sub(t.failedAt); left > 0 {
		return left
	}
	return 0
}

func (t *hubTracker) searchFailed(now time.Time) { t.failedAt = now }

func (t *hubTracker) requestSucceeded() {
	t.streak = 0
	t.failedAt = time.Time{}
}

// requestFailed reports whether the path should be dropped now.
func (t *hubTracker) requestFailed() bool {
	t.streak++
	if t.streak < hubFailureLimit {
		return false
	}
	t.streak = 0
	return true
}

func (t *hubTracker) invalidate() {
	t.generation++
	t.streak = 0
	t.failedAt = time.Time{}
}

// searchHubMethods tries each method in order; if all fail it spends one
// reseed, and only walks them again when the reseed changed something.
func searchHubMethods[P any](methods []string, try func(method string) (P, bool), reseed func() bool) (P, bool) {
	if path, ok := firstWorkingMethod(methods, try); ok {
		return path, true
	}
	if reseed != nil && reseed() {
		return firstWorkingMethod(methods, try)
	}
	var none P
	return none, false
}

func firstWorkingMethod[P any](methods []string, try func(string) (P, bool)) (P, bool) {
	for _, method := range methods {
		if path, ok := try(method); ok {
			return path, true
		}
	}
	var none P
	return none, false
}

// raceStaggered starts attempt i at i*stagger, settles on the first answer and
// cancels the rest; attempts not yet started never start.
func raceStaggered(count int, stagger time.Duration, attempt func(ctx context.Context, index int) (string, bool)) (string, bool) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type answer struct {
		value string
		ok    bool
	}
	answers := make(chan answer, count)
	for index := 0; index < count; index++ {
		go func(index int) {
			if index > 0 {
				select {
				case <-time.After(time.Duration(index) * stagger):
				case <-ctx.Done():
					answers <- answer{}
					return
				}
			}
			value, ok := attempt(ctx, index)
			answers <- answer{value, ok}
		}(index)
	}
	for pending := count; pending > 0; pending-- {
		if got := <-answers; got.ok {
			return got.value, true
		}
	}
	return "", false
}

// openHubReply turns one transport answer into the hub's inner response,
// refusing error pages and anything not sealed for this request.
func openHubReply(sealed sealedRequest, body []byte, status int) (innerResponse, error) {
	if status < 200 || status >= 300 {
		return innerResponse{}, fmt.Errorf("secure channel error (%d): %s", status, clipForError(body))
	}
	if trimmed := bytes.TrimSpace(body); len(trimmed) > 0 && trimmed[0] == '<' {
		return innerResponse{}, errors.New("secure channel answered with an HTML page, not the hub")
	}
	var encrypted encryptedResponse
	if err := json.Unmarshal(body, &encrypted); err != nil {
		return innerResponse{}, fmt.Errorf("decode secure response: %w", err)
	}
	return sealed.open(encrypted)
}
