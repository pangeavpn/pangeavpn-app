//go:build android && (!arm64 || !naive_cgo)

package mobile

import (
	"context"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// Only arm64-v8a ships libpangea_naive.a; on the other ABIs the cascade skips
// naive and ends at Hysteria2.
const naiveLinked = false

func bindNaiveProtector() {}

func (s *starter) startNaive(context.Context, *state.Profile, *state.WireGuardProfile) error {
	panic("naive is not linked into this build")
}
