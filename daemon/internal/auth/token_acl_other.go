//go:build !windows

package auth

import (
	"os"
	"path/filepath"
	"strings"
)

func ensureTokenReadACL(path string) error {
	cleanPath := filepath.Clean(path)
	if strings.HasPrefix(cleanPath, "/Library/Application Support/PangeaVPN/") {
		// 0o644: root owns the file but all local users must be able to read
		// the token so the Electron front-end can authenticate to the daemon.
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
