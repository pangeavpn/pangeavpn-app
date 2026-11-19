//go:build linux && !android

package nicpin

import (
	"context"
	"errors"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

// Under strict rp_filter the replies to a marked socket are dropped, so the pin must fail
// fast instead of leaving the CDN transport to time out.
func TestAcquire_LinuxStrictReversePathFailsFast(t *testing.T) {
	orig := strictReversePath
	t.Cleanup(func() { strictReversePath = orig })

	strictReversePath = func() (bool, error) { return true, nil }
	if _, err := Acquire(context.Background(), "104.21.24.200"); !errors.Is(err, errStrictReversePath) {
		t.Fatalf("Acquire() = %v, want the strict rp_filter refusal", err)
	}

	strictReversePath = func() (bool, error) { return false, nil }
	pin, err := Acquire(context.Background(), "104.21.24.200")
	if err != nil || pin.Mark != egress.SplitMark || pin.Server != "104.21.24.200" {
		t.Fatalf("Acquire() = %+v, %v; want the split mark on the edge", pin, err)
	}
}
