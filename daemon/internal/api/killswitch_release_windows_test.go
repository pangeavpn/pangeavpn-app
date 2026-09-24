//go:build windows && (amd64 || arm64)

package api

import (
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// The release is asserted, not part of KillSwitch, so a rename would silently
// leave WCM bad-state tracking off for good after a lock lost without a Clear.
func TestWindowsKillSwitchReleasesOrphanedSettings(t *testing.T) {
	if _, ok := platform.NewKillSwitch().(orphanedSettingsReleaser); !ok {
		t.Fatal("the Windows kill switch does not implement ReleaseOrphanedSettings")
	}
}
