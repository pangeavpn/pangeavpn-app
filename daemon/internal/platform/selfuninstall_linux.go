//go:build linux

package platform

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
)

// Detached cleanup under the daemon's root systemd token — no auth prompt.
func SpawnSelfUninstall(appPID int) error {
	cmd := exec.Command("/bin/sh", "-c", buildLinuxUninstallScript(appPID, os.Getpid()))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn self-uninstall: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func buildLinuxUninstallScript(appPID, daemonPID int) string {
	wait := ""
	if appPID > 0 {
		wait = fmt.Sprintf("n=0; while kill -0 %d 2>/dev/null && [ $n -lt 60 ]; do sleep 0.5; n=$((n+1)); done", appPID)
	}
	return strings.Join([]string{
		wait,
		"systemctl disable pangea-daemon pangea-killswitch-boot >/dev/null 2>&1 || true",
		"rm -f /etc/systemd/system/pangea-daemon.service /etc/systemd/system/pangea-killswitch-boot.service >/dev/null 2>&1 || true",
		"systemctl daemon-reload >/dev/null 2>&1 || true",
		// dpkg's own removal also drops the deb's /usr/bin alternative and AppArmor profile.
		"if command -v dpkg >/dev/null 2>&1 && dpkg -s pangeavpn >/dev/null 2>&1; then dpkg --purge pangeavpn >/dev/null 2>&1 || true; fi",
		"rm -f /usr/local/bin/pangea-daemon >/dev/null 2>&1 || true",
		"rm -rf /etc/pangeavpn /opt/PangeaVPN >/dev/null 2>&1 || true",
		"rm -f /usr/share/applications/pangeavpn.desktop /usr/share/icons/hicolor/256x256/apps/pangeavpn.png >/dev/null 2>&1 || true",
		"groupdel pangeavpn >/dev/null 2>&1 || true",
		"systemctl stop pangea-daemon >/dev/null 2>&1 || true",
		// Only reached without systemd (a pkexec-launched daemon): stop it directly.
		fmt.Sprintf("kill %d >/dev/null 2>&1 || true", daemonPID),
	}, "; ")
}
