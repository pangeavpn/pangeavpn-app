//go:build !windows

package api

import (
	"errors"
	"syscall"
)

// isNetworkUnreachable reports a dial that found no route out of the host yet.
func isNetworkUnreachable(err error) bool {
	return errors.Is(err, syscall.ENETUNREACH)
}
