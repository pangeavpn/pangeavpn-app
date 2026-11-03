//go:build ios

package mobile

import (
	"encoding/json"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// checkHostFDAfterRefusedStart: the extension keeps its fd, so a refused Start leaves it open.
func checkHostFDAfterRefusedStart(t *testing.T, fd int) {
	t.Helper()
	if !fdIsOpen(fd) {
		t.Fatalf("refused Start closed the host's fd %d", fd)
	}
	_ = unix.Close(fd)
}

func TestStartReportsAnAdoptFailure(t *testing.T) {
	initForTest(t, nil)
	mu.Lock()
	prepared = &preparedTunnel{serverID: "fra", serverName: "Frankfurt", profile: &state.Profile{}}
	mu.Unlock()

	err := Start(-1)
	if err == nil || !strings.Contains(err.Error(), "adopt tun fd") {
		t.Fatalf("Start(-1) = %v, want an adopt failure", err)
	}
	var status statusJSON
	if err := json.Unmarshal([]byte(State()), &status); err != nil {
		t.Fatalf("decode State(): %v", err)
	}
	if status.State != string(state.StateError) || !strings.Contains(status.Detail, "adopt tun fd") {
		t.Fatalf("State() = %+v, want the adopt failure", status)
	}
}
