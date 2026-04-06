//go:build !windows

package platform

import "os/exec"

func ConfigureBackgroundProcess(cmd *exec.Cmd) {
	_ = cmd
}
