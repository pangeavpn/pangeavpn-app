package api

import (
	"context"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// ArmBootLock re-applies the lock the previous boot left, from disk alone: it
// runs before the network is up, so nothing here may resolve a name.
func ArmBootLock(ctx context.Context, ks platform.KillSwitch) error {
	persisted, err := loadKillSwitchState()
	if err != nil {
		return fmt.Errorf("arm boot lock: %w", err)
	}
	if !persisted.Active {
		return nil
	}
	// An older version's endpoints may hold shared CDN addresses nothing here can vouch for,
	// so that lock arms with no permits until the daemon reconciles it.
	endpoints := persisted.EndpointIPs
	if persisted.Version < platform.KillSwitchStateVersion {
		endpoints = nil
	}
	if err := ks.Enable(ctx, endpoints, persisted.AllowLAN, persisted.Locked); err != nil {
		return fmt.Errorf("arm boot lock: %w", err)
	}
	return nil
}
