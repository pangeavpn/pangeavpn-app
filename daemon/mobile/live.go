//go:build android || ios

package mobile

// DNS, MTU, routes and excluded apps apply to a running tunnel: the host rebuilds its
// interface from LiveConfig and, on Android, hands the new fd to SwapTUN.

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

var errNoRunningTunnel = errors.New("mobile: no tunnel is running")

// carrierClamp is the MTU TCP must be clamped to when kind carries a TUN of
// mtu bytes, 0 when it fits.
func carrierClamp(kind string, mtu int) int {
	if kind == "shadowsocks" && mtu > shadowsocks.MaxTunnelMTU {
		return shadowsocks.MaxTunnelMTU
	}
	return 0
}

// LiveConfig is the running tunnel's interface under the settings saved now. The host
// applies it at once, so the clamp is refitted to the new MTU here.
func LiveConfig() (string, error) {
	mu.Lock()
	t := activeTunnel
	if t == nil || t.prepared == nil {
		mu.Unlock()
		return "", errNoRunningTunnel
	}
	p := t.prepared
	mtu := tunnelMTU(settings.MTU, preferredTransport)
	cfg := tunnelConfigJSON{
		Address:         p.address,
		PrefixLength:    32,
		DNS:             resolveDNS(p.serverDNS, settings.CustomDNS),
		MTU:             mtu,
		ServerID:        p.serverID,
		ServerName:      p.serverName,
		EntryServerID:   p.entryServerID,
		EntryServerName: p.entryServerName,
	}
	p.mtu = mtu
	if t.clamp != nil {
		t.clamp.SetMTU(carrierClamp(t.activeTransport, mtu))
	}
	logs := wgLogs
	mu.Unlock()

	b, err := json.Marshal(cfg)
	if err != nil {
		return "", fmt.Errorf("marshal tunnel config: %w", err)
	}
	logAdd(logs, state.LogInfo, fmt.Sprintf("applying settings to the running tunnel (MTU %d, %d resolvers)", mtu, len(cfg.DNS)))
	return string(b), nil
}

// SwapTUN moves the running tunnel onto a newly established TUN and owns tunFd either
// way; an error means nothing changed, and the host should reconnect instead.
func SwapTUN(tunFd int) error {
	mu.Lock()
	defer mu.Unlock()
	t := activeTunnel
	if t == nil || t.tun == nil {
		discardTunFD(tunFd)
		return errNoRunningTunnel
	}
	fd, err := adoptTunFD(tunFd)
	if err != nil {
		return fmt.Errorf("adopt tun fd: %w", err)
	}
	// openTUN has already closed the fd when it fails.
	next, err := openTUN(fd)
	if err != nil {
		return fmt.Errorf("create tun: %w", err)
	}
	if err := t.tun.Swap(next); err != nil {
		return err
	}
	logAdd(wgLogs, state.LogInfo, "moved the running tunnel to a new interface")
	return nil
}
