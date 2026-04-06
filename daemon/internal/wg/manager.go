package wg

import (
	"context"
	"errors"
	"fmt"
	"runtime"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

type Manager interface {
	Start(ctx context.Context, profile state.WireGuardProfile) error
	Stop(ctx context.Context, profile state.WireGuardProfile) error
	Status(ctx context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error)

	// ActiveLUIDs returns the set of Windows interface LUIDs for currently
	// active tunnels. On non-Windows platforms this returns an empty map.
	ActiveLUIDs() map[uint64]struct{}
}

// Set by platform-specific files (windows.go, darwin_linux_shared.go).
var newPlatformManager func(logs *state.LogStore) Manager

func NewManager(logs *state.LogStore) Manager {
	if newPlatformManager != nil {
		return newPlatformManager(logs)
	}
	return &unsupportedManager{logs: logs}
}

type unsupportedManager struct {
	logs *state.LogStore
}

func (m *unsupportedManager) Start(ctx context.Context, profile state.WireGuardProfile) error {
	_ = ctx
	_ = profile

	message := fmt.Sprintf("wireguard backend is not implemented for %s", runtime.GOOS)
	m.logs.Add(state.LogWarn, state.SourceWireGuard, message)
	return errors.New(message)
}

func (m *unsupportedManager) Stop(ctx context.Context, profile state.WireGuardProfile) error {
	_ = ctx
	_ = profile
	return nil
}

func (m *unsupportedManager) Status(ctx context.Context, profile state.WireGuardProfile) (state.WireGuardStatus, error) {
	_ = ctx
	_ = profile
	return state.WireGuardStatus{
		Running: false,
		Detail:  "wireguard backend not implemented on this OS",
	}, nil
}

func (m *unsupportedManager) ActiveLUIDs() map[uint64]struct{} {
	return map[uint64]struct{}{}
}
