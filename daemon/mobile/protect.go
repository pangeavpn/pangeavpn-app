//go:build android

package mobile

// Sockets leaving outside the tunnel need VpnService.protect(). Cloak takes a
// Go callback; the sing-box transports take the unix socket served here.

import (
	"fmt"
	"path/filepath"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/anytls"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/hysteria2"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/protect"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/reality"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const protectSocketName = "protect.sock"

var protectServer *protect.Server

// startProtectServer points every sing-box transport at the protect socket. A
// failure is surfaced: those transports would otherwise dial into the tunnel.
func startProtectServer(filesDir string, logs *state.LogStore) error {
	srv, err := protect.Listen(filepath.Join(filesDir, protectSocketName), protectFD)
	if err != nil {
		logs.Add(state.LogError, state.SourceDaemon, fmt.Sprintf("socket protection unavailable: %v", err))
		return err
	}

	protectServer = srv
	bindNaiveProtector()
	reality.ProtectPath = srv.Path()
	shadowsocks.ProtectPath = srv.Path()
	hysteria2.ProtectPath = srv.Path()
	anytls.ProtectPath = srv.Path()
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
