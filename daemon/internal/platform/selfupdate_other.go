//go:build !darwin

package platform

import "errors"

func InstallUpdatePackage(_, _ string) error {
	return errors.New("in-app updates are only supported on macOS")
}
