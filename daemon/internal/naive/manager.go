//go:build naive_cgo

package naive

/*
#cgo LDFLAGS: -lpangea_naive
#include <stdlib.h>
#include "pangea_naive_capi.h"
*/
import "C"

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"unsafe"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

// Manager wraps the cgo-linked NaiveProxy engine (github.com/pangeavpn/naiveproxy,
// branch feature/pangea-static-lib) behind the transport.Manager shape, mirroring
// cloak.inProcessManager's structure: a loopback UDP listener WireGuard's peer
// Endpoint points at, owned entirely on the Go side; the engine itself only
// exposes lifecycle control (Start/Stop/Status) across the cgo boundary plus a
// local SOCKS5 listener that this package's framing.go dials through.
// Compile-time check: Manager satisfies transport.Manager (Stop only —
// Start and Status below are naive's own concretely-typed extensions, per
// Task 2's note on why they aren't part of the shared interface).
var _ transport.Manager = (*Manager)(nil)

type Manager struct {
	mu      sync.RWMutex
	running bool
}

func NewManager() *Manager {
	return &Manager{}
}

type startConfig struct {
	RemoteHost string `json:"remoteHost"`
	RemotePort int    `json:"remotePort"`
	Username   string `json:"username"`
	Password   string `json:"password"`
	ServerName string `json:"serverName"`
}

// Start begins with the engine's config JSON only — the loopback UDP
// listener + framing goroutine that bridges WireGuard traffic through the
// engine's local SOCKS5 port is wired up in a follow-on task once Task 1's
// cgo spike confirms the link approach and the real capi header exists.
func (m *Manager) Start(ctx context.Context, profile state.NaiveProfile) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.running {
		return nil
	}

	cfg := startConfig{
		RemoteHost: profile.RemoteHost,
		RemotePort: profile.RemotePort,
		Username:   profile.Username,
		Password:   profile.Password,
		ServerName: profile.ServerName,
	}
	payload, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("naive: marshal start config: %w", err)
	}

	cPayload := C.CString(string(payload))
	defer C.free(unsafe.Pointer(cPayload))

	if C.PangeaNaiveStart(cPayload) != 0 {
		return errors.New("naive: engine failed to start")
	}
	m.running = true
	return nil
}

func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.running {
		return nil
	}
	C.PangeaNaiveStop()
	m.running = false
	return nil
}

func (m *Manager) Status() state.TransportStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return state.TransportStatus{Running: m.running}
}
