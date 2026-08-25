//go:build !windows && !darwin && (!linux || android)

package platform

import "context"

// WatchSystemEvents has no event sources wired on android; a nil
// channel tells the consumer to fall back to timer-based detection.
func WatchSystemEvents(_ context.Context) (<-chan SystemEvent, error) {
	return nil, nil
}
