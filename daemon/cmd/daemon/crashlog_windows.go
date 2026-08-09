//go:build windows

package main

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// redirectStderr points the process's STD_ERROR_HANDLE at path. The Go runtime
// resolves that handle on every panic write, so fatal crashes land on disk.
func redirectStderr(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open crash log %s: %w", path, err)
	}

	if err := windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd())); err != nil {
		f.Close()
		return nil, fmt.Errorf("redirect stderr to %s: %w", path, err)
	}

	os.Stderr = f
	return f, nil
}
