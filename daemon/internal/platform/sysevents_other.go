//go:build !windows

package platform

import "context"

// WatchSystemEvents has no event sources wired outside Windows yet; a nil
// channel tells the consumer to fall back to timer-based detection.
func WatchSystemEvents(_ context.Context) (<-chan SystemEvent, error) {
	return nil, nil
}
