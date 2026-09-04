//go:build darwin

package platform

import (
	"context"
	"net"
	"testing"
	"time"
)

// buildPFRules is covered by killswitch_pf_test.go, which is untagged so it
// runs where there is CI; only darwinKillSwitch itself is tested here.

// The shipped wedge: Enable held the one mutex Active() needs across DNS
// resolution and pfctl, so a hung resolve froze every /status response.
func TestDarwinKillSwitch_ActiveDoesNotBlockBehindEnable(t *testing.T) {
	t.Setenv("PANGEA_APP_SUPPORT_DIR", t.TempDir())
	prevLookup := lookupResolverIP
	defer func() { lookupResolverIP = prevLookup }()

	resolveStarted := make(chan struct{})
	release := make(chan struct{})
	lookupResolverIP = func(ctx context.Context, _, _ string) ([]net.IP, error) {
		close(resolveStarted)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, context.Canceled
	}

	ks := &darwinKillSwitch{}
	enableCtx, cancelEnable := context.WithCancel(context.Background())
	enableDone := make(chan struct{})
	go func() {
		defer close(enableDone)
		_ = ks.Enable(enableCtx, []string{"node.example.com"}, false, false)
	}()
	<-resolveStarted

	activeDone := make(chan bool, 1)
	go func() { activeDone <- ks.Active() }()
	select {
	case active := <-activeDone:
		if active {
			t.Error("kill switch reported active before Enable completed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Active() blocked behind an in-flight Enable")
	}

	cancelEnable()
	close(release)
	<-enableDone
}
