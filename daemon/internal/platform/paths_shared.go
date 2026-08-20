package platform

import (
	"fmt"
	"path/filepath"
)

// LogDir holds the daemon's on-disk logs. The in-memory ring buffer dies with
// the process, so a crash erases its own evidence unless it is mirrored here.
func LogDir() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(appDir, "logs")
	if err := ensureDataDir(dir); err != nil {
		return "", fmt.Errorf("create log dir: %w", err)
	}
	return dir, nil
}

func LogPath() (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon.log"), nil
}

// CrashLogPath receives the process's raw stderr, which is where the Go runtime
// prints panics and fatal faults. The service discards stderr otherwise.
func CrashLogPath() (string, error) {
	dir, err := LogDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "daemon-crash.log"), nil
}

// TransportMemoryPath persists the per-network last-good transport, so
// auto-connect tries it first on a familiar network.
func TransportMemoryPath() (string, error) {
	appDir, err := AppSupportDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(appDir, "transport-memory.json"), nil
}
