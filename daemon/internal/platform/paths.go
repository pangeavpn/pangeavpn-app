//go:build !windows

package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const appFolder = "pangeavpn-desktop"
const appSupportDirOverrideEnv = "PANGEA_APP_SUPPORT_DIR"

func AppSupportDir() (string, error) {
	if override := strings.TrimSpace(os.Getenv(appSupportDirOverrideEnv)); override != "" {
		if err := os.MkdirAll(override, 0o755); err != nil {
			return "", fmt.Errorf("create app support dir override: %w", err)
		}
		return override, nil
	}

	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
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
