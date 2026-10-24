package reality

import (
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestBuildOutboundOptionsCarriesProtectPath(t *testing.T) {
	previous := ProtectPath
	t.Cleanup(func() { ProtectPath = previous })

	profile := state.RealityProfile{UUID: "u", PublicKey: "k"}
	if opts := buildOutboundOptions(profile, "1.2.3.4", 443, "sni"); opts.ProtectPath != "" {
		t.Fatalf("ProtectPath = %q, want empty when unset", opts.ProtectPath)
	}

	ProtectPath = "/data/app/protect.sock"
	opts := buildOutboundOptions(profile, "1.2.3.4", 443, "sni")
	if opts.ProtectPath != ProtectPath {
		t.Fatalf("ProtectPath = %q, want %q", opts.ProtectPath, ProtectPath)
	}
}
