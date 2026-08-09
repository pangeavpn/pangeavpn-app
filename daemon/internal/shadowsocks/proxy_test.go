package shadowsocks

import (
	"context"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestProxyManager_StartStopLifecycle(t *testing.T) {
	mgr := NewProxyManager(state.NewLogStore(100))
	if got := mgr.Port(); got != 0 {
		t.Fatalf("Port() before start = %d, want 0", got)
	}

	port, err := mgr.Start(context.Background(), startableProfile(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })

	if port <= 0 {
		t.Fatalf("Start returned port %d, want > 0", port)
	}
	if got := mgr.Port(); got != port {
		t.Fatalf("Port() = %d, want the started %d", got, port)
	}

	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if got := mgr.Port(); got != 0 {
		t.Fatalf("Port() after stop = %d, want 0", got)
	}
}

func TestProxyManager_DoubleStartReturnsSamePort(t *testing.T) {
	mgr := NewProxyManager(state.NewLogStore(100))
	first, err := mgr.Start(context.Background(), startableProfile(t))
	if err != nil {
		t.Fatalf("first Start: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })

	second, err := mgr.Start(context.Background(), startableProfile(t))
	if err != nil {
		t.Fatalf("second Start: %v", err)
	}
	if second != first {
		t.Fatalf("second Start rebound to %d, want the live %d", second, first)
	}
}

func TestProxyManager_StopWhenNotRunning(t *testing.T) {
	if err := NewProxyManager(state.NewLogStore(100)).Stop(context.Background()); err != nil {
		t.Fatalf("Stop before Start = %v, want nil", err)
	}
}

func TestProxyManager_RejectsInvalidProfile(t *testing.T) {
	mgr := NewProxyManager(state.NewLogStore(100))
	profile := startableProfile(t)
	profile.Method = "rc4-md5"

	if _, err := mgr.Start(context.Background(), profile); err == nil {
		t.Fatal("Start with a stream cipher = nil, want it rejected")
	} else if !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("Start error = %v, want the unsupported-method error", err)
	}
	if got := mgr.Port(); got != 0 {
		t.Fatalf("Port() after a rejected Start = %d, want 0", got)
	}
}

// A restart must bind a fresh port rather than reuse the closed listener.
func TestProxyManager_RestartRebinds(t *testing.T) {
	mgr := NewProxyManager(state.NewLogStore(100))
	if _, err := mgr.Start(context.Background(), startableProfile(t)); err != nil {
		t.Fatalf("first Start: %v", err)
	}
	if err := mgr.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	port, err := mgr.Start(context.Background(), startableProfile(t))
	if err != nil {
		t.Fatalf("restart: %v", err)
	}
	t.Cleanup(func() { mgr.Stop(context.Background()) })
	if port <= 0 {
		t.Fatalf("restart port = %d, want > 0", port)
	}
}
