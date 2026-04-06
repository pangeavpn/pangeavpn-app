//go:build windows

package platform

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
)

// RunWindowsBackgroundCommand executes a command without attaching a visible
// console window and returns combined stdout/stderr text.
func RunWindowsBackgroundCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	ConfigureBackgroundProcess(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	return combined, err
}
