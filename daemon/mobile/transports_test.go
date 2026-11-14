//go:build android || ios

package mobile

import (
	"context"
	"encoding/base64"
	"net"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
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
