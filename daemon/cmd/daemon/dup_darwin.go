//go:build darwin

package main

import (
	"os"

	"golang.org/x/sys/unix"
)

func dupOverStderr(fd int) error {
	return unix.Dup2(fd, int(os.Stderr.Fd()))
}
