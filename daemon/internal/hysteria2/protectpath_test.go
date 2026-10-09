package hysteria2

import (
	"testing"

	"github.com/sagernet/sing-box/option"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestBuildClientOptionsCarriesProtectPath(t *testing.T) {
	previous := ProtectPath
	t.Cleanup(func() { ProtectPath = previous })

	profile := state.Hysteria2Profile{
		RemoteHost: "1.2.3.4", RemotePort: 443,
		Password: "p", ObfsPassword: "o",
	}

	ProtectPath = "/data/app/protect.sock"
	opts, err := buildClientOptions(profile, 1080)
	if err != nil {
		t.Fatalf("buildClientOptions: %v", err)
	}
	outbound, ok := opts.Outbounds[0].Options.(*option.Hysteria2OutboundOptions)
	if !ok {
		t.Fatalf("outbound options are %T", opts.Outbounds[0].Options)
	}
	if outbound.ProtectPath != ProtectPath {
		t.Fatalf("ProtectPath = %q, want %q", outbound.ProtectPath, ProtectPath)
	}
}
