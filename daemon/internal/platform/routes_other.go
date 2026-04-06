//go:build !windows

package platform

import (
	"context"
)

func RepairNetworkAfterTunnelDisconnect(ctx context.Context, tunnelNames []string) ([]string, error) {
	_ = ctx
	_ = tunnelNames
	return nil, nil
}
