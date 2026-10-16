//go:build darwin

package platform

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// Detached cleanup under the daemon's root launchd token — no auth prompt.
func SpawnSelfUninstall(appPID int) error {
	cmd := exec.Command("/bin/sh", "-c", buildDarwinUninstallScript(appPID))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("spawn self-uninstall: %w", err)
	}
	_ = cmd.Process.Release()
	return nil
}

func buildDarwinUninstallScript(appPID int) string {
	wait := ""
	if appPID > 0 {
		wait = fmt.Sprintf("n=0; while /bin/kill -0 %d 2>/dev/null && [ $n -lt 60 ]; do /bin/sleep 0.5; n=$((n+1)); done", appPID)
	}
	return strings.Join([]string{
		wait,
		"/bin/launchctl bootout system/com.pangea.pangeavpn.pf 2>/dev/null || true",
		"/bin/rm -f /etc/pf.anchors/com.pangeavpn.killswitch 2>/dev/null || true",
		"/sbin/pfctl -f /etc/pf.conf 2>/dev/null || true",
		"/bin/rm -f /Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist /Library/LaunchDaemons/com.pangea.pangeavpn.pf.plist 2>/dev/null || true",
		"/usr/bin/dscl . -delete /Groups/_pangeasplit 2>/dev/null || true",
		"/bin/rm -rf '/Library/Application Support/PangeaVPN' 2>/dev/null || true",
		"/bin/rm -rf /Applications/PangeaVPN.app 2>/dev/null || true",
		"/bin/launchctl bootout system/com.pangea.pangeavpn.daemon 2>/dev/null || true",
	}, "; ")
}
