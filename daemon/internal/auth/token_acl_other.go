//go:build !windows

package auth

import (
	"os"
	"path/filepath"
	"strings"
)

func ensureTokenReadACL(path string) error {
	cleanPath := filepath.Clean(path)
	// When the daemon runs as root, the token file must be readable by the
	// non-root Electron front-end.  Apply 0o644 for all known token
	// locations rather than only the system install paths.
	if strings.HasPrefix(cleanPath, "/Library/Application Support/PangeaVPN/") ||
		strings.HasPrefix(cleanPath, "/etc/pangeavpn/") ||
		strings.Contains(cleanPath, "pangeavpn-desktop") {
		if err := os.Chmod(cleanPath, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func shouldIgnoreTokenACLError(err error) bool {
	_ = err
	return false
}
