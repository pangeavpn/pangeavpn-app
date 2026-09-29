package reality

import (
	"context"
	"errors"
	"testing"

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
