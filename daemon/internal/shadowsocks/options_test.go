package shadowsocks

import (
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// Base64 of 16 and 32 ASCII bytes: the PSK lengths shadowaead_2022 requires.
const (
	testPSK16 = "MTIzNDU2Nzg5MGFiY2RlZg=="
	testPSK32 = "MTIzNDU2Nzg5MGFiY2RlZjEyMzQ1Njc4OTBhYmNkZWY="
)

func validProfile() state.ShadowsocksProfile {
	return state.ShadowsocksProfile{
		RemoteHost: "ss.example.com",
		RemotePort: 8488,
		Method:     "2022-blake3-aes-128-gcm",
		Password:   testPSK16,
	}
}

func TestValidateProfile(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*state.ShadowsocksProfile)
		wantErr string
	}{
		{name: "valid", mutate: func(*state.ShadowsocksProfile) {}},
		{name: "empty method takes the ss2022 default", mutate: func(p *state.ShadowsocksProfile) { p.Method = "" }},
		{
			name: "aead-2017 method keeps taking a passphrase",
			mutate: func(p *state.ShadowsocksProfile) {
				p.Method, p.Password = "chacha20-ietf-poly1305", "hunter2"
			},
		},
		{
			name: "ss2022 aes-256 takes a 32 byte key",
			mutate: func(p *state.ShadowsocksProfile) {
				p.Method, p.Password = "2022-blake3-aes-256-gcm", testPSK32
			},
		},
		{
			name:   "ss2022 aes accepts an ipsk chain",
			mutate: func(p *state.ShadowsocksProfile) { p.Password = testPSK16 + ":" + testPSK16 },
		},
		{name: "dynamic local port", mutate: func(p *state.ShadowsocksProfile) { p.LocalPort = 0 }},
		{
			name:    "missing remote host",
			mutate:  func(p *state.ShadowsocksProfile) { p.RemoteHost = "  " },
			wantErr: "remoteHost is required",
		},
		{
			name:    "zero remote port",
			mutate:  func(p *state.ShadowsocksProfile) { p.RemotePort = 0 },
			wantErr: "remotePort must be > 0",
		},
		{
			name:    "missing password",
			mutate:  func(p *state.ShadowsocksProfile) { p.Password = "" },
			wantErr: "password is required",
		},
		{
			name:    "negative local port",
			mutate:  func(p *state.ShadowsocksProfile) { p.LocalPort = -1 },
			wantErr: "localPort must be >= 0",
		},
		{
			name:    "negative target port",
			mutate:  func(p *state.ShadowsocksProfile) { p.TargetPort = -1 },
			wantErr: "targetPort must be >= 0",
		},
		{
			name:    "unknown method",
			mutate:  func(p *state.ShadowsocksProfile) { p.Method = "salsa20" },
			wantErr: "is not supported",
		},
		{
			name:    "ss2022 rejects a passphrase",
			mutate:  func(p *state.ShadowsocksProfile) { p.Password = "hunter2!!" },
			wantErr: "not standard base64",
		},
		{
			name:    "ss2022 rejects a short key",
			mutate:  func(p *state.ShadowsocksProfile) { p.Password = "MTIzNA==" },
			wantErr: "decodes to 4 bytes, need exactly 16",
		},
		{
			name:    "ss2022 aes-256 rejects a 16 byte key",
			mutate:  func(p *state.ShadowsocksProfile) { p.Method = "2022-blake3-aes-256-gcm" },
			wantErr: "decodes to 16 bytes, need exactly 32",
		},
		{
			name: "ss2022 chacha20 rejects an ipsk chain",
			mutate: func(p *state.ShadowsocksProfile) {
				p.Method, p.Password = "2022-blake3-chacha20-poly1305", testPSK32+":"+testPSK32
			},
			wantErr: "takes a single key, got 2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			profile := validProfile()
			tt.mutate(&profile)
			err := validateProfile(profile)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("validateProfile() = %v, want nil", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("validateProfile() = %v, want error containing %q", err, tt.wantErr)
			}
		})
	}
}

// The legacy shadowstream ciphers are unauthenticated and trivially probed;
// sing-shadowsocks2 still registers them, so this package has to say no.
func TestValidateProfile_RejectsShadowstreamCiphers(t *testing.T) {
	for _, method := range []string{"aes-128-ctr", "aes-256-cfb", "rc4-md5", "chacha20-ietf", "xchacha20"} {
		t.Run(method, func(t *testing.T) {
			profile := validProfile()
			profile.Method = method
			if err := validateProfile(profile); err == nil {
				t.Fatalf("validateProfile(%q) = nil, want the stream cipher rejected", method)
			}
		})
	}
}

func TestBuildOutboundOptions_Defaults(t *testing.T) {
	profile := validProfile()
	profile.Method = ""
	opts := buildOutboundOptions(profile)

	if opts.Method != defaultMethod {
		t.Errorf("Method = %q, want %q", opts.Method, defaultMethod)
	}
	if opts.Server != "ss.example.com" || opts.ServerPort != 8488 {
		t.Errorf("server = %s:%d, want ss.example.com:8488", opts.Server, opts.ServerPort)
	}
	if opts.Password != testPSK16 {
		t.Errorf("Password = %q, want %q", opts.Password, testPSK16)
	}
	if opts.UDPOverTCP != nil {
		t.Errorf("UDPOverTCP = %+v, want nil when the profile does not ask for it", opts.UDPOverTCP)
	}
	if opts.Multiplex != nil {
		t.Errorf("Multiplex = %+v, want nil: one long-lived WireGuard flow needs no mux", opts.Multiplex)
	}
	if opts.Plugin != "" {
		t.Errorf("Plugin = %q, want empty: SIP003 would spawn an external process", opts.Plugin)
	}
}

func TestBuildOutboundOptions_UDPOverTCP(t *testing.T) {
	profile := validProfile()
	profile.UDPOverTCP = true
	opts := buildOutboundOptions(profile)

	if opts.UDPOverTCP == nil || !opts.UDPOverTCP.Enabled {
		t.Fatalf("UDPOverTCP = %+v, want enabled", opts.UDPOverTCP)
	}
	if opts.Multiplex != nil {
		t.Errorf("Multiplex = %+v, want nil", opts.Multiplex)
	}
}

func TestBuildOutboundOptions_TrimsRemoteHost(t *testing.T) {
	profile := validProfile()
	profile.RemoteHost = "  ss.example.com  "
	if got := buildOutboundOptions(profile).Server; got != "ss.example.com" {
		t.Errorf("Server = %q, want the trimmed host", got)
	}
}

func TestTargetDefaults(t *testing.T) {
	if got := targetHostOrDefault(""); got != defaultTargetHost {
		t.Errorf("targetHostOrDefault(\"\") = %q, want %q", got, defaultTargetHost)
	}
	if got := targetHostOrDefault(" 10.10.1.1 "); got != "10.10.1.1" {
		t.Errorf("targetHostOrDefault() = %q, want 10.10.1.1", got)
	}
	if got := targetPortOrDefault(0); got != defaultTargetPort {
		t.Errorf("targetPortOrDefault(0) = %d, want %d", got, defaultTargetPort)
	}
	if got := targetPortOrDefault(51821); got != 51821 {
		t.Errorf("targetPortOrDefault(51821) = %d, want 51821", got)
	}
}
