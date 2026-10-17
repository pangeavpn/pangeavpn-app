//go:build darwin

package platform

import (
	"strings"
	"testing"
)

func TestBuildDarwinUninstallScriptUnwiresPFConfBeforeRemovingAnchor(t *testing.T) {
	script := buildDarwinUninstallScript(1234)
	unwire := strings.Index(script, "/usr/bin/grep -vF '"+pfAnchorLine+"' "+pfConfPath)
	removeAnchor := strings.Index(script, "/bin/rm -f "+pfAnchorFilePath)
	reload := strings.Index(script, "/sbin/pfctl -f "+pfConfPath)
	if unwire < 0 || removeAnchor < 0 || reload < 0 {
		t.Fatalf("script missing pf.conf unwire, anchor removal, or reload:\n%s", script)
	}
	if !(unwire < removeAnchor && removeAnchor < reload) {
		t.Error("pf.conf must be unwired before the anchor file goes and pf reloads")
	}
	if !strings.HasSuffix(script, "/bin/launchctl bootout system/com.pangea.pangeavpn.daemon 2>/dev/null || true") {
		t.Error("booting out the daemon must be the last step")
	}
}
