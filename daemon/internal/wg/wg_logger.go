//go:build darwin || linux || windows

package wg

import (
	"fmt"
	"os"
	"strings"

	"golang.zx2c4.com/wireguard/device"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// newWGLogger routes wireguard-go log output into the daemon LogStore.
// Verbosef is left nil unless PANGEA_WG_VERBOSE opts in, since device.NewDevice
// treats any non-nil Verbosef as "verbose" with no level gate of its own.
func newWGLogger(logs *state.LogStore) *device.Logger {
	logger := &device.Logger{
		Errorf: func(format string, args ...any) {
			logs.Add(state.LogError, state.SourceWireGuard, fmt.Sprintf(format, args...))
		},
	}
	if wgVerboseLoggingEnabled() {
		logger.Verbosef = func(format string, args ...any) {
			logs.Add(state.LogDebug, state.SourceWireGuard, fmt.Sprintf(format, args...))
		}
	}
	return logger
}

func wgVerboseLoggingEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PANGEA_WG_VERBOSE"))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
