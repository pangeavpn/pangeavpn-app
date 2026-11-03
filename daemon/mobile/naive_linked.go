//go:build android && arm64 && naive_cgo

package mobile

import (
	"context"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/naive"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// naiveLinked reports whether this ABI carries the Chromium engine; only
// arm64-v8a ships libpangea_naive.a.
const naiveLinked = true

// bindNaiveProtector hands the engine the same protect path Cloak uses. It is
// a package-level global there because the C hook cannot carry a receiver.
func bindNaiveProtector() {
	naive.ProtectFD = protectFD
}

func (s *starter) startNaive(ctx context.Context, profile *state.Profile, _ *state.WireGuardProfile) error {
	mgr := naive.NewManager(s.logs)
	start := *profile.Naive
	start.LocalPort = 0
	if err := mgr.Start(ctx, start); err != nil {
		return fmt.Errorf("naive start: %w", err)
	}
	if err := awaitSession(ctx, mgr); err != nil {
		_ = mgr.Stop(context.Background())
		return fmt.Errorf("naive session: %w", err)
	}
	s.record("naive", mgr, profile.Naive.LocalPort)
	return nil
}
