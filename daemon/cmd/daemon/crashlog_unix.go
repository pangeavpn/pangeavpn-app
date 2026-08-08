//go:build !windows

package main

import (
	"fmt"
	"os"
)

// redirectStderr dups path over fd 2, which is where the Go runtime writes
// panics. Reassigning os.Stderr alone would not capture them.
func redirectStderr(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open crash log %s: %w", path, err)
	}

	if err := dupOverStderr(int(f.Fd())); err != nil {
		// Nothing written yet, so a close error has no data behind it.
		_ = f.Close()
		return nil, fmt.Errorf("redirect stderr to %s: %w", path, err)
	}

	os.Stderr = f
	return f, nil
}
