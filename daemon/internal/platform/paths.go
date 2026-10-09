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

// Traversal only. Each file inside carries its own mode, so the directory
// grants the path walk the desktop app needs without granting a listing.
const dirTraverseBits = 0o011
const dataDirMode = 0o711

func AppSupportDir() (string, error) {
	privileged := os.Geteuid() == 0

	// Honoured under root too: the launchd plist points the daemon at the
	// installer's directory, and ensureDataDir refuses one root does not own.
	if override := strings.TrimSpace(os.Getenv(appSupportDirOverrideEnv)); override != "" {
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

// ensureDataDir creates path (and any missing parents) traversable, or, if it
// already exists, refuses a symlink and — when running privileged — hard-fails
// unless it is uid-0 owned, repairing a bad mode in place.
func ensureDataDir(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(path, dataDirMode); err != nil {
			return err
		}
		// MkdirAll applies the umask, which can strip the traversal bits.
		return os.Chmod(path, dataDirMode)
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
	return repairDirMode(path, info.Mode().Perm())
}

// repairDirMode drops write access, and restores the traversal a 0700 dir left
// by an older build denies: without it the app cannot reach the token inside.
func repairDirMode(path string, perm os.FileMode) error {
	want := (perm &^ 0o022) | dirTraverseBits
	if want == perm {
		return nil
	}
	if err := os.Chmod(path, want); err != nil {
		return fmt.Errorf("repair permissions on %s: %w", path, err)
	}
	return nil
}
