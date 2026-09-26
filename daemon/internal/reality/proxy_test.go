package reality

import (
	"context"
	"slices"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestBuildHubOutboundOptionsUsesVisionOverTCP(t *testing.T) {
	profile := state.RealityProfile{
		UUID:      "9f8f3c1e-1234-4a5b-8c9d-abcdef012345",
		PublicKey: "public-key-value",
		ShortID:   "deadbeef",
	}
	opts := buildHubOutboundOptions(profile, "203.0.113.10", 443, "cover.example.com")

	if opts.Server != "203.0.113.10" || opts.ServerPort != 443 {
		t.Fatalf("ServerOptions = %s:%d, want 203.0.113.10:443", opts.Server, opts.ServerPort)
	}
	if opts.UUID != profile.UUID {
		t.Fatalf("UUID = %q, want %q", opts.UUID, profile.UUID)
	}
	if opts.Flow != "xtls-rprx-vision" {
		t.Fatalf("Flow = %q, want xtls-rprx-vision (the node's hub user requires it)", opts.Flow)
	}
	if got := opts.Network.Build(); !slices.Equal(got, []string{"tcp"}) {
		t.Fatalf("Network = %v, want [tcp]: Vision cannot carry UDP", got)
	}
	if opts.TLS == nil || !opts.TLS.Enabled {
		t.Fatal("expected TLS enabled")
	}
	if opts.TLS.ServerName != "cover.example.com" {
		t.Fatalf("ServerName = %q, want cover.example.com", opts.TLS.ServerName)
	}
	if opts.TLS.UTLS == nil || !opts.TLS.UTLS.Enabled || opts.TLS.UTLS.Fingerprint != utlsFingerprint {
		t.Fatalf("UTLS = %+v, want enabled with the %q fingerprint", opts.TLS.UTLS, utlsFingerprint)
	}
	if opts.TLS.Reality == nil || !opts.TLS.Reality.Enabled {
		t.Fatal("expected Reality enabled")
	}
	if opts.TLS.Reality.PublicKey != profile.PublicKey || opts.TLS.Reality.ShortID != profile.ShortID {
		t.Fatalf("Reality = %+v, want the profile's key and short ID", opts.TLS.Reality)
	}
}

// The data-plane builder must keep forcing an empty flow even with the hub
// builder alongside it, or the UDP relay handshake breaks.
func TestBuildOutboundOptionsIgnoresHubFlow(t *testing.T) {
	opts := buildOutboundOptions(state.RealityProfile{UUID: "u", PublicKey: "k", Flow: hubFlow}, "h", 443, "s")
	if opts.Flow != "" {
		t.Fatalf("data-plane Flow = %q, want empty", opts.Flow)
	}
}

func TestProxyManager_IdleReportsNothing(t *testing.T) {
	mgr := NewProxyManager(state.NewLogStore(16))
	if got := mgr.Port(); got != 0 {
		t.Fatalf("Port() before start = %d, want 0", got)
	}
	if user, pass := mgr.Credentials(); user != "" || pass != "" {
		t.Fatal("Credentials() before start returned a credential")
	}
	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start = %v, want nil", err)
	}
}

func TestProxyManager_RejectsIncompleteProfile(t *testing.T) {
	valid := state.RealityProfile{
		RemoteHost: "127.0.0.1",
		RemotePort: 443,
		UUID:       "9f8f3c1e-1234-4a5b-8c9d-abcdef012345",
		PublicKey:  "public-key-value",
		ServerName: "cover.example.com",
	}
	cases := []struct {
		name   string
		mutate func(*state.RealityProfile)
	}{
		{"missing remote host", func(p *state.RealityProfile) { p.RemoteHost = " " }},
		{"missing uuid", func(p *state.RealityProfile) { p.UUID = "" }},
		{"missing public key", func(p *state.RealityProfile) { p.PublicKey = "" }},
		{"zero remote port", func(p *state.RealityProfile) { p.RemotePort = 0 }},
		{"remote port too large", func(p *state.RealityProfile) { p.RemotePort = 65536 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			profile := valid
			tc.mutate(&profile)
			mgr := NewProxyManager(state.NewLogStore(16))
			if _, err := mgr.Start(context.Background(), profile); err == nil {
				mgr.Stop(context.Background())
				t.Fatal("Start accepted an incomplete profile")
			}
			if got := mgr.Port(); got != 0 {
				t.Fatalf("Port() after a rejected Start = %d, want 0", got)
			}
		})
	}
}

// Fields the data-plane transport uses must not make an identical hub
// profile look like a rotation and force a rebind.
func TestHubProfileIgnoresDataPlaneFields(t *testing.T) {
	base := state.RealityProfile{RemoteHost: "203.0.113.10", RemotePort: 443, UUID: "u", PublicKey: "k", ShortID: "ab", ServerName: "s"}
	noisy := base
	noisy.LocalPort, noisy.TargetPort, noisy.Flow = 5555, 51820, "xtls-rprx-vision"
	noisy.RemoteHost = " 203.0.113.10 "
	if hubProfile(noisy) != hubProfile(base) {
		t.Fatalf("hubProfile(%+v) != hubProfile(%+v)", noisy, base)
	}
	changed := base
	changed.ShortID = "cd"
	if hubProfile(changed) == hubProfile(base) {
		t.Fatal("hubProfile dropped the short ID")
	}
}
