//go:build android || ios

package mobile

import (
	"context"
	"encoding/base64"
	"net"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/shadowsocks"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/tunclamp"
)

// closedTCPPort is a loopback port nothing listens on.
func closedTCPPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

// Cloak dials on WireGuard's first datagram, which only comes once the start
// has returned; a start that waited for the session timed out every time.
func TestStartCloakReturnsBeforeWireGuardSendsAnything(t *testing.T) {
	profile := &state.Profile{Cloak: state.CloakProfile{
		RemoteHost:       "127.0.0.1",
		RemotePort:       closedTCPPort(t),
		UID:              base64.StdEncoding.EncodeToString(make([]byte, 16)),
		PublicKey:        base64.StdEncoding.EncodeToString(make([]byte, 32)),
		EncryptionMethod: "plain",
	}}
	st := newStarter(state.NewLogStore(50))
	t.Cleanup(func() { st.stopActive(context.Background()) })

	began := time.Now()
	if err := st.startCloak(context.Background(), profile, nil); err != nil {
		t.Fatalf("startCloak: %v", err)
	}
	if elapsed := time.Since(began); elapsed > 2*time.Second {
		t.Fatalf("startCloak took %s; it must not wait for a session WireGuard has not asked for", elapsed)
	}
	if st.kind != "cloak" || st.boundPort == 0 {
		t.Fatalf("starter recorded kind=%q port=%d, want cloak on a bound port", st.kind, st.boundPort)
	}
}

func TestAPinnedShadowsocksGetsATunnelMTUItsDatagramsFit(t *testing.T) {
	cases := []struct {
		setting   int
		preferred string
		want      int
	}{
		{1380, "shadowsocks", shadowsocks.MaxTunnelMTU},
		{1420, " shadowsocks ", shadowsocks.MaxTunnelMTU},
		{1280, "shadowsocks", 1280},
		{9000, "shadowsocks", shadowsocks.MaxTunnelMTU}, // out of range: the default, then the cap
		{1380, "auto", 1380},
		{1420, "", 1420},
		{1420, "reality", 1420},
		{1300, "cloak", 1300},
	}
	for _, c := range cases {
		if got := tunnelMTU(c.setting, c.preferred); got != c.want {
			t.Errorf("tunnelMTU(%d, %q) = %d, want %d", c.setting, c.preferred, got, c.want)
		}
	}
}

func TestShadowsocksWinningOverAWiderTUNClampsTCP(t *testing.T) {
	logs := state.NewLogStore(50)
	attempt := &startAttempt{
		prepared: &preparedTunnel{mtu: 1380},
		clamp:    tunclamp.Wrap(tuntest.NewChannelTUN().TUN()),
	}

	fitCarrier(attempt, "shadowsocks", logs)
	if got := attempt.clamp.ClampMTU(); got != shadowsocks.MaxTunnelMTU {
		t.Fatalf("shadowsocks over a 1380 TUN: clamp = %d, want %d", got, shadowsocks.MaxTunnelMTU)
	}
	fitCarrier(attempt, "reality", logs)
	if got := attempt.clamp.ClampMTU(); got != 0 {
		t.Fatalf("reality: clamp = %d, want off", got)
	}

	attempt.prepared.mtu = shadowsocks.MaxTunnelMTU
	fitCarrier(attempt, "shadowsocks", logs)
	if got := attempt.clamp.ClampMTU(); got != 0 {
		t.Fatalf("shadowsocks over a TUN that already fits: clamp = %d, want off", got)
	}
}
