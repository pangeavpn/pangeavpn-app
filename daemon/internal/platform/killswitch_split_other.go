//go:build !darwin

package platform

import "errors"

var errSplitEgressGroupUnsupported = errors.New("the split-egress group exists only on macOS")

// SplitEgressGroupID is macOS-only: Linux marks bypass sockets and Windows
// permits the daemon image instead.
func SplitEgressGroupID() (int, error) {
	return 0, errSplitEgressGroupUnsupported
}
