package anytls

import (
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestBuildOutboundOptionsCarriesProtectPath(t *testing.T) {
	previous := ProtectPath
	t.Cleanup(func() { ProtectPath = previous })

	ProtectPath = "/data/app/protect.sock"
	opts, err := buildOutboundOptions(state.AnyTLSProfile{RemoteHost: "1.2.3.4", RemotePort: 8446, Password: "p"})
	if err != nil {
		t.Fatalf("buildOutboundOptions: %v", err)
	}
	if opts.ProtectPath != ProtectPath {
		t.Fatalf("ProtectPath = %q, want %q", opts.ProtectPath, ProtectPath)
	}
}
