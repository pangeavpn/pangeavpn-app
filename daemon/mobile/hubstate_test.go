package mobile

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var trackerNow = time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

// A failed search is believed for a while, so every caller after it fails fast
// instead of walking the whole ladder again.
func TestHubTrackerCoolsDownAfterAFailedSearch(t *testing.T) {
	var tracker hubTracker
	if wait := tracker.retryIn(trackerNow); wait != 0 {
		t.Fatalf("fresh tracker waits %v", wait)
	}
	tracker.searchFailed(trackerNow)
	if wait := tracker.retryIn(trackerNow.Add(5 * time.Second)); wait != 15*time.Second {
		t.Fatalf("retryIn = %v, want 15s left", wait)
	}
	if wait := tracker.retryIn(trackerNow.Add(hubRetryCooldown)); wait != 0 {
		t.Fatalf("retryIn = %v after the cooldown, want 0", wait)
	}
}

// A deliberate change of plan must not be held back by the last failure.
func TestHubTrackerInvalidateClearsTheCooldownAndMovesTheGeneration(t *testing.T) {
	var tracker hubTracker
	tracker.searchFailed(trackerNow)
	before := tracker.generation
	tracker.invalidate()
	if tracker.retryIn(trackerNow) != 0 {
		t.Fatal("invalidate left the cooldown in place")
	}
	if tracker.generation == before {
		t.Fatal("invalidate must move the generation so a running search is discarded")
	}
}

func TestHubTrackerDropsAPathAfterRepeatedFailures(t *testing.T) {
	var tracker hubTracker
	if tracker.requestFailed() {
		t.Fatal("one failure must not drop a path that just proved itself")
	}
	if !tracker.requestFailed() {
		t.Fatalf("failure %d should drop the path", hubFailureLimit)
	}
	if tracker.requestFailed() {
		t.Fatal("the streak should restart after a drop")
	}
	tracker.requestSucceeded()
	if tracker.requestFailed() {
		t.Fatal("a success should reset the streak")
	}
}

func TestSearchHubMethodsTriesInOrderAndStopsAtTheFirstWin(t *testing.T) {
	var tried []string
	got, ok := searchHubMethods([]string{"directIp", "reality", "shadowsocks"}, func(method string) (string, bool) {
		tried = append(tried, method)
		return method, method == "reality"
	}, nil)
	if !ok || got != "reality" {
		t.Fatalf("got (%q, %v), want reality", got, ok)
	}
	if !sameStrings(tried, []string{"directIp", "reality"}) {
		t.Fatalf("tried %v, want to stop after the winner", tried)
	}
}

func TestSearchHubMethodsReseedsOnceThenRetries(t *testing.T) {
	reseeds, rounds := 0, 0
	got, ok := searchHubMethods([]string{"directIp"}, func(string) (string, bool) {
		rounds++
		return "fresh", rounds == 2
	}, func() bool {
		reseeds++
		return true
	})
	if !ok || got != "fresh" || reseeds != 1 {
		t.Fatalf("got (%q, %v) after %d reseeds, want a win after one", got, ok, reseeds)
	}
}

func TestSearchHubMethodsSkipsTheRetryWhenTheReseedChangedNothing(t *testing.T) {
	rounds := 0
	_, ok := searchHubMethods([]string{"directIp", "normal"}, func(string) (string, bool) {
		rounds++
		return "", false
	}, func() bool { return false })
	if ok || rounds != 2 {
		t.Fatalf("ok=%v after %d attempts, want one pass and a failure", ok, rounds)
	}
}

func TestRaceStaggeredTakesTheFirstAnswer(t *testing.T) {
	got, ok := raceStaggered(3, 20*time.Millisecond, func(ctx context.Context, index int) (string, bool) {
		if index == 0 {
			<-ctx.Done()
			return "", false
		}
		return []string{"", "one", "two"}[index], true
	})
	if !ok || got != "one" {
		t.Fatalf("got (%q, %v), want the second provider's answer", got, ok)
	}
}

func TestRaceStaggeredStopsLaunchingOnceSomeoneWins(t *testing.T) {
	var started atomic.Int32
	got, ok := raceStaggered(4, 200*time.Millisecond, func(context.Context, int) (string, bool) {
		started.Add(1)
		return "fast", true
	})
	if !ok || got != "fast" {
		t.Fatalf("got (%q, %v)", got, ok)
	}
	time.Sleep(250 * time.Millisecond)
	if n := started.Load(); n != 1 {
		t.Fatalf("%d attempts started, want the rest never launched", n)
	}
}

func TestRaceStaggeredFailsWhenEveryAttemptFails(t *testing.T) {
	if got, ok := raceStaggered(3, time.Millisecond, func(context.Context, int) (string, bool) { return "", false }); ok {
		t.Fatalf("got %q, want a failure", got)
	}
}

func TestOpenHubReplyRejectsWhatIsNotASealedReply(t *testing.T) {
	_, keys := newTestHub(t)
	sealed, err := sealRequestWith(keys, "GET", "/api/client/regions", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		body   string
		status int
		want   string
	}{
		{"bad gateway", "upstream down", 502, "502"},
		{"captive portal", "  <html>login</html>", 200, "HTML"},
		{"garbage", "{not json", 200, "decode"},
		{"wrong key", `{"iv":"AAAAAAAAAAAAAAAA","ct":"AA==","tag":"AAAAAAAAAAAAAAAAAAAAAA=="}`, 200, "decrypt"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := openHubReply(sealed, []byte(tt.body), tt.status)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want one mentioning %q", err, tt.want)
			}
		})
	}
}
