//go:build !windows && !darwin && !linux

package main

import "errors"

func dupOverStderr(fd int) error {
	_ = fd
	return errors.New("stderr redirection unsupported on this platform")
}
