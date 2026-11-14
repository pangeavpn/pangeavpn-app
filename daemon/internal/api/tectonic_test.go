package api

import (
	"slices"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/transport"
)

func tectonicTestProfile() state.Profile {
	profile := testProfile()
	profile.Tectonic = &state.TectonicProfile{
		Node: "203.0.113.70:443", KeyID: make([]byte, 20), DeviceKey: make([]byte, 32),
		CDN: &state.TectonicCDNRoute{Host: "assets.cdn.example", Path: "/ws", Edge: "198.51.100.9:443"},
	}
	return profile
}

func TestStarterFor_TectonicNeedsItsBlockAndASingleHop(t *testing.T) {
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
	profile := tectonicTestProfile()
	if _, availability := svc.StarterFor(&profile, "tectonic"); availability != transport.Available {
		t.Fatalf("availability = %v, want Available", availability)
	}
	profile.Hop = &state.HopProfile{SingBoxPort: 9100, CloakProxyMethod: "hop", EntryRegion: "a", ExitRegion: "b"}
	if _, availability := svc.StarterFor(&profile, "tectonic"); availability != transport.NotConfigured {
		t.Fatalf("multihop availability = %v, want NotConfigured: the node would egress at the entry", availability)
	}
	profile = testProfile()
	if _, availability := svc.StarterFor(&profile, "tectonic"); availability != transport.NotConfigured {
		t.Fatalf("no block: availability = %v, want NotConfigured", availability)
	}
}

func TestAutoCascadeTriesTectonicFirst(t *testing.T) {
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
	profile := tectonicTestProfile()
	candidates, err := svc.transportCandidates(&profile, "auto")
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) < 2 || candidates[0].Kind != "tectonic" || candidates[1].Kind != "cloak" {
		t.Fatalf("cascade = %v, want tectonic then cloak", candidates)
	}
}

func TestKillSwitchPermits_IncludeTectonicNodeAndCDNEdge(t *testing.T) {
	permits := killSwitchPermits(tectonicTestProfile())
	for _, want := range []string{"203.0.113.70", "198.51.100.9"} {
		if !slices.Contains(permits, want) {
			t.Errorf("killSwitchPermits() = %v, want %s", permits, want)
		}
	}
	// The hub's endpoint IPs replace node hosts, but the CDN edge is never among them.
	profile := tectonicTestProfile()
	profile.TransportEndpointIPs = []string{"203.0.113.70"}
	if permits := killSwitchPermits(profile); !slices.Contains(permits, "198.51.100.9") {
		t.Errorf("with endpoint IPs, permits = %v, want the CDN edge kept", permits)
	}
}

func TestDeepCopyProfile_DoesNotAliasTectonic(t *testing.T) {
	profile := tectonicTestProfile()
	out := deepCopyProfile(profile)
	out.Tectonic.KeyID[0] = 9
	out.Tectonic.CDN.Host = "other"
	if profile.Tectonic.KeyID[0] != 0 || profile.Tectonic.CDN.Host != "assets.cdn.example" {
		t.Fatal("deepCopyProfile shares Tectonic state with its source")
	}
}
