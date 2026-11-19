package api

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/nicpin"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

const (
	testCDNEdgeName = "sync.cdn.example"
	testCDNEdgeIP   = "104.21.24.200"
)

func cdnEdgeProfile(remote string) state.Profile {
	profile := testProfile()
	profile.CDN = &state.CDNProfile{RemoteHost: remote, UUID: "u", ServerName: testCDNEdgeName, Path: "/p"}
	return profile
}

// A CDN edge's addresses serve unrelated sites, so a route or an all-app permit for one
// would take every one of them out of the tunnel: the leak this guards against.
func TestCDNEdge_NeverRoutedNorPermittedToAllApps(t *testing.T) {
	for _, remote := range []string{testCDNEdgeName, testCDNEdgeIP, " SYNC.CDN.example. "} {
		profile := cdnEdgeProfile(remote)
		profile.TransportEndpointIPs = []string{"192.0.2.50"}
		profile.WireGuard.BypassHosts = []string{"198.51.100.7"}
		if remote == testCDNEdgeIP {
			// Older apps sent a literal edge among the endpoint IPs too. A named edge's
			// addresses are caught where they resolve, in the kill switch (withoutDaemonIPs).
			profile.TransportEndpointIPs = append(profile.TransportEndpointIPs, testCDNEdgeIP)
			profile.WireGuard.BypassHosts = append(profile.WireGuard.BypassHosts, testCDNEdgeIP)
		}

		for name, hosts := range map[string][]string{
			"killSwitchPermits": killSwitchPermits(profile),
			"bypassHosts":       withTransportBypassHosts(profile).BypassHosts,
		} {
			for _, h := range hosts {
				if k := hostKey(h); k == testCDNEdgeIP || k == testCDNEdgeName {
					t.Errorf("remote %q: %s = %v, want the shared edge left out", remote, name, hosts)
				}
			}
			for _, keep := range []string{"192.0.2.50", "198.51.100.7"} {
				if !slices.Contains(hosts, keep) {
					t.Errorf("remote %q: %s = %v, lost dedicated host %s", remote, name, hosts, keep)
				}
			}
		}
		if got := daemonEgressHosts(profile); !slices.ContainsFunc(got, func(h string) bool { return hostKey(h) == hostKey(remote) }) {
			t.Errorf("remote %q: daemonEgressHosts() = %v, want the edge permitted to the daemon", remote, got)
		}
	}
}

func TestWireGuardProfileFor_PutsACarvedEdgeBackInTheTunnel(t *testing.T) {
	profile := cdnEdgeProfile(testCDNEdgeIP)
	// 0.0.0.0/0 minus 104.0.0.0/8, standing in for the old app's endpoint carve.
	profile.WireGuard.ConfigText = "[Interface]\nPrivateKey = YWJjZGVmZw==\nAddress = 10.10.1.3/32\n\n[Peer]\nPublicKey = eHl6MTIzNDU=\nEndpoint = 127.0.0.1:51820\nAllowedIPs = 0.0.0.0/2, 64.0.0.0/3, 96.0.0.0/5, 105.0.0.0/8, 106.0.0.0/7, 108.0.0.0/6, 112.0.0.0/4, 128.0.0.0/1\n"

	got, err := wireGuardProfileFor(profile, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.ConfigText, "104.21.24.200/32") {
		t.Errorf("AllowedIPs lost the shared edge:\n%s", got.ConfigText)
	}

	// A range the user split out of the tunnel stays theirs.
	split := []netip.Prefix{netip.MustParsePrefix("104.21.0.0/16")}
	got, err = wireGuardProfileFor(profile, false, split)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got.ConfigText, "104.21.24.200/32") {
		t.Errorf("re-included an edge inside the user's split range:\n%s", got.ConfigText)
	}
}

func TestConnect_PermitsTheCDNEdgeToTheDaemonBeforeArming(t *testing.T) {
	profile := cdnEdgeProfile(testCDNEdgeName)
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)

	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if !slices.Equal(ks.daemonEgress, []string{testCDNEdgeName}) {
		t.Errorf("SetDaemonEgress() = %v, want [%s]", ks.daemonEgress, testCDNEdgeName)
	}
	if ks.daemonEgressAtEnable != 0 {
		t.Errorf("daemon-only permits set after %d Enable calls, want them in the arm itself", ks.daemonEgressAtEnable)
	}
	if slices.Contains(ks.enableEndpoints, testCDNEdgeName) {
		t.Errorf("Enable() endpoints = %v, want the edge kept out of the all-app permits", ks.enableEndpoints)
	}
}

func TestEngageKillSwitch_WithdrawsDaemonOnlyPermits(t *testing.T) {
	ks := &fakeKillSwitch{daemonEgress: []string{testCDNEdgeName}}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, cdnEdgeProfile(testCDNEdgeName))

	if err := svc.EngageKillSwitch(context.Background(), "", false); err != nil {
		t.Fatalf("EngageKillSwitch: %v", err)
	}
	ks.mu.Lock()
	defer ks.mu.Unlock()
	if ks.daemonEgressCalls == 0 || len(ks.daemonEgress) != 0 {
		t.Errorf("SetDaemonEgress() = %v after %d calls, want withdrawn with no session", ks.daemonEgress, ks.daemonEgressCalls)
	}
}

