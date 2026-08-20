package state_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func testProfile(id string) state.Profile {
	return state.Profile{
		ID:   id,
		Name: "Test",
		Cloak: state.CloakProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
			LocalPort:  51820,
		},
		WireGuard: state.WireGuardProfile{
			ConfigText: "[Interface]\nPrivateKey=x\n",
		},
	}
}

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
			LocalPort:  51820,
		},
		Naive: &state.NaiveProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
			Username:   "u",
			Password:   "p",
			LocalPort:  origLocalPort,
		},
		WireGuard: state.WireGuardProfile{
			ConfigText: "[Interface]\nPrivateKey=x\n",
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

// TestGet_ClonesTransportProfiles is the regression test for cloneProfile
// leaving the Reality/Hysteria2/Snowflake pointer fields (and Snowflake's
// FrontDomains/ICEServers slices) shallow-copied and thus aliased to the
// config store's internal state — the same aliasing bug cloneNaiveProfile
// fixed for Naive. It stores a profile carrying all three transports, fetches
// it via Get(), mutates the returned copy's transport pointers and Snowflake
// slices, then Get()s again and asserts the store's snapshot is untouched.
func TestGet_ClonesTransportProfiles(t *testing.T) {
	dir := t.TempDir()
	cs, err := state.NewConfigStore(dir + "/config.json")
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}

	const (
		origRealityPort   = 51822
		origHysteria2Port = 51823
		origSnowflakePort = 51824
		origFront         = "front.example.com"
		origICE           = "stun:stun.example.com:3478"
		origEndpointIP    = "203.0.113.40"
	)
	profile := state.Profile{
		ID:                   "p1",
		Name:                 "Test",
		TransportEndpointIPs: []string{origEndpointIP},
		WireGuard: state.WireGuardProfile{
			ConfigText: "[Interface]\nPrivateKey=x\n",
		},
		Reality: &state.RealityProfile{
			RemoteHost: "example.com",
			RemotePort: 443,
			LocalPort:  origRealityPort,
			UUID:       "uuid",
		},
		Hysteria2: &state.Hysteria2Profile{
			RemoteHost: "example.com",
			RemotePort: 443,
			LocalPort:  origHysteria2Port,
			Password:   "pw",
		},
		Snowflake: &state.SnowflakeProfile{
			LocalPort:    origSnowflakePort,
			BrokerURL:    "https://broker.example.com",
			FrontDomains: []string{origFront},
			ICEServers:   []string{origICE},
		},
	}
	if err := cs.Set(state.Config{Profiles: []state.Profile{profile}}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	first := cs.Get()
	if len(first.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(first.Profiles))
	}
	fp := first.Profiles[0]
	if fp.Reality == nil || fp.Hysteria2 == nil || fp.Snowflake == nil {
		t.Fatal("expected first copy to carry Reality, Hysteria2, and Snowflake")
	}

	// Mutate the returned copy's transport pointers and Snowflake slices, the
	// way a caller that forgot to make its own local copy might.
	fp.Reality.LocalPort = 61822
	fp.Hysteria2.LocalPort = 61823
	fp.Snowflake.LocalPort = 61824
	fp.Snowflake.FrontDomains[0] = "leaked.example.com"
	fp.Snowflake.ICEServers[0] = "stun:leaked.example.com:3478"
	fp.TransportEndpointIPs[0] = "198.51.100.99"

	second := cs.Get()
	if len(second.Profiles) != 1 {
		t.Fatalf("expected 1 profile, got %d", len(second.Profiles))
	}
	sp := second.Profiles[0]
	if sp.Reality == nil || sp.Hysteria2 == nil || sp.Snowflake == nil {
		t.Fatal("expected second copy to carry Reality, Hysteria2, and Snowflake")
	}

	// Each Get() must hand back independent pointers; equal pointers mean the
	// store's own *Profile is being aliased out.
	if sp.Reality == fp.Reality {
		t.Fatal("expected distinct *RealityProfile pointers across Get() calls")
	}
	if sp.Hysteria2 == fp.Hysteria2 {
		t.Fatal("expected distinct *Hysteria2Profile pointers across Get() calls")
	}
	if sp.Snowflake == fp.Snowflake {
		t.Fatal("expected distinct *SnowflakeProfile pointers across Get() calls")
	}

	if sp.Reality.LocalPort != origRealityPort {
		t.Errorf("Reality.LocalPort = %d, want unchanged %d (mutation leaked into store)",
			sp.Reality.LocalPort, origRealityPort)
	}
	if sp.Hysteria2.LocalPort != origHysteria2Port {
		t.Errorf("Hysteria2.LocalPort = %d, want unchanged %d (mutation leaked into store)",
			sp.Hysteria2.LocalPort, origHysteria2Port)
	}
	if sp.Snowflake.LocalPort != origSnowflakePort {
		t.Errorf("Snowflake.LocalPort = %d, want unchanged %d (mutation leaked into store)",
			sp.Snowflake.LocalPort, origSnowflakePort)
	}
	if sp.Snowflake.FrontDomains[0] != origFront {
		t.Errorf("Snowflake.FrontDomains[0] = %q, want unchanged %q (slice aliased into store)",
			sp.Snowflake.FrontDomains[0], origFront)
	}
	if sp.Snowflake.ICEServers[0] != origICE {
		t.Errorf("Snowflake.ICEServers[0] = %q, want unchanged %q (slice aliased into store)",
			sp.Snowflake.ICEServers[0], origICE)
	}
	if sp.TransportEndpointIPs[0] != origEndpointIP {
		t.Errorf("TransportEndpointIPs[0] = %q, want unchanged %q (slice aliased into store) — these drive kill-switch permits",
			sp.TransportEndpointIPs[0], origEndpointIP)
	}
}

