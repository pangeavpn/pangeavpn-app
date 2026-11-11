//go:build android || ios

package mobile

import (
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

func TestStarterForTectonicNeedsACredentialAndASingleHop(t *testing.T) {
	st := newStarter(nil)
	if _, got := st.StarterFor(&state.Profile{}, "tectonic"); got != transport.NotConfigured {
		t.Fatalf("no credential = %v, want NotConfigured", got)
	}
	hop := &state.Profile{Tectonic: &state.TectonicProfile{Node: "203.0.113.7:443"}, Hop: &state.HopProfile{}}
	if _, got := st.StarterFor(hop, "tectonic"); got != transport.NotConfigured {
		t.Fatalf("multihop = %v, want NotConfigured", got)
	}
	single := &state.Profile{Tectonic: &state.TectonicProfile{Node: "203.0.113.7:443"}}
	start, got := st.StarterFor(single, "tectonic")
	if got != st.singBoxAvailability() || (got == transport.Available && start == nil) {
		t.Fatalf("single hop = %v, want the protect gate's answer with a start func", got)
	}
}
