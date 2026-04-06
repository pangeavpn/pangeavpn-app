//go:build !windows

package main

import "fmt"

func shouldRunAsService() bool {
	return false
}

func runService() error {
	return fmt.Errorf("windows service mode is not available on this platform")
}
