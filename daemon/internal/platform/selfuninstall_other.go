//go:build !windows && !darwin && !linux

package platform

import "errors"

func SpawnSelfUninstall(_ int) error {
	return errors.New("self-uninstall is not supported on this platform")
}
