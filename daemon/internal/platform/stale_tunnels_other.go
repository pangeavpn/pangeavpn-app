//go:build !windows

package platform

import "context"

func CleanupStaleTunnelArtifacts(_ context.Context, _ []string) ([]string, error) {
	return nil, nil
}
