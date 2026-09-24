package main

import (
	"bytes"
	"log"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// The uninstaller's ExecToLog capture is the only record of --clear-killswitch,
// so a half-finished clear must reach the process log rather than vanish.
func TestLogKillSwitchWarningsReachesProcessLog(t *testing.T) {
	var buf bytes.Buffer
	previousOut, previousFlags, previousWarn := log.Writer(), log.Flags(), platform.KillSwitchWarnf
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOut)
		log.SetFlags(previousFlags)
		platform.KillSwitchWarnf = previousWarn
	})

	logKillSwitchWarnings()
	platform.KillSwitchWarn("could not restore WCM bad-state tracking: %v", "access denied")

	if !strings.Contains(buf.String(), "could not restore WCM bad-state tracking: access denied") {
		t.Fatalf("log = %q, want the kill switch warning", buf.String())
	}
}
