//go:build !windows

package platform

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
)

const appFolder = "pangeavpn-desktop"
const appSupportDirOverrideEnv = "PANGEA_APP_SUPPORT_DIR"

func AppSupportDir() (string, error) {
	privileged := os.Geteuid() == 0

	// The override only serves unprivileged dev/test runs; honouring it for
	// root would let anything that can set the daemon's environment (sudo
	// preserves HOME by default) relocate the token file and killswitch
	// state to a directory the invoking user controls.
	if override := strings.TrimSpace(os.Getenv(appSupportDirOverrideEnv)); override != "" && !privileged {
		if !filepath.IsAbs(override) {
			return "", fmt.Errorf("%s must be an absolute path", appSupportDirOverrideEnv)
		}
		if err := ensureDataDir(override); err != nil {
			return "", fmt.Errorf("app support dir override: %w", err)
		}
		return override, nil
	}

	appDir, err := systemAppDir(privileged)
	if err != nil {
		return "", err
	}
	if err := ensureDataDir(appDir); err != nil {
		return "", fmt.Errorf("create app support dir: %w", err)
	}

	return appDir, nil
}

// systemAppDir resolves the root daemon to a system-owned location instead of
// deriving from $HOME, which the invoking (unprivileged) user controls.
func systemAppDir(privileged bool) (string, error) {
	if privileged {
		if runtime.GOOS == "darwin" {
			return filepath.Join("/Library/Application Support", appFolder), nil
		}
		return filepath.Join("/var/lib", appFolder), nil
	}

	baseDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	return filepath.Join(baseDir, appFolder), nil
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
	if err := ensureDataDir(dir); err != nil {
		return "", fmt.Errorf("create tunnel config dir: %w", err)
	}

	return dir, nil
}

// ensureDataDir creates path (and any missing parents) mode 0700, or, if it
// already exists, refuses a symlink and — when running privileged — hard-fails
// unless it is uid-0 owned, repairing an overly permissive mode in place.
func ensureDataDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(path, 0o700)
	}
	if err != nil {
		return fmt.Errorf("stat %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink, refusing to use it", path)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	if os.Geteuid() != 0 {
		return nil
	}
	return verifyRootOwnedDir(path, info)
}

func verifyRootOwnedDir(path string, info os.FileInfo) error {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return fmt.Errorf("cannot determine ownership of %s", path)
	}
	if stat.Uid != 0 {
		return fmt.Errorf("%s is not owned by root (uid %d)", path, stat.Uid)
	}
	if info.Mode().Perm()&0o077 != 0 {
		if err := os.Chmod(path, 0o700); err != nil {
			return fmt.Errorf("tighten permissions on %s: %w", path, err)
		}
	}
	return nil
}
