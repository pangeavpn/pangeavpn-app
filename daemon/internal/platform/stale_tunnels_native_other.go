//go:build !windows

package platform

func CleanupStaleTunnelArtifactsNative(_ []string, _ map[uint64]struct{}) ([]string, error) {
	return nil, nil
}
