package mobile

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseTunnelStatsSumsPeersAndKeepsTheNewestHandshake(t *testing.T) {
	tests := []struct {
		name          string
		uapi          string
		rx, tx, shake int64
	}{
		{"two peers", strings.Join([]string{
			"private_key=aa", "public_key=bb", "last_handshake_time_sec=100", "rx_bytes=1", "tx_bytes=3",
			"public_key=cc", "last_handshake_time_sec=250", "rx_bytes=2", "tx_bytes=4", "errno=0", "",
		}, "\n"), 3, 7, 250},
		{"empty", "", 0, 0, 0},
		{"garbage values", "rx_bytes=x\ntx_bytes=-5\nlast_handshake_time_sec=\nrx_bytes=9\nnoequals", 9, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rx, tx, shake := parseTunnelStats(tt.uapi)
			if rx != tt.rx || tx != tt.tx || shake != tt.shake {
				t.Fatalf("got (%d, %d, %d), want (%d, %d, %d)", rx, tx, shake, tt.rx, tt.tx, tt.shake)
			}
		})
	}
}

func TestBuildUAPIAddsThePresharedKeyAfterThePeer(t *testing.T) {
	psk := bytes.Repeat([]byte{0xab}, 32)
	lines := strings.Split(buildUAPI([]byte{1}, []byte{2}, psk, 4242), "\n")

	peer, keyed := -1, -1
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "public_key="):
			peer = i
		case strings.HasPrefix(line, "preshared_key="):
			keyed = i
			if hex := strings.TrimPrefix(line, "preshared_key="); hex != strings.Repeat("ab", 32) {
				t.Fatalf("got preshared_key=%s, want 64 hex chars of the key", hex)
			}
		}
	}
	if peer < 0 || keyed != peer+1 {
		t.Fatalf("preshared_key at line %d, want directly after public_key at %d:\n%s", keyed, peer, strings.Join(lines, "\n"))
	}
}

func TestBuildUAPIOmitsThePresharedKeyWhenUnkeyed(t *testing.T) {
	for name, psk := range map[string][]byte{"nil": nil, "wrong size": {1, 2, 3}} {
		t.Run(name, func(t *testing.T) {
			if uapi := buildUAPI([]byte{1}, []byte{2}, psk, 4242); strings.Contains(uapi, "preshared_key") {
				t.Fatalf("unkeyed config carries a pre-shared key:\n%s", uapi)
			}
		})
	}
}
