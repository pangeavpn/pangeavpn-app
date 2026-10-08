package api

import (
	"context"
	"slices"
	"sync"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// hubProxyPermitStub keeps the hub-proxy permit record in memory for a test.
type hubProxyPermitStub struct {
	mu  sync.Mutex
	ips []string
}

func (s *hubProxyPermitStub) get() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.ips)
}

func stubHubProxyPermits(t *testing.T) *hubProxyPermitStub {
	t.Helper()
	stub := &hubProxyPermitStub{}
	origSave, origLoad := saveHubProxyPermits, loadHubProxyPermits
	saveHubProxyPermits = func(ips []string) error {
		stub.mu.Lock()
		defer stub.mu.Unlock()
		stub.ips = slices.Clone(ips)
		return nil
	}
	loadHubProxyPermits = func() ([]string, error) { return stub.get(), nil }
	t.Cleanup(func() { saveHubProxyPermits, loadHubProxyPermits = origSave, origLoad })
	return stub
}

func TestSessionKillSwitchPermits_RecordsWhatOnlyAHubProxyNeeds(t *testing.T) {
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, testProfile(), vouchingProfile())
	record := stubHubProxyPermits(t)
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
	svc.sessionKillSwitchPermits(testProfile(), false)
	if got := record.get(); !slices.Equal(got, []string{testProxyNode}) {
		t.Fatalf("record = %v, want the hub proxy's node", got)
	}

	svc.SetRealityProxy(&fakeRealityProxy{})
	svc.sessionKillSwitchPermits(testProfile(), false)
	if got := record.get(); len(got) != 0 {
		t.Fatalf("record = %v after the proxy stopped, want empty", got)
	}
}

func TestSessionKillSwitchPermits_NeverRecordsTheSessionsOwnNode(t *testing.T) {
	profile := testProfile()
	profile.TransportEndpointIPs = []string{testProxyNode}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, profile)
	record := stubHubProxyPermits(t)
	svc.SetRealityProxy(&fakeRealityProxy{port: 41000, remote: testProxyNode})
	svc.sessionKillSwitchPermits(profile, false)
	if got := record.get(); len(got) != 0 {
		t.Fatalf("record = %v; the session's own node is not there for the hub proxy", got)
	}
}

func TestStartRealityProxy_RecordsItsNode(t *testing.T) {
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{}, testProfile(), vouchingProfile())
	record := stubHubProxyPermits(t)
	svc.SetRealityProxy(&fakeRealityProxy{})
	if _, err := svc.StartRealityProxy(context.Background(), realityHubProfile(testProxyNode)); err != nil {
		t.Fatalf("StartRealityProxy: %v", err)
	}
	if got := record.get(); !slices.Contains(got, testProxyNode) {
		t.Fatalf("record = %v, want the proxy's node so a crash cannot leave it permitted", got)
	}
}

// After a crash no hub proxy is running, so a node permitted only for one must
// not come back with the rest of the persisted lock.
func TestReconcile_DropsHubProxyOnlyPermitsAfterACrash(t *testing.T) {
	profile := reachProfile()
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	stubSessionRecordStore(t)
	record := stubHubProxyPermits(t)
	record.ips = []string{testProxyNode, testHubIP}
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{testHubIP, testNodeIP, testProxyNode}})

	svc.reconcilePersistedKillSwitch(context.Background())

	if slices.Contains(ks.enableEndpoints, testProxyNode) {
		t.Fatalf("re-applied %v, still permitting the hub proxy's node", ks.enableEndpoints)
	}
	for _, keep := range []string{testHubIP, testNodeIP} {
		if !slices.Contains(ks.enableEndpoints, keep) {
			t.Fatalf("re-applied %v, lost %s", ks.enableEndpoints, keep)
		}
	}
	if got := record.get(); len(got) != 0 {
		t.Fatalf("record = %v after reconcile, want it cleared", got)
	}
}

func TestReconcile_KeepsTheRecordedSessionsOwnNode(t *testing.T) {
	profile := reachProfile()
	ks := &fakeKillSwitch{}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	sessions := stubSessionRecordStore(t)
	sessions.set(sessionRecord{ProfileID: profile.ID})
	record := stubHubProxyPermits(t)
	record.ips = []string{testNodeIP}
	stubKillSwitchState(t, platform.KillSwitchState{Active: true, EndpointIPs: []string{testHubIP, testNodeIP}})

	svc.reconcilePersistedKillSwitch(context.Background())

	if !slices.Contains(ks.enableEndpoints, testNodeIP) {
		t.Fatalf("re-applied %v; the recorded session's own node must survive to reconnect", ks.enableEndpoints)
	}
}

func realityHubProfile(host string) state.RealityProfile {
	return state.RealityProfile{RemoteHost: host, RemotePort: 443, UUID: "hub-user", PublicKey: "pub", ServerName: "swdist.apple.com"}
}
