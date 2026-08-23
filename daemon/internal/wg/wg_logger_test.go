//go:build darwin || linux || windows

package wg

import (
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// wireguard-go calls device.log.Verbosef unconditionally — a nil func there
// segfaults the daemon the moment a device's workers start.
func TestNewWGLogger_VerbosefIsAlwaysCallable(t *testing.T) {
	t.Setenv("PANGEA_WG_VERBOSE", "")

	logger := newWGLogger(state.NewLogStore(16))
	if logger.Verbosef == nil {
		t.Fatal("Verbosef must never be nil: wireguard-go dereferences it without a nil check")
	}
	if logger.Errorf == nil {
		t.Fatal("Errorf must never be nil")
	}
	logger.Verbosef("routine %d started", 1)
	logger.Errorf("handshake failed: %v", "boom")
}

func TestNewWGLogger_VerboseOptInReachesTheStore(t *testing.T) {
	t.Setenv("PANGEA_WG_VERBOSE", "1")

	logs := state.NewLogStore(16)
	newWGLogger(logs).Verbosef("peer %d handshake", 7)

	entries := logs.Since(0)
	if len(entries) != 1 || entries[0].Msg != "peer 7 handshake" {
		t.Fatalf("verbose line did not reach the log store: %+v", entries)
	}
}

func TestNewWGLogger_VerboseStaysQuietWithoutOptIn(t *testing.T) {
	t.Setenv("PANGEA_WG_VERBOSE", "")

	logs := state.NewLogStore(16)
	newWGLogger(logs).Verbosef("peer %d handshake", 7)

	if entries := logs.Since(0); len(entries) != 0 {
		t.Fatalf("verbose logging must stay off without opt-in, got: %+v", entries)
	}
}
