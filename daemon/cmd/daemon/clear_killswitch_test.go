package main

import (
	"bytes"
	"context"
	"errors"
	"io"
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

// The excluded app paths must not outlive an uninstall, even one whose lock clear
// failed; the session record stays so a reinstall can reconnect behind that lock.
func TestClearKillSwitchCommand_ForgetsSplitSettingsWhateverElseFails(t *testing.T) {
	previousOut := log.Writer()
	previousWarn, previousInfo := platform.KillSwitchWarnf, platform.KillSwitchInfof
	previousClear, previousSession, previousSplit := clearKillSwitch, forgetSession, forgetSplitTunnel
	log.SetOutput(io.Discard)
	t.Cleanup(func() {
		log.SetOutput(previousOut)
		platform.KillSwitchWarnf, platform.KillSwitchInfof = previousWarn, previousInfo
		clearKillSwitch, forgetSession, forgetSplitTunnel = previousClear, previousSession, previousSplit
	})

	for name, tc := range map[string]struct {
		clearErr, splitErr   error
		wantCode             int
		wantSessionForgotten bool
	}{
		"clear fails":        {clearErr: errors.New("BFE unavailable"), wantCode: 1},
		"split forget fails": {splitErr: errors.New("access denied"), wantCode: 1, wantSessionForgotten: true},
		"all succeed":        {wantCode: 0, wantSessionForgotten: true},
	} {
		t.Run(name, func(t *testing.T) {
			sessionForgotten, splitAttempts := false, 0
			clearKillSwitch = func(context.Context) error { return tc.clearErr }
			forgetSession = func() error {
				sessionForgotten = true
				return nil
			}
			forgetSplitTunnel = func() error {
				splitAttempts++
				return tc.splitErr
			}
			if code := clearKillSwitchCommand(); code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if splitAttempts != 1 {
				t.Errorf("split-tunnel settings removed %d times, want once", splitAttempts)
			}
			if sessionForgotten != tc.wantSessionForgotten {
				t.Errorf("session record forgotten = %v, want %v", sessionForgotten, tc.wantSessionForgotten)
			}
		})
	}
}
