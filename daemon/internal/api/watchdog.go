package api

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime/pprof"
	"sync"
	"time"
)

// slowRequestThreshold is how long a request may run before the watchdog
// dumps every goroutine stack, naming whatever is blocking the handlers.
var slowRequestThreshold = 10 * time.Second

// slowDumpCooldown spaces dumps out so a wedge polled at 1Hz cannot flood
// the crash log with one dump per stuck request.
var slowDumpCooldown = 2 * time.Minute

// watchdogOut receives the dumps. Stderr is redirected to the crash log at
// startup, so dumps survive independently of the in-memory LogStore.
var watchdogOut io.Writer = os.Stderr

var (
	watchdogMu       sync.Mutex
	lastWatchdogDump time.Time
)

func slowRequestWatchdog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path := r.Method, r.URL.Path
		timer := time.AfterFunc(slowRequestThreshold, func() {
			dumpGoroutines(method, path)
		})
		defer timer.Stop()
		next.ServeHTTP(w, r)
	})
}

func dumpGoroutines(method, path string) {
	watchdogMu.Lock()
	defer watchdogMu.Unlock()
	if time.Since(lastWatchdogDump) < slowDumpCooldown {
		return
	}
	lastWatchdogDump = time.Now()

	fmt.Fprintf(watchdogOut, "=== watchdog: %s %s unanswered after %s at %s; goroutine dump ===\n",
		method, path, slowRequestThreshold, time.Now().Format(time.RFC3339))
	if profile := pprof.Lookup("goroutine"); profile != nil {
		_ = profile.WriteTo(watchdogOut, 2)
	}
	fmt.Fprintln(watchdogOut, "=== watchdog: end of dump ===")
}
