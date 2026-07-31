package main

import (
	"fmt"
	"log"
	"os"
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
