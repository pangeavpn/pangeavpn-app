package api

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

// The boot unit re-applies the lock before the network comes up; it can only
// use what is on disk, since DNS is both blocked and not yet available.
func TestArmBootLock_ReappliesThePersistedLock(t *testing.T) {
	ks := &fakeKillSwitch{}
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true, AllowLAN: true, EndpointIPs: []string{"198.51.100.9"}})

	if err := ArmBootLock(context.Background(), ks); err != nil {
		t.Fatalf("ArmBootLock() error = %v", err)
	}
	if ks.enableCount != 1 || !slices.Equal(ks.enableEndpoints, []string{"198.51.100.9"}) || !ks.enableLocked || !ks.enableAllowLAN {
		t.Fatalf("Enable calls=%d endpoints=%v locked=%v lan=%v, want one re-arm from the persisted state",
			ks.enableCount, ks.enableEndpoints, ks.enableLocked, ks.enableAllowLAN)
	}
}

func TestArmBootLock_NothingPersistedArmsNothing(t *testing.T) {
	ks := &fakeKillSwitch{}
	stubKillSwitchState(t, platform.KillSwitchState{})

	if err := ArmBootLock(context.Background(), ks); err != nil {
		t.Fatalf("ArmBootLock() error = %v, want a quiet no-op", err)
	}
	if ks.enableCount != 0 {
		t.Fatalf("Enable() called %d times with no lock on disk", ks.enableCount)
	}
}

// An unreadable state file may hide a lock; the unit must fail loudly, not pass.
func TestArmBootLock_UnreadableStateIsAnError(t *testing.T) {
	ks := &fakeKillSwitch{}
	original := loadKillSwitchState
	loadKillSwitchState = func() (platform.KillSwitchState, error) {
		return platform.KillSwitchState{}, platform.ErrKillSwitchStateUnreadable
	}
	t.Cleanup(func() { loadKillSwitchState = original })

	if err := ArmBootLock(context.Background(), ks); !errors.Is(err, platform.ErrKillSwitchStateUnreadable) {
		t.Fatalf("ArmBootLock() error = %v, want the unreadable-state error surfaced", err)
	}
	if ks.enableCount != 0 {
		t.Fatalf("Enable() called %d times on a state it could not read", ks.enableCount)
	}
}
