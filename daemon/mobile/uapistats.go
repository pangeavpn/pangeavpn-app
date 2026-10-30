package mobile

// WireGuard UAPI text: the single-peer config Start sets and the counters
// State reads back.

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
)

// parseTunnelStats sums rx/tx across peers and keeps the newest handshake.
func parseTunnelStats(uapi string) (rx, tx, lastHandshake int64) {
	for _, line := range strings.Split(uapi, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 0 {
			continue
		}
		switch key {
		case "rx_bytes":
			rx += n
		case "tx_bytes":
			tx += n
		case "last_handshake_time_sec":
			lastHandshake = max(lastHandshake, n)
		}
	}
	return rx, tx, lastHandshake
}

// buildUAPI renders the fixed single-peer config in UAPI's hex keys. The
// pre-shared key is a peer attribute, so it must follow that peer's public_key.
func buildUAPI(privRaw, pubRaw, pskRaw []byte, endpointPort int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "private_key=%s\n", hex.EncodeToString(privRaw))
	fmt.Fprintf(&b, "public_key=%s\n", hex.EncodeToString(pubRaw))
	if len(pskRaw) == pq.PresharedKeySize {
		fmt.Fprintf(&b, "preshared_key=%s\n", hex.EncodeToString(pskRaw))
	}
	fmt.Fprintf(&b, "endpoint=127.0.0.1:%d\n", endpointPort)
	fmt.Fprintf(&b, "persistent_keepalive_interval=25\n")
	fmt.Fprintf(&b, "allowed_ip=0.0.0.0/0\n")
	return b.String()
}
