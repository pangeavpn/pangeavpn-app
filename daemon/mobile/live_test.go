//go:build android || ios

package mobile

import (
	"encoding/json"
	"errors"
	"testing"

	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/tunclamp"
)

// withLiveTunnel installs a running tunnel and settings for one test, restoring both after.
func withLiveTunnel(t *testing.T, rt *tunnelRuntime, cfg config, preferred string) {
	t.Helper()
	mu.Lock()
	prevTunnel, prevSettings, prevPreferred, prevLogs := activeTunnel, settings, preferredTransport, wgLogs
	activeTunnel, settings, preferredTransport, wgLogs = rt, cfg, preferred, state.NewLogStore(50)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		activeTunnel, settings, preferredTransport, wgLogs = prevTunnel, prevSettings, prevPreferred, prevLogs
		mu.Unlock()
	})
}

func liveConfig(t *testing.T) tunnelConfigJSON {
	t.Helper()
	raw, err := LiveConfig()
	if err != nil {
		t.Fatalf("LiveConfig: %v", err)
	}
	var got tunnelConfigJSON
	if err := json.Unmarshal([]byte(raw), &got); err != nil {
		t.Fatalf("decode %q: %v", raw, err)
	}
	return got
}

func TestLiveConfigFollowsSavedDNSAndMTU(t *testing.T) {
	p := &preparedTunnel{address: "10.8.0.7", serverDNS: "10.8.0.1, 10.8.0.2", mtu: 1380, serverID: "de-1"}
	rt := &tunnelRuntime{prepared: p, activeTransport: "reality"}
	cfg := defaultConfig()
	cfg.MTU = 1300
	cfg.CustomDNS = []string{"9.9.9.9"}
	withLiveTunnel(t, rt, cfg, "auto")

	got := liveConfig(t)
	if got.Address != "10.8.0.7" || got.PrefixLength != 32 || got.ServerID != "de-1" {
		t.Fatalf("interface %+v, want the hub's address for de-1", got)
	}
	if len(got.DNS) != 1 || got.DNS[0] != "9.9.9.9" || got.MTU != 1300 {
		t.Fatalf("dns %v mtu %d, want the custom 9.9.9.9 at 1300", got.DNS, got.MTU)
	}
	if p.mtu != 1300 {
		t.Fatalf("prepared MTU %d, want it to follow the applied 1300", p.mtu)
	}

	mu.Lock()
	settings.CustomDNS = nil
	mu.Unlock()
	if got := liveConfig(t); len(got.DNS) != 2 || got.DNS[0] != "10.8.0.1" || got.DNS[1] != "10.8.0.2" {
		t.Fatalf("dns %v after clearing custom DNS, want the hub's two resolvers", got.DNS)
	}
}

func TestLiveConfigRefitsTheShadowsocksClamp(t *testing.T) {
	clamp := tunclamp.Wrap(tuntest.NewChannelTUN().TUN())
	rt := &tunnelRuntime{
		prepared:        &preparedTunnel{address: "10.8.0.7", serverDNS: "10.8.0.1", mtu: shadowsocks.MaxTunnelMTU},
		activeTransport: "shadowsocks", clamp: clamp,
	}
	cfg := defaultConfig()
	cfg.MTU = 1420
	withLiveTunnel(t, rt, cfg, "auto")

	liveConfig(t)
	if got := clamp.ClampMTU(); got != shadowsocks.MaxTunnelMTU {
		t.Fatalf("clamp %d after raising the MTU under shadowsocks, want %d", got, shadowsocks.MaxTunnelMTU)
	}
}

func TestLiveConfigAndSwapNeedARunningTunnel(t *testing.T) {
	withLiveTunnel(t, nil, defaultConfig(), "auto")
	if _, err := LiveConfig(); !errors.Is(err, errNoRunningTunnel) {
		t.Fatalf("LiveConfig with no tunnel: %v, want errNoRunningTunnel", err)
	}
	if err := SwapTUN(-1); !errors.Is(err, errNoRunningTunnel) {
		t.Fatalf("SwapTUN with no tunnel: %v, want errNoRunningTunnel", err)
	}
}
