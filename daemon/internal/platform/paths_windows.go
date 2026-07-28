//go:build windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const appFolder = "PangeaVPN"

// appSupportDirOverrideEnv redirects the daemon's state directory. Present on
// every platform so a test run can never touch a real installation's files —
// without it, `go test ./internal/platform` on a machine with PangeaVPN
// installed rewrites and deletes the live killswitch-state.json, and destroying
// its Locked record makes the next daemon start clear an engaged Lockdown lock
// as stale crash leftover.
const appSupportDirOverrideEnv = "PANGEA_APP_SUPPORT_DIR"

func AppSupportDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv(appSupportDirOverrideEnv)); override != "" {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", fmt.Errorf("create app support dir override: %w", err)
		}
		return override, nil
	}

	baseDir := strings.TrimSpace(os.Getenv("ProgramData"))
	if baseDir == "" {
		baseDir = filepath.Join(`C:\`, "ProgramData")
	}

	appDir := filepath.Join(baseDir, appFolder)
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return "", fmt.Errorf("create app support dir: %w", err)
	}

	return appDir, nil
}

func TokenPath() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "daemon-token.txt"), nil
}

func ConfigPath() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "config.json"), nil
}

func TunnelConfigDir() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}

	dir := filepath.Join(appDir, "wireguard", "tunnels")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create tunnel config dir: %w", err)
	}

	return dir, nil
}