// TestNewConfigStore_RecoversFromBackupWhenPrimaryMissing simulates a crash
// that lost config.json entirely (the old code's window between renaming it
// away and renaming the temp file into place) but left a good
// config.json.bak on disk. The store must recover the saved profile instead
// of silently starting from DefaultConfig.
func TestNewConfigStore_RecoversFromBackupWhenPrimaryMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cs1, err := state.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}
	if err := cs1.Set(state.Config{Profiles: []state.Profile{testProfile("p1")}}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A second Set produces a config.json.bak holding the first write.
	second := testProfile("p2")
	second.Cloak.LocalPort = 51821
	if err := cs1.Set(state.Config{Profiles: []state.Profile{testProfile("p1"), second}}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("expected backup file to exist: %v", err)
	}

	if err := os.Remove(path); err != nil {
		t.Fatalf("simulate crash by removing primary config: %v", err)
	}

	cs2, err := state.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore should recover from backup, got error: %v", err)
	}
	cfg := cs2.Get()
	if len(cfg.Profiles) == 0 {
		t.Fatal("expected recovered config to carry profiles from the backup, got none")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected recovery to rewrite the primary config: %v", err)
	}
}

// TestNewConfigStore_EmptyFileRecoversOrErrors covers the destructive-reset
// bug: an empty config.json (the most likely crash outcome of a
// non-fsynced write) must never be treated as "no config yet". With a
// backup present it recovers; with no backup it must fail loudly instead of
// silently writing an empty profile list.
func TestNewConfigStore_EmptyFileRecoversOrErrors(t *testing.T) {
	t.Run("with backup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")

		cs1, err := state.NewConfigStore(path)
		if err != nil {
			t.Fatalf("NewConfigStore: %v", err)
		}
		if err := cs1.Set(state.Config{Profiles: []state.Profile{testProfile("p1")}}); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if err := cs1.Set(state.Config{Profiles: []state.Profile{testProfile("p1")}}); err != nil {
			t.Fatalf("Set: %v", err)
		}
		if _, err := os.Stat(path + ".bak"); err != nil {
			t.Fatalf("expected backup file to exist: %v", err)
		}

		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("truncate config to simulate a crashed write: %v", err)
		}

		cs2, err := state.NewConfigStore(path)
		if err != nil {
			t.Fatalf("NewConfigStore should recover from backup, got error: %v", err)
		}
		if len(cs2.Get().Profiles) == 0 {
			t.Fatal("expected recovered config to carry profiles from the backup, got none")
		}
	})

	t.Run("without backup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.json")

		if err := os.WriteFile(path, nil, 0o600); err != nil {
			t.Fatalf("write empty config: %v", err)
		}

		if _, err := state.NewConfigStore(path); err == nil {
			t.Fatal("expected NewConfigStore to fail on an empty config with no backup, got nil error")
		}
	})
}

// TestNewConfigStore_CleansUpStaleTempFile covers a temp file left behind
// by a persist that crashed before its rename: it must not stop the daemon
// from starting, and must not be mistaken for the real config.
func TestNewConfigStore_CleansUpStaleTempFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")

	cs1, err := state.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}
	if err := cs1.Set(state.Config{Profiles: []state.Profile{testProfile("p1")}}); err != nil {
		t.Fatalf("Set: %v", err)
	}

	stalePath := path + ".tmp-stale12345"
	if err := os.WriteFile(stalePath, []byte("garbage, half-written"), 0o600); err != nil {
		t.Fatalf("write stale tmp file: %v", err)
	}

	cs2, err := state.NewConfigStore(path)
	if err != nil {
		t.Fatalf("NewConfigStore: %v", err)
	}
	if len(cs2.Get().Profiles) != 1 {
		t.Fatalf("expected the real config to load unaffected, got %d profiles", len(cs2.Get().Profiles))
	}
	if _, err := os.Stat(stalePath); !os.IsNotExist(err) {
		t.Fatalf("expected stale tmp file to be cleaned up, stat err = %v", err)
	}
}
