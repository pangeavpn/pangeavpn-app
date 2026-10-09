//go:build !windows

package platform

import (
	"context"
	"errors"
)

// ErrUDPPortOwnersUnsupported signals no owner-lookup is implemented on this
// platform; callers must not treat it as "no owner found".
var ErrUDPPortOwnersUnsupported = errors.New("udp port owner lookup unsupported on this platform")

func KillUDPPortOwners(ctx context.Context, port int, excludePIDs []int) ([]int, error) {
	_ = ctx
	_ = port
	_ = excludePIDs
	return nil, ErrUDPPortOwnersUnsupported
}

func UDPPortOwners(ctx context.Context, port int, excludePIDs []int) ([]int, error) {
	_ = ctx
	_ = port
	_ = excludePIDs
	return nil, ErrUDPPortOwnersUnsupported
}
