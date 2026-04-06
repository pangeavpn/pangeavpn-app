package wg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoExecCommand ensures that neither the wg nor cloak packages shell out
// to external processes. All tunnel operations must be in-process.
func TestNoExecCommand(t *testing.T) {
	// Walk upward from the wg package to daemon/internal.
	internalDir := filepath.Join("..", "..")
	dirs := []string{
		filepath.Join(internalDir, "internal", "wg"),
		filepath.Join(internalDir, "internal", "cloak"),
	}

	forbidden := []string{"exec.Command", "exec.CommandContext"}

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			// cloak dir may not exist yet on all branches.
			continue
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
				continue
			}
			// Skip test files.
			if strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			// Skip netconf files — platform network configuration
			// (addresses, routes, DNS) requires system commands and is
			// distinct from in-process tunnel operations.
			if strings.HasPrefix(entry.Name(), "netconf_") {
				continue
			}
			path := filepath.Join(dir, entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				t.Errorf("read %s: %v", path, err)
				continue
			}
			content := string(data)
			for _, pat := range forbidden {
				if strings.Contains(content, pat) {
					t.Errorf("%s contains %q — tunnel packages must not shell out to external processes", entry.Name(), pat)
				}
			}
		}
	}
}
