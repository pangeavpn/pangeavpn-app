//go:build linux

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

// Dup3 rather than Dup2: linux/arm64 has no dup2 syscall.
func dupOverStderr(fd int) error {
	return unix.Dup3(fd, int(os.Stderr.Fd()), 0)
}
