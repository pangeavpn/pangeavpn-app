package platform

import "path/filepath"

// TransportMemoryPath is where the daemon persists the per-network map of the
// transport that last established a tunnel, so auto-connect can try it first on
// a familiar network. Lives alongside config.json in the app support dir.
func TransportMemoryPath() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "transport-memory.json"), nil
}
