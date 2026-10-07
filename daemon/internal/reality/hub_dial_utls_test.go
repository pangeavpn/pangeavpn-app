//go:build with_utls

// REALITY needs uTLS to build an engine at all, so this runs only in builds that
// carry it, as the daemon and the transport e2e runs do.
package reality

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// TEST-NET-1 never answers, so a dial that ignored the bind would hang to the
// deadline; binding to a missing interface must instead fail at once.
func TestDialHub_AppliesTheBoundInterface(t *testing.T) {
	keys, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	p := NewProxyManager(state.NewLogStore(8))
	p.mu.Lock()
	p.running = true
	p.profile = hubProfile(state.RealityProfile{
		RemoteHost: "192.0.2.1", RemotePort: 443, UUID: "b831381d-6324-4d53-ad4f-8cda48b30811",
		PublicKey: keys.PublicKey, ShortID: "0123", ServerName: "swdist.apple.com",
	})
	p.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	start := time.Now()
	conn, err := p.DialHub(ctx, "pangea-no-such-interface", "api.pangeavpn.org", 443)
	if err == nil {
		conn.Close()
		t.Fatal("DialHub succeeded bound to a missing interface")
	}
	if elapsed := time.Since(start); elapsed > time.Second || !strings.Contains(err.Error(), "interface") {
		t.Fatalf("DialHub failed after %s with %v; a bind that was applied fails at once", elapsed, err)
	}
}
