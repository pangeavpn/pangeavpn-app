//go:build linux

package platform

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// Detached cleanup under the daemon's root systemd token — no auth prompt.
func SpawnSelfUninstall(appPID int) error {
	cmd := exec.Command("/bin/sh", "-c", buildLinuxUninstallScript(appPID))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn self-uninstall: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func buildLinuxUninstallScript(appPID int) string {
	wait := ""
	if appPID > 0 {
		wait = fmt.Sprintf("n=0; while kill -0 %d 2>/dev/null && [ $n -lt 60 ]; do sleep 0.5; n=$((n+1)); done", appPID)
	}
	return strings.Join([]string{
		wait,
		"systemctl disable pangea-killswitch-boot >/dev/null 2>&1 || true",
		"rm -f /etc/systemd/system/pangea-daemon.service /etc/systemd/system/pangea-killswitch-boot.service >/dev/null 2>&1 || true",
		"systemctl daemon-reload >/dev/null 2>&1 || true",
		"rm -f /usr/local/bin/pangea-daemon >/dev/null 2>&1 || true",
		"rm -rf /etc/pangeavpn /opt/PangeaVPN >/dev/null 2>&1 || true",
		"rm -f /usr/share/applications/pangeavpn.desktop /usr/share/icons/hicolor/256x256/apps/pangeavpn.png >/dev/null 2>&1 || true",
		"groupdel pangeavpn >/dev/null 2>&1 || true",
		"systemctl stop pangea-daemon >/dev/null 2>&1 || true",
	}, "; ")
}
