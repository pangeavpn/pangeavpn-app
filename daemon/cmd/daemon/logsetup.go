package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	logMaxBytes    = 8 << 20
	logKeepRotated = 3
)

// attachLogFile mirrors the log ring buffer to disk and captures raw stderr.
// Without both, a crash erases the only record of what led to it.
func attachLogFile(logs *state.LogStore) {
	logPath, err := platform.LogPath()
	if err != nil {
		log.Printf("log file disabled: %v", err)
		return
	}

	if err := logs.AttachFile(logPath, logMaxBytes, logKeepRotated); err != nil {
		log.Printf("log file disabled: %v", err)
		return
	}
	log.Printf("log file: %s", logPath)

	crashPath, err := platform.CrashLogPath()
	if err != nil {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("crash log disabled: %v", err))
		return
	}

	if _, err := redirectStderr(crashPath); err != nil {
		logs.Add(state.LogWarn, state.SourceDaemon, fmt.Sprintf("crash log disabled: %v", err))
		return
	}

	fmt.Fprintf(os.Stderr, "\n=== daemon start %s ===\n", time.Now().Format(time.RFC3339))
	logs.Add(state.LogInfo, state.SourceDaemon, fmt.Sprintf("crash log: %s", crashPath))
}

const resolveWarnRepeat = 10 * time.Minute

var (
	resolveWarnMu     sync.Mutex
	resolveWarnedKeys = map[string]time.Time{}
)

// resolveWarningIsNew keeps one unreachable permit host to one line per window:
// every profile sync retries the same names and they fail every time behind a lock.
func resolveWarningIsNew(host string, err error) bool {
	key := host + "|" + err.Error()
	resolveWarnMu.Lock()
	defer resolveWarnMu.Unlock()

	now := time.Now()
	if at, seen := resolveWarnedKeys[key]; seen && now.Sub(at) < resolveWarnRepeat {
		return false
	}
	if len(resolveWarnedKeys) > 256 {
		resolveWarnedKeys = map[string]time.Time{}
	}
	resolveWarnedKeys[key] = now
	return true
}
