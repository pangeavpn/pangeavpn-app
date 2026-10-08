package shadowsocks

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestProxyManager_HubRemoteIsEmptyWhenStopped(t *testing.T) {
	if got := NewProxyManager(state.NewLogStore(8)).HubRemote(); got != "" {
		t.Fatalf("HubRemote = %q on a stopped proxy, want empty", got)
	}
}

func TestProxyManager_DialHubRefusesWhenStopped(t *testing.T) {
	_, err := NewProxyManager(state.NewLogStore(8)).DialHub(context.Background(), "", "api.pangeavpn.org", 443)
	if !errors.Is(err, errHubProxyStopped) {
		t.Fatalf("DialHub err = %v, want errHubProxyStopped", err)
	}
}

// TEST-NET-1 never answers, so a dial that ignored the bind would hang to the
// deadline; binding to a missing interface must instead fail at once.
func TestDialHub_AppliesTheBoundInterface(t *testing.T) {
	p := NewProxyManager(state.NewLogStore(8))
	p.mu.Lock()
	p.running = true
	p.profile = state.ShadowsocksProfile{
		RemoteHost: "192.0.2.1", RemotePort: 8489, Method: "chacha20-ietf-poly1305", Password: "hub-dial-bind",
	}
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	conn, err := p.DialHub(ctx, "pangea-no-such-interface", "api.pangeavpn.org", 443)
	if err == nil {
		// Shadowsocks dials lazily: the bind error surfaces on first use.
		_, err = conn.Write([]byte("ping"))
		conn.Close()
	}
	if err == nil {
		t.Fatal("DialHub carried data bound to a missing interface")
	}
	if elapsed := time.Since(start); elapsed > time.Second || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("DialHub failed after %s with %v; a bind that was applied fails at once", elapsed, err)
	}
}
