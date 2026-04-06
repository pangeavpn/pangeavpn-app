//go:build windows

package auth

import (
	"fmt"
	"os/exec"
	"strings"
)

func ensureTokenReadACL(path string) error {
	cmd := exec.Command(
		"icacls",
		path,
		"/inheritance:r",
		"/grant:r",
		"SYSTEM:(F)",
		"Administrators:(F)",
		"Users:(R)",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("set token ACL: %w (%s)", err, strings.TrimSpace(string(output)))
	}
	return nil
}

func shouldIgnoreTokenACLError(err error) bool {
	if err == nil {
		return false
	}

	message := strings.ToLower(err.Error())
	return strings.Contains(message, "access is denied") ||
		strings.Contains(message, "privilege not held") ||
		strings.Contains(message, "required privilege is not held")
}
