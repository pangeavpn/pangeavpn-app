package shadowsocks

import (
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestBuildOutboundOptionsCarriesProtectPath(t *testing.T) {
	previous := ProtectPath
	t.Cleanup(func() { ProtectPath = previous })

	profile := state.ShadowsocksProfile{RemoteHost: "1.2.3.4", RemotePort: 443, Password: "p"}
	if opts := buildOutboundOptions(profile); opts.ProtectPath != "" {
		t.Fatalf("ProtectPath = %q, want empty when unset", opts.ProtectPath)
	}

	ProtectPath = "/data/app/protect.sock"
	opts := buildOutboundOptions(profile)
	if opts.ProtectPath != ProtectPath {
		t.Fatalf("ProtectPath = %q, want %q", opts.ProtectPath, ProtectPath)
	}
}
