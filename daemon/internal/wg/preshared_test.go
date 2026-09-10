//go:build darwin || linux || windows

package wg

import "testing"

func TestHasPresharedKey(t *testing.T) {
	tests := []struct {
		name   string
		config string
		want   bool
	}{
		{
			name: "keyed peer",
			config: `[Interface]
PrivateKey = YWJj
Address = 10.7.0.2/32

[Peer]
PublicKey = eHl6
PresharedKey = cHNr
AllowedIPs = 0.0.0.0/0
`,
			want: true,
		},
		{
			name:   "case-insensitive key name",
			config: "[Interface]\nPrivateKey = a\n[Peer]\npresharedkey = b\n",
			want:   true,
		},
		{
			name:   "no peer at all",
			config: "[Interface]\nPrivateKey = a\n",
			want:   false,
		},
		{
			name:   "peer without a key",
			config: "[Interface]\nPrivateKey = a\n[Peer]\nPublicKey = b\n",
			want:   false,
		},
		{
			name:   "empty value counts as absent",
			config: "[Interface]\nPrivateKey = a\n[Peer]\nPresharedKey = \n",
			want:   false,
		},
		{
			name:   "one keyed peer among two is not enough",
			config: "[Interface]\nPrivateKey = a\n[Peer]\nPresharedKey = b\n[Peer]\nPublicKey = c\n",
			want:   false,
		},
		{
			name:   "unparseable config",
			config: "[Bogus]\nx = y\n",
			want:   false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasPresharedKey(tc.config); got != tc.want {
				t.Fatalf("HasPresharedKey = %v, want %v", got, tc.want)
			}
		})
	}
}
