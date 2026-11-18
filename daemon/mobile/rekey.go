//go:build android || ios

package mobile

import (
	"golang.zx2c4.com/wireguard/device"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// Rekey drops the running tunnel's session keys and sends a fresh handshake. iOS
// pauses Go's monotonic clock in sleep, so WireGuard trusts keys the server has expired.
func Rekey() {
	mu.Lock()
	t, logs := activeTunnel, wgLogs
	mu.Unlock()
	if t == nil || t.dev == nil {
		return
	}
	uapi, err := t.dev.IpcGet()
	if err != nil {
		return
	}
	renewed := 0
	for _, hexKey := range peerKeys(uapi) {
		var pk device.NoisePublicKey
		if pk.FromHex(hexKey) != nil {
			continue
		}
		peer := t.dev.LookupPeer(pk)
		if peer == nil {
			continue
		}
		peer.ExpireCurrentKeypairs()
		_ = peer.SendHandshakeInitiation(false)
		renewed++
	}
	if renewed > 0 {
		logAdd(logs, state.LogInfo, "renewing the WireGuard session")
	}
}
