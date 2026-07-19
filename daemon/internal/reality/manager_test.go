package reality

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestGenerateKeyPairProducesDistinctValidKeys(t *testing.T) {
	a, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	b, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	if a.PrivateKey == "" || a.PublicKey == "" {
		t.Fatal("GenerateKeyPair returned empty key material")
	}
	if a.PrivateKey == b.PrivateKey || a.PublicKey == b.PublicKey {
		t.Fatal("GenerateKeyPair returned identical keys across calls")
	}
	// base64.RawURLEncoding of 32 bytes is 43 chars, no padding.
	if len(a.PublicKey) != 43 || strings.ContainsAny(a.PublicKey, "+/=") {
		t.Fatalf("PublicKey %q does not look like base64.RawURLEncoding(32 bytes)", a.PublicKey)
	}
	if len(a.PrivateKey) != 43 || strings.ContainsAny(a.PrivateKey, "+/=") {
		t.Fatalf("PrivateKey %q does not look like base64.RawURLEncoding(32 bytes)", a.PrivateKey)
	}
}

func TestGenerateShortIDLength(t *testing.T) {
	id, err := GenerateShortID(8)
	if err != nil {
		t.Fatalf("GenerateShortID: %v", err)
	}
	if len(id) != 16 {
		t.Fatalf("GenerateShortID(8) = %q, want 16 hex chars", id)
	}

	// Out-of-range requests clamp to the 8-byte max rather than erroring.
	id2, err := GenerateShortID(0)
	if err != nil {
		t.Fatalf("GenerateShortID(0): %v", err)
	}
	if len(id2) != 16 {
		t.Fatalf("GenerateShortID(0) = %q, want 16 hex chars (clamped default)", id2)
	}
}

func TestBuildOutboundOptionsPropagatesProfileFields(t *testing.T) {
	profile := state.RealityProfile{
		UUID:      "9f8f3c1e-1234-4a5b-8c9d-abcdef012345",
		PublicKey: "public-key-value",
		ShortID:   "deadbeef",
		Flow:      "xtls-rprx-vision",
	}
	opts := buildOutboundOptions(profile, "reality.example.com", 8443, "reality.example.com")

	if opts.Server != "reality.example.com" || opts.ServerPort != 8443 {
		t.Fatalf("ServerOptions = %s:%d, want reality.example.com:8443", opts.Server, opts.ServerPort)
	}
	if opts.UUID != profile.UUID {
		t.Fatalf("UUID = %q, want %q", opts.UUID, profile.UUID)
	}
	if opts.Flow != profile.Flow {
		t.Fatalf("Flow = %q, want %q", opts.Flow, profile.Flow)
	}
	if opts.TLS == nil || !opts.TLS.Enabled {
		t.Fatal("expected TLS enabled")
	}
	if opts.TLS.ServerName != "reality.example.com" {
		t.Fatalf("ServerName = %q, want reality.example.com", opts.TLS.ServerName)
	}
	if opts.TLS.UTLS == nil || !opts.TLS.UTLS.Enabled {
		t.Fatal("expected UTLS enabled (required by reality client)")
	}
	if opts.TLS.Reality == nil || !opts.TLS.Reality.Enabled {
		t.Fatal("expected Reality enabled")
	}
	if opts.TLS.Reality.PublicKey != profile.PublicKey {
		t.Fatalf("Reality.PublicKey = %q, want %q", opts.TLS.Reality.PublicKey, profile.PublicKey)
	}
	if opts.TLS.Reality.ShortID != profile.ShortID {
		t.Fatalf("Reality.ShortID = %q, want %q", opts.TLS.Reality.ShortID, profile.ShortID)
	}
}

func TestStartRejectsIncompleteProfile(t *testing.T) {
	logs := state.NewLogStore(16)
	cases := []struct {
		name    string
		profile state.RealityProfile
	}{
		{"missing remote host", state.RealityProfile{UUID: "u", PublicKey: "k"}},
		{"missing uuid", state.RealityProfile{RemoteHost: "h", PublicKey: "k"}},
		{"missing public key", state.RealityProfile{RemoteHost: "h", UUID: "u"}},
		{"negative local port", state.RealityProfile{RemoteHost: "h", UUID: "u", PublicKey: "k", LocalPort: -1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewManager(logs)
			if err := m.Start(context.Background(), tc.profile); err == nil {
				t.Fatal("expected validation error, got nil")
			}
			if m.Status().Running {
				t.Fatal("Manager should not report running after a validation failure")
			}
		})
	}
}

func TestStopOnNeverStartedManagerIsNoop(t *testing.T) {
	m := NewManager(state.NewLogStore(16))
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop on idle manager: %v", err)
	}
	if m.BoundLocalPort() != 0 {
		t.Fatalf("BoundLocalPort = %d, want 0 when not running", m.BoundLocalPort())
	}
	if err := m.WaitForSession(ctx, 0); err == nil {
		t.Fatal("WaitForSession should error when not running")
	}
}
