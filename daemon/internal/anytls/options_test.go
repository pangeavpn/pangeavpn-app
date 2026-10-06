package anytls

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// testPin is a well-formed SPKI SHA-256 pin: base64 of exactly 32 bytes.
var testPin = base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef"))

func validProfile() state.AnyTLSProfile {
	return state.AnyTLSProfile{
		RemoteHost: "203.0.113.10",
		RemotePort: 8443,
		Password:   "hunter2",
		ServerName: "node.example.com",
	}
}

func TestValidateProfile(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*state.AnyTLSProfile)
		wantErr string
	}{
		{name: "valid", mutate: func(*state.AnyTLSProfile) {}},
		{name: "dynamic local port", mutate: func(p *state.AnyTLSProfile) { p.LocalPort = 0 }},
		{name: "no server name falls back to the host", mutate: func(p *state.AnyTLSProfile) { p.ServerName = "" }},
		{name: "pin without insecure", mutate: func(p *state.AnyTLSProfile) { p.PinSHA256 = testPin }},
		{
			name:   "insecure with a pin",
			mutate: func(p *state.AnyTLSProfile) { p.Insecure, p.PinSHA256 = true, testPin },
		},
		{
			name:    "missing remote host",
			mutate:  func(p *state.AnyTLSProfile) { p.RemoteHost = "  " },
			wantErr: "remoteHost is required",
		},
		{
			name:    "zero remote port",
			mutate:  func(p *state.AnyTLSProfile) { p.RemotePort = 0 },
			wantErr: "remotePort must be > 0",
		},
		{
			name:    "oversized remote port",
			mutate:  func(p *state.AnyTLSProfile) { p.RemotePort = 70000 },
			wantErr: "remotePort must be > 0",
		},
		{
			name:    "missing password",
			mutate:  func(p *state.AnyTLSProfile) { p.Password = "" },
			wantErr: "password is required",
		},
		{
			name:    "negative local port",
			mutate:  func(p *state.AnyTLSProfile) { p.LocalPort = -1 },
			wantErr: "localPort must be >= 0",
		},
		{
			name:    "negative target port",
			mutate:  func(p *state.AnyTLSProfile) { p.TargetPort = -1 },
			wantErr: "targetPort must be >= 0",
		},
		{
			// Insecure alone would let any on-path box terminate the TLS session.
			name:    "insecure without a pin",
			mutate:  func(p *state.AnyTLSProfile) { p.Insecure = true },
			wantErr: "insecure requires pinSha256",
		},
		{
			name:    "pin is not base64",
			mutate:  func(p *state.AnyTLSProfile) { p.PinSHA256 = "not*base64" },
			wantErr: "not standard base64",
		},
		{
			name:    "pin is the wrong length",
			mutate:  func(p *state.AnyTLSProfile) { p.PinSHA256 = base64.StdEncoding.EncodeToString([]byte("short")) },
			wantErr: "need exactly 32",
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

func TestBuildOutboundOptionsPropagatesProfileFields(t *testing.T) {
	profile := validProfile()
	profile.PinSHA256 = testPin
	profile.Insecure = true

	opts, err := buildOutboundOptions(profile)
	if err != nil {
		t.Fatalf("buildOutboundOptions: %v", err)
	}
	if opts.Server != "203.0.113.10" || opts.ServerPort != 8443 {
		t.Fatalf("ServerOptions = %s:%d, want 203.0.113.10:8443", opts.Server, opts.ServerPort)
	}
	if opts.Password != "hunter2" {
		t.Fatalf("Password = %q, want hunter2", opts.Password)
	}
	if opts.ClientMetadata != clientMetadata {
		t.Fatalf("ClientMetadata = %q, want %q: the session must name this client, not sing-box's blank default", opts.ClientMetadata, clientMetadata)
	}
	if opts.TLS == nil || !opts.TLS.Enabled {
		t.Fatal("TLS must be enabled: the anytls outbound refuses to build without it")
	}
	if opts.TLS.ServerName != "node.example.com" {
		t.Fatalf("TLS.ServerName = %q, want node.example.com", opts.TLS.ServerName)
	}
	if !opts.TLS.Insecure {
		t.Fatal("TLS.Insecure = false, want the profile's true")
	}
	if len(opts.TLS.CertificatePublicKeySHA256) != 1 || len(opts.TLS.CertificatePublicKeySHA256[0]) != 32 {
		t.Fatalf("CertificatePublicKeySHA256 = %v, want one 32-byte pin", opts.TLS.CertificatePublicKeySHA256)
	}
	if (opts.TLS.UTLS != nil && opts.TLS.UTLS.Enabled) != utlsAvailable {
		t.Fatalf("UTLS enabled = %v, want %v (tracks the with_utls build tag)", opts.TLS.UTLS != nil && opts.TLS.UTLS.Enabled, utlsAvailable)
	}
	if opts.TCPFastOpen {
		t.Fatal("TCPFastOpen must stay off: the anytls outbound rejects it")
	}
}

func TestBuildOutboundOptionsDefaultsServerNameToHost(t *testing.T) {
	profile := validProfile()
	profile.ServerName = " "
	opts, err := buildOutboundOptions(profile)
	if err != nil {
		t.Fatalf("buildOutboundOptions: %v", err)
	}
	if opts.TLS.ServerName != "203.0.113.10" {
		t.Fatalf("TLS.ServerName = %q, want the remote host", opts.TLS.ServerName)
	}
	if len(opts.TLS.CertificatePublicKeySHA256) != 0 {
		t.Fatalf("CertificatePublicKeySHA256 = %v, want none without a pin", opts.TLS.CertificatePublicKeySHA256)
	}
}

func TestTargetDefaults(t *testing.T) {
	if got := targetHostOrDefault(" "); got != "127.0.0.1" {
		t.Fatalf("targetHostOrDefault(blank) = %q, want 127.0.0.1", got)
	}
	if got := targetHostOrDefault("10.0.0.2"); got != "10.0.0.2" {
		t.Fatalf("targetHostOrDefault(set) = %q, want 10.0.0.2", got)
	}
	if got := targetPortOrDefault(0); got != state.DefaultWireGuardPort {
		t.Fatalf("targetPortOrDefault(0) = %d, want %d", got, state.DefaultWireGuardPort)
	}
	if got := targetPortOrDefault(51831); got != 51831 {
		t.Fatalf("targetPortOrDefault(set) = %d, want 51831", got)
	}
}
