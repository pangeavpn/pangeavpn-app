//go:build darwin || linux || windows

package wg

import (
	"slices"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestResolvers(t *testing.T) {
	const config = `[Interface]
PrivateKey = YWJjZGVmZw==
Address = 10.7.0.2/32
DNS = 1.1.1.1, 1.0.0.1

[Peer]
PublicKey = eHl6MTIzNDU=
Endpoint = 10.0.0.1:51820
AllowedIPs = 0.0.0.0/0
`

	tests := []struct {
		name    string
		profile state.WireGuardProfile
		want    []string
	}{
		{
			name:    "config DNS",
			profile: state.WireGuardProfile{ConfigText: config},
			want:    []string{"1.1.1.1", "1.0.0.1"},
		},
		{
			name:    "profile overrides append after the config's own",
			profile: state.WireGuardProfile{ConfigText: config, DNS: []string{"9.9.9.9"}},
			want:    []string{"1.1.1.1", "1.0.0.1", "9.9.9.9"},
		},
		{
			name:    "duplicates collapse",
			profile: state.WireGuardProfile{ConfigText: config, DNS: []string{"1.1.1.1"}},
			want:    []string{"1.1.1.1", "1.0.0.1"},
		},
		{
			name:    "no DNS anywhere",
			profile: state.WireGuardProfile{ConfigText: "[Interface]\nPrivateKey = YWJjZGVmZw==\n"},
			want:    []string{},
		},
		{
			// The probe must still have something to aim at when the config is
			// unusable, rather than silently reporting a healthy data path.
			name:    "unparseable config falls back to the profile list",
			profile: state.WireGuardProfile{ConfigText: "\x00not ini", DNS: []string{"9.9.9.9"}},
			want:    []string{"9.9.9.9"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Resolvers(tc.profile)
			if !slices.Equal(got, tc.want) {
				t.Errorf("Resolvers() = %v, want %v", got, tc.want)
			}
		})
	}
}
