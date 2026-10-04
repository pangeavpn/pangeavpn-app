//go:build windows && !(amd64 || arm64)

package platform

import (
	"context"
	"errors"
)

// The WFP layouts are 64-bit only; a build without them must refuse to
// connect rather than run with no lock at all.
var errKillSwitchUnsupportedArch = errors.New("kill switch is not available on this Windows architecture")

func init() {
	newPlatformKillSwitch = func() KillSwitch {
		return &unsupportedKillSwitch{}
	}
}

type unsupportedKillSwitch struct{}

func (u *unsupportedKillSwitch) Enable(context.Context, []string, bool, bool) error {
	return errKillSwitchUnsupportedArch
}
func (u *unsupportedKillSwitch) Update(context.Context, TunnelRef) error {
	return errKillSwitchUnsupportedArch
}
func (u *unsupportedKillSwitch) Clear(context.Context) error { return nil }
func (u *unsupportedKillSwitch) Active() bool                { return false }

func (u *unsupportedKillSwitch) SetSplitEgress(_ context.Context, on bool) error {
	if on {
		return errKillSwitchUnsupportedArch
	}
	return nil
}

func (u *unsupportedKillSwitch) SetSplitCIDRs(_ context.Context, cidrs []string) error {
	if len(cidrs) > 0 {
		return errKillSwitchUnsupportedArch
	}
	return nil
}
