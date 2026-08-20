//go:build windows

package platform

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RunWindowsBackgroundCommand executes a command without attaching a visible
// console window and returns combined stdout/stderr text.
func RunWindowsBackgroundCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, resolveSystemCommand(command), args...)
	ConfigureBackgroundProcess(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	combined := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	return combined, err
}

// resolveSystemCommand pins bare elevated-helper names to an absolute path
// under %SystemRoot%\System32 so a writable PATH entry can't hijack SYSTEM execution.
func resolveSystemCommand(name string) string {
	if filepath.IsAbs(name) {
		return name
	}

	root := os.Getenv("SystemRoot")
	if root == "" {
		root = `C:\Windows`
	}

	base := name
	if filepath.Ext(base) == "" {
		base += ".exe"
	}
	if strings.EqualFold(base, "powershell.exe") {
		return filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	}
	return filepath.Join(root, "System32", base)
}
