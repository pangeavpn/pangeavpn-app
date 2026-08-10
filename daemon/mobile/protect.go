//go:build android

package mobile

// Every socket that must leave the device outside the tunnel has to go through
// VpnService.protect(), or the TUN's default route captures the very traffic
// the transport exists to carry. Cloak takes a Go callback directly; the
// sing-box transports take a unix socket path instead, served here.

import (
	"fmt"
	"path/filepath"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/hysteria2"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/protect"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reality"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const protectSocketName = "protect.sock"

var protectServer *protect.Server

// startProtectServer publishes the protect socket and points every sing-box
// transport at it. Failing to open it would leave those transports dialing
// into the tunnel, so the error is surfaced rather than swallowed.
func startProtectServer(filesDir string, logs *state.LogStore) error {
	srv, err := protect.Listen(filepath.Join(filesDir, protectSocketName), protectFD)
	if err != nil {
		logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("socket protection unavailable: %v", err))
		return err
	}

	protectServer = srv
	reality.ProtectPath = srv.Path()
	shadowsocks.ProtectPath = srv.Path()
	hysteria2.ProtectPath = srv.Path()
	return nil
}

// protectFD defers to whatever protector is installed now: Init runs before
// VpnService exists, and the service replaces the protector on every bind.
func protectFD(fd int) bool {
	mu.Lock()
	p := protector
	mu.Unlock()
	if p == nil {
		return false
	}
	return p.Protect(fd)
}
