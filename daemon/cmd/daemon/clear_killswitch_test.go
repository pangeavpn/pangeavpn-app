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
	previousOut, previousFlags := log.Writer(), log.Flags()
	previousWarn, previousInfo := platform.KillSwitchWarnf, platform.KillSwitchInfof
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previousOut)
		log.SetFlags(previousFlags)
		platform.KillSwitchWarnf = previousWarn
		platform.KillSwitchInfof = previousInfo
	})

	logKillSwitchWarnings()
	platform.KillSwitchWarn("could not restore WCM bad-state tracking: %v", "access denied")
	platform.KillSwitchInfo("restored Windows WCM bad-state tracking")

	for _, want := range []string{"could not restore WCM bad-state tracking: access denied", "restored Windows WCM bad-state tracking"} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("log = %q, want %q", buf.String(), want)
		}
	}
}
