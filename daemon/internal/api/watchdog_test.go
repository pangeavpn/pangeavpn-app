package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// The wedge this hunts answers nothing while the process looks alive; the
// dump must land even though the request itself never finishes in time.
func TestSlowRequestWatchdog_DumpsGoroutinesForAStuckHandler(t *testing.T) {
	prevThreshold, prevCooldown, prevOut := slowRequestThreshold, slowDumpCooldown, watchdogOut
	var buf bytes.Buffer
	var mu sync.Mutex
	slowRequestThreshold = 20 * time.Millisecond
	slowDumpCooldown = 0
	watchdogOut = writerFunc(func(p []byte) (int, error) {
		mu.Lock()
		defer mu.Unlock()
		return buf.Write(p)
	})
	defer func() {
		slowRequestThreshold, slowDumpCooldown, watchdogOut = prevThreshold, prevCooldown, prevOut
	}()

	handler := slowRequestWatchdog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))

	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if !strings.Contains(out, "watchdog: GET /status") {
		t.Fatalf("no watchdog banner in output: %q", out)
	}
	if !strings.Contains(out, "goroutine") {
		t.Fatalf("no goroutine dump in output: %q", out)
	}
}

func TestSlowRequestWatchdog_QuietForFastHandlers(t *testing.T) {
	prevThreshold, prevOut := slowRequestThreshold, watchdogOut
	var buf bytes.Buffer
	slowRequestThreshold = 500 * time.Millisecond
	watchdogOut = &buf
	defer func() { slowRequestThreshold, watchdogOut = prevThreshold, prevOut }()

	handler := slowRequestWatchdog(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/logs", nil))
	time.Sleep(50 * time.Millisecond)

	if buf.Len() != 0 {
		t.Fatalf("watchdog fired for a fast handler: %q", buf.String())
	}
}

type writerFunc func(p []byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }
