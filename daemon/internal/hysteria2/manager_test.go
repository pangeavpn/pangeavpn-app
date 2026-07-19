package hysteria2

import (
	"context"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestManagerStatusAndBoundPortWhenNotRunning(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	if got := m.Status(); got.Running {
		t.Fatalf("Status().Running = true before Start")
	}
	if got := m.BoundLocalPort(); got != 0 {
		t.Fatalf("BoundLocalPort() = %d, want 0 before Start", got)
	}
}

func TestManagerStartRejectsInvalidProfile(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	err := m.Start(context.Background(), state.Hysteria2Profile{})
	if err == nil {
		t.Fatalf("expected error starting with an empty profile")
	}
	if m.Status().Running {
		t.Fatalf("Status().Running = true after a failed Start")
	}
}

func TestManagerStopWhenNotRunningIsNoop(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop on a never-started manager: %v", err)
	}
}

func TestManagerWaitForSessionWhenNotRunning(t *testing.T) {
	m := NewManager(state.NewLogStore(10))
	if err := m.WaitForSession(context.Background(), 0); err == nil {
		t.Fatalf("expected error waiting for session on a manager that never started")
	}
}
