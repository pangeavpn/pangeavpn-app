//go:build windows

package platform

import (
	"os/exec"
	"syscall"
)

// ConfigureBackgroundProcess keeps spawned helper processes hidden on Windows.
func ConfigureBackgroundProcess(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}

	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow: true,
	}
}
