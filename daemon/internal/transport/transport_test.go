package transport_test

import (
	"context"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

type fakeManager struct {
	stopCalled bool
	stopErr    error
}

func (f *fakeManager) Stop(ctx context.Context) error {
	f.stopCalled = true
	return f.stopErr
}

func TestManagerInterfaceSatisfiedByFake(t *testing.T) {
	fake := &fakeManager{}
	var m transport.Manager = fake

	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("Stop returned error: %v", err)
	}
	if !fake.stopCalled {
		t.Fatalf("Stop() on the interface value did not reach the underlying fake")
	}
}
