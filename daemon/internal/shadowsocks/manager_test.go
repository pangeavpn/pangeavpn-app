package shadowsocks

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	M "github.com/sagernet/sing/common/metadata"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// pickFreeLoopbackUDPPort probes with UDP rather than TCP: on Windows the
// ephemeral ranges are not interchangeable (Hyper-V reserves UDP blocks).
func pickFreeLoopbackUDPPort(t *testing.T) int {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("pick free udp port: %v", err)
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func testManager(t *testing.T) *Manager {
	t.Helper()
	return NewManager(state.NewLogStore(100))
}

func startableProfile(t *testing.T) state.ShadowsocksProfile {
	t.Helper()
	return state.ShadowsocksProfile{
		LocalPort:  0,
		RemoteHost: "127.0.0.1",
		RemotePort: pickFreeLoopbackUDPPort(t),
		Method:     "chacha20-ietf-poly1305",
		Password:   "hunter2",
	}
}

// Start succeeds with nothing listening on the far side: ListenPacket only
// dials a UDP socket, which is exactly why this package has no SessionWaiter.
func TestManager_StartBindsLocalPortAndStops(t *testing.T) {
	mgr := testManager(t)
	if got := mgr.BoundLocalPort(); got != 0 {
		t.Fatalf("BoundLocalPort() before start = %d, want 0", got)
	}
	if status := mgr.Status(); status.Running {
		t.Fatal("Status().Running before start = true, want false")
	}

	if err := mgr.Start(context.Background(), startableProfile(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })

	port := mgr.BoundLocalPort()
	if port <= 0 {
		t.Fatalf("BoundLocalPort() = %d, want a kernel-assigned port", port)
	}
	if status := mgr.Status(); !status.Running || status.PID == nil {
		t.Fatalf("Status() = %+v, want running with a PID", status)
	}

	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := mgr.BoundLocalPort(); got != 0 {
		t.Fatalf("BoundLocalPort() after stop = %d, want 0", got)
	}
	if status := mgr.Status(); status.Running {
		t.Fatal("Status().Running after stop = true, want false")
	}
}

func TestManager_DoubleStartIsNoOp(t *testing.T) {
	mgr := testManager(t)
	profile := startableProfile(t)
	if err := mgr.Start(context.Background(), profile); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })

	first := mgr.BoundLocalPort()
	if err := mgr.Start(context.Background(), profile); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if second := mgr.BoundLocalPort(); second != first {
		t.Fatalf("BoundLocalPort() = %d after the second Start, want the first run's %d", second, first)
	}
}

// A Start with a different profile while running is a server switch: it must
// tear down the old session and bind a fresh one, not silently no-op.
func TestManager_StartWithDifferentProfileSwitchesSession(t *testing.T) {
	mgr := testManager(t)
	if err := mgr.Start(context.Background(), startableProfile(t)); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })

	first := mgr.BoundLocalPort()
	next := startableProfile(t)
	next.LocalPort = pickFreeLoopbackUDPPort(t)
	if err := mgr.Start(context.Background(), next); err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if got := mgr.BoundLocalPort(); got != next.LocalPort {
		t.Fatalf("BoundLocalPort() = %d, want the new profile's requested %d (first run's was %d)", got, next.LocalPort, first)
	}
}

func TestManager_StopWhenNotRunning(t *testing.T) {
	if err := testManager(t).Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start = %v, want nil", err)
	}
}

func TestManager_StartRejectsInvalidProfile(t *testing.T) {
	mgr := testManager(t)
	profile := startableProfile(t)
	profile.Password = ""

	err := mgr.Start(context.Background(), profile)
	if err == nil {
		t.Fatal("Start with no password = nil, want a validation error")
	}
	if !strings.Contains(err.Error(), "password is required") {
		t.Fatalf("Start error = %v, want the password validation error", err)
	}
	if mgr.Status().Running {
		t.Fatal("a rejected Start must leave the manager stopped")
	}
}

func TestManager_StartHonoursExplicitLocalPort(t *testing.T) {
	mgr := testManager(t)
	profile := startableProfile(t)
	profile.LocalPort = pickFreeLoopbackUDPPort(t)

	if err := mgr.Start(context.Background(), profile); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })

	if got := mgr.BoundLocalPort(); got != profile.LocalPort {
		t.Fatalf("BoundLocalPort() = %d, want the requested %d", got, profile.LocalPort)
	}
}

// A second Start after Stop must rebind rather than reuse the dead socket.
func TestManager_RestartRebinds(t *testing.T) {
	mgr := testManager(t)
	if err := mgr.Start(context.Background(), startableProfile(t)); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := mgr.Start(context.Background(), startableProfile(t)); err != nil {
		t.Fatalf("restart: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })

	if got := mgr.BoundLocalPort(); got <= 0 {
		t.Fatalf("BoundLocalPort() after restart = %d, want a fresh port", got)
	}
}

func TestManager_StopIsIdempotent(t *testing.T) {
	mgr := testManager(t)
	if err := mgr.Start(context.Background(), startableProfile(t)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	for i := range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := mgr.Stop(ctx)
		cancel()
		if err != nil {
			t.Fatalf("Stop #%d: %v", i+1, err)
		}
	}
}

func TestDestinationUDPAddr(t *testing.T) {
	addr := destinationUDPAddr("127.0.0.1", 51820)
	udp, ok := addr.(*net.UDPAddr)
	if !ok {
		t.Fatalf("destinationUDPAddr(literal) = %T, want *net.UDPAddr", addr)
	}
	if udp.Port != 51820 || !udp.IP.Equal(net.ParseIP("127.0.0.1")) {
		t.Fatalf("destinationUDPAddr() = %v, want 127.0.0.1:51820", udp)
	}

	// A hostname stays unresolved: the SS server resolves it, and the client
	// must not emit a DNS query a Lockdown lock would block anyway.
	hostAddr := destinationUDPAddr("wg.internal", 51820)
	socksaddr, ok := hostAddr.(M.Socksaddr)
	if !ok {
		t.Fatalf("destinationUDPAddr(hostname) = %T, want M.Socksaddr", hostAddr)
	}
	if socksaddr.Fqdn != "wg.internal" || socksaddr.Port != 51820 {
		t.Fatalf("destinationUDPAddr() = %+v, want wg.internal:51820", socksaddr)
	}
}