func TestPermitHosts_RefusesASharedEdge(t *testing.T) {
	ks := &fakeKillSwitch{active: true}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, cdnEdgeProfile(testCDNEdgeIP))
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{"198.51.100.9"}})

	if err := svc.PermitHosts(context.Background(), []string{testCDNEdgeIP}); err == nil {
		t.Fatal("PermitHosts() = nil, want a refusal with only the shared edge asked for")
	}
	if slices.Contains(ks.enableEndpoints, testCDNEdgeIP) {
		t.Errorf("Enable() endpoints = %v, want the shared edge never permitted to every app", ks.enableEndpoints)
	}
}

// The previous version resolved a named edge inside the lock and saved its addresses; after an
// update they must not come back as all-app permits, though the hub and node do.
func TestReconcile_DropsAnOlderVersionsUnvouchedAddresses(t *testing.T) {
	profile := cdnEdgeProfile(testCDNEdgeName)
	profile.TransportEndpointIPs = []string{"192.0.2.50"}
	profile.WireGuard.BypassHosts = []string{"198.51.100.7"}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	stubLegacyKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true, EndpointIPs: []string{testCDNEdgeIP, "172.67.220.125", "192.0.2.50", "198.51.100.7"}})

	svc.reconcilePersistedKillSwitch(context.Background())

	for _, edge := range []string{testCDNEdgeIP, "172.67.220.125"} {
		if slices.Contains(ks.enableEndpoints, edge) {
			t.Errorf("re-armed %v, permitting the old version's resolved edge %s to every app", ks.enableEndpoints, edge)
		}
	}
	for _, keep := range []string{"192.0.2.50", "198.51.100.7"} {
		if !slices.Contains(ks.enableEndpoints, keep) {
			t.Errorf("re-armed %v, lost vouched %s", ks.enableEndpoints, keep)
		}
	}

	ks2 := &fakeKillSwitch{active: true}
	svc2 := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks2, profile)
	stubLegacyKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{testCDNEdgeIP, "198.51.100.7"}})
	if err := svc2.PermitHosts(context.Background(), []string{"198.51.100.7"}); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(ks2.enableEndpoints, testCDNEdgeIP) {
		t.Errorf("PermitHosts re-armed %v, carrying the old version's edge along", ks2.enableEndpoints)
	}
}

// The boot lock armed an older version's state with no permits and saved that as current;
// the recorded session must still get its own node back, or it can never reconnect.
func TestReconcile_RestoresTheRecordedSessionsPermitsAfterAnEmptyBootLock(t *testing.T) {
	profile := cdnEdgeProfile(testCDNEdgeName)
	profile.TransportEndpointIPs = []string{"192.0.2.50"}
	profile.WireGuard.BypassHosts = []string{"198.51.100.7"}
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	stubSessionRecordStore(t).set(sessionRecord{ProfileID: profile.ID})
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true})

	svc.reconcilePersistedKillSwitch(context.Background())

	for _, want := range []string{"192.0.2.50", "198.51.100.7"} {
		if !slices.Contains(ks.enableEndpoints, want) {
			t.Errorf("re-armed %v, want the recorded session's %s", ks.enableEndpoints, want)
		}
	}
	if slices.Contains(ks.enableEndpoints, testCDNEdgeName) {
		t.Errorf("re-armed %v with the shared edge", ks.enableEndpoints)
	}
}

func TestArmBootLock_AnOlderVersionsLockArmsWithNoPermits(t *testing.T) {
	ks := &fakeKillSwitch{}
	stubLegacyKillSwitchState(t, platform.KillSwitchState{Active: true, Locked: true, EndpointIPs: []string{testCDNEdgeIP, "198.51.100.9"}})

	if err := ArmBootLock(context.Background(), ks); err != nil {
		t.Fatal(err)
	}
	if ks.enableCount != 1 || len(ks.enableEndpoints) != 0 || !ks.enableLocked {
		t.Fatalf("Enable calls=%d endpoints=%v locked=%v, want a block-all lock until the daemon vouches", ks.enableCount, ks.enableEndpoints, ks.enableLocked)
	}
}

func TestPinCDNEdge_FailsClosed(t *testing.T) {
	orig := acquireEdgePin
	t.Cleanup(func() { acquireEdgePin = orig })
	acquireEdgePin = func(context.Context, string) (*nicpin.Pin, error) { return nil, errors.New("no physical interface") }

	if _, err := pinCDNEdge(context.Background(), testCDNEdgeName); err == nil {
		t.Error("pinCDNEdge() = nil error, want the dial refused rather than left unpinned")
	}
	if _, err := pinTectonicEdge(context.Background(), testCDNEdgeName); err == nil {
		t.Error("pinTectonicEdge() = nil error, want the dial refused rather than left unpinned")
	}
}
