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
	if err := ks.Enable(ctx, persisted.EndpointIPs, persisted.AllowLAN, persisted.Locked); err != nil {
		return fmt.Errorf("arm boot lock: %w", err)
	}
	return nil
}
