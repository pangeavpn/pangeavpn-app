package state_test

import (
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// TestFindProfile_ClonesNaiveProfile is the regression test for the
// cloneProfile aliasing bug: cloneProfile deep-copies WireGuard.DNS and
// WireGuard.BypassHosts but, before this fix, left the Naive pointer
// pointing straight at the config store's own internal *state.NaiveProfile.
// Any caller that mutated the returned profile's Naive field (as
// api.Service's fallbackToNaive used to, before being patched locally at
// that one call site) would silently corrupt the store's data without its
// lock.
//
// This test proves the fix by fetching the same profile twice via
// FindProfile, mutating the first copy's Naive.LocalPort, and asserting the
// second copy is unaffected — which only holds if each call returns an
// independent *NaiveProfile.
func TestFindProfile_ClonesNaiveProfile(t *testing.T) {
	dir := t.TempDir()
	cs, err := state.NewConfigStore(dir + "/config.json")
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}

	const origLocalPort = 51821
	profile := state.Profile{
		ID:   "p1",
		Name: "Test",
		Cloak: state.CloakProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
		},
		Naive: &state.NaiveProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
			Username:   "u",
			Password:   "p",
			LocalPort:  origLocalPort,
		},
	}
	if err := cs.Set(state.Config{Profiles: []state.Profile{profile}}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	first, ok := cs.FindProfile("p1")
	if !ok {
		t.Fatal("expected to find profile p1")
	}
	if first.Naive == nil {
		t.Fatal("expected first clone to have a non-nil Naive")
	}

	// Mutate the first clone's Naive pointer directly, the way a caller that
	// forgot to make its own local copy might.
	first.Naive.LocalPort = 61822

	second, ok := cs.FindProfile("p1")
	if !ok {
		t.Fatal("expected to find profile p1")
	}
	if second.Naive == nil {
		t.Fatal("expected second clone to have a non-nil Naive")
	}
	if second.Naive == first.Naive {
		t.Fatal("expected first and second clones to have distinct *NaiveProfile pointers")
	}
	if second.Naive.LocalPort != origLocalPort {
		t.Errorf("second clone's Naive.LocalPort = %d, want unchanged original %d (mutation via first clone leaked into config store)",
			second.Naive.LocalPort, origLocalPort)
	}
}
