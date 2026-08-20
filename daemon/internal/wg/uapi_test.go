//go:build darwin || linux || windows

package wg

import (
	"strings"
	"testing"
)

func TestWgConfigToUAPI(t *testing.T) {
	tests := []struct {
		name      string
		config    string
		wantErr   bool
		wantLines []string
		notLines  []string
	}{
		{
			name: "allowedips before publickey still leads with public_key",
			config: `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
AllowedIPs = 10.7.0.1/32
Endpoint = 127.0.0.1:51820
PublicKey = eHl6MTIzNDU=
`,
			wantLines: []string{"public_key=", "allowed_ip=10.7.0.1/32"},
		},
		{
			name: "bare address allowedips defaults to /32",
			config: `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
PublicKey = eHl6MTIzNDU=
AllowedIPs = 10.7.0.1
`,
			wantLines: []string{"allowed_ip=10.7.0.1/32"},
		},
		{
			name: "persistent keepalive off becomes 0",
			config: `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
PublicKey = eHl6MTIzNDU=
AllowedIPs = 0.0.0.0/0
PersistentKeepalive = off
`,
			wantLines: []string{"persistent_keepalive_interval=0"},
		},
		{
			name: "dual stack allowedips keeps ipv4 and drops ipv6",
			config: `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
PublicKey = eHl6MTIzNDU=
AllowedIPs = 0.0.0.0/0, ::/0
`,
			wantLines: []string{"allowed_ip=0.0.0.0/0"},
			notLines:  []string{"::"},
		},
		{
			name: "missing private key errors",
			config: `[Interface]
ListenPort = 51820

[Peer]
PublicKey = eHl6MTIzNDU=
AllowedIPs = 0.0.0.0/0
`,
			wantErr: true,
		},
		{
			name: "missing peer public key errors",
			config: `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
AllowedIPs = 0.0.0.0/0
`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uapi, err := wgConfigToUAPI(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got uapi:\n%s", uapi)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for _, want := range tt.wantLines {
				if !strings.Contains(uapi, want) {
					t.Errorf("expected output to contain %q, got:\n%s", want, uapi)
				}
			}
			for _, not := range tt.notLines {
				if strings.Contains(uapi, not) {
					t.Errorf("expected output not to contain %q, got:\n%s", not, uapi)
				}
			}
		})
	}
}

func TestWgConfigToUAPI_PublicKeyPrecedesAllowedIP(t *testing.T) {
	config := `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
AllowedIPs = 10.7.0.1/32
PublicKey = eHl6MTIzNDU=
`
	uapi, err := wgConfigToUAPI(config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	pkIdx := strings.Index(uapi, "public_key=")
	allowedIdx := strings.Index(uapi, "allowed_ip=")
	if pkIdx < 0 || allowedIdx < 0 {
		t.Fatalf("expected both public_key= and allowed_ip= in output:\n%s", uapi)
	}
	if pkIdx > allowedIdx {
		t.Errorf("expected public_key= to precede allowed_ip=, got:\n%s", uapi)
	}
}

func TestExtractAllowedIPsFromConfig(t *testing.T) {
	tests := []struct {
		name    string
		config  string
		want    []string
		wantErr bool
	}{
		{
			name: "bare address defaults to /32",
			config: `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
PublicKey = eHl6MTIzNDU=
AllowedIPs = 10.7.0.1
`,
			want: []string{"10.7.0.1/32"},
		},
		{
			name: "dual stack keeps only ipv4",
			config: `[Interface]
PrivateKey = YWJjZGVmZw==

[Peer]
PublicKey = eHl6MTIzNDU=
AllowedIPs = 0.0.0.0/0, ::/0
`,
			want: []string{"0.0.0.0/0"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := extractAllowedIPsFromConfig(tt.config)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("got %v, want %v", got, tt.want)
				}
			}
		})
	}
}
