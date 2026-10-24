//go:build linux

package platform

import (
	"strings"
	"testing"
)

func TestBuildLinuxUninstallScript(t *testing.T) {
	script := buildLinuxUninstallScript(1234, 99)
	for _, want := range []string{
		"kill -0 1234",
		"systemctl disable pangea-daemon pangea-killswitch-boot",
		"dpkg --purge pangeavpn",
		"rm -rf /etc/pangeavpn /opt/PangeaVPN",
	} {
		if !strings.Contains(script, want) {
			t.Errorf("script missing %q", want)
		}
	}
	if !strings.HasSuffix(script, "kill 99 >/dev/null 2>&1 || true") {
		t.Error("stopping the daemon itself must be the last step")
	}
	if strings.Contains(buildLinuxUninstallScript(0, 99), "kill -0") {
		t.Error("a non-positive app pid must not emit a wait loop")
	}
}
