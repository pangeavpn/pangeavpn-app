//go:build !windows

package platform

import "context"

func KillUDPPortOwners(ctx context.Context, port int, excludePIDs []int) ([]int, error) {
	_ = ctx
	_ = port
	_ = excludePIDs
	return nil, nil
}

func UDPPortOwners(ctx context.Context, port int, excludePIDs []int) ([]int, error) {
	_ = ctx
	_ = port
	_ = excludePIDs
	return nil, nil
}
