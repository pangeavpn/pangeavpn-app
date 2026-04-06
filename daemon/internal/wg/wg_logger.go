//go:build darwin || linux || windows

package wg

import (
	"fmt"

	"golang.zx2c4.com/wireguard/device"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// newWGLogger creates a wireguard-go Logger that routes messages into
// the daemon LogStore under the WireGuard source.
func newWGLogger(logs *state.LogStore) *device.Logger {
	return &device.Logger{
		Verbosef: func(format string, args ...any) {
			logs.Add(state.LogDebug, state.SourceWireGuard, fmt.Sprintf(format, args...))
		},
		Errorf: func(format string, args ...any) {
			logs.Add(state.LogError, state.SourceWireGuard, fmt.Sprintf(format, args...))
		},
	}
}
