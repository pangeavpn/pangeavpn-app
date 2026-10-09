//go:build darwin || linux || windows

package wg

import "strings"

// HasPresharedKey reports whether every [Peer] in the config carries a
// PresharedKey, which is what makes the tunnel post-quantum keyed.
func HasPresharedKey(wgConfig string) bool {
	_, peers, err := parseUAPISource(wgConfig)
	if err != nil || len(peers) == 0 {
		return false
	}
	for _, peer := range peers {
		if !peerHasPresharedKey(peer) {
			return false
		}
	}
	return true
}

func peerHasPresharedKey(peer []kv) bool {
	for _, entry := range peer {
		if strings.EqualFold(entry.key, "PresharedKey") && entry.value != "" {
			return true
		}
	}
	return false
}
