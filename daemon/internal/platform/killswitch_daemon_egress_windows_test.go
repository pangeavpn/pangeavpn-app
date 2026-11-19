//go:build windows && (amd64 || arm64)

package platform

import (
	"errors"
	"slices"
	"testing"
)

const (
	daemonEgressName = "PangeaVPN Allow Daemon Egress "
	endpointName     = "PangeaVPN Allow Endpoint "
	sharedEdgeA      = "104.21.24.200"
	sharedEdgeB      = "172.67.220.125"
)

// endpointPermits counts the all-app endpoint filters for ip, outbound and inbound.
func (h *windowsSplitHarness) endpointPermits(ip string) int {
	return len(h.bfe.withPrefix(endpointName+ip)) + len(h.bfe.withPrefix(endpointName+"Inbound "+ip))
}

func (h *windowsSplitHarness) setDaemonEgress(hosts ...string) error {
	return h.ks.SetDaemonEgress(h.t.Context(), hosts, nil)
}

// assertDaemonEgress checks the engine holds app-scoped permits for exactly ips, and that
// none of them also has an endpoint permit, which would let every app reach it.
func (h *windowsSplitHarness) assertDaemonEgress(ips ...string) {
	h.t.Helper()
	var want []uint64
	for _, ip := range ips {
		filters := h.bfe.named(daemonEgressName + ip)
		if len(filters) != 2 {
			h.t.Fatalf("daemon egress %s has %d filters, want the CONNECT and RECV_ACCEPT pair", ip, len(filters))
		}
		for _, f := range filters {
			assertDaemonEgressFilter(h.t, f, ip)
		}
		want = append(want, filterIDs(filters)...)
		if all := h.endpointPermits(ip); all != 0 {
			h.t.Fatalf("shared edge %s also has %d all-app endpoint permits", ip, all)
		}
	}
	if got := len(h.bfe.withPrefix(daemonEgressName)); got != 2*len(ips) {
		h.t.Fatalf("%d daemon egress filters installed, want %d", got, 2*len(ips))
	}
	slices.Sort(want)
	if !slices.Equal(want, sortedIDs(h.ks.daemonEgressFilterIds)) {
		h.t.Fatalf("tracked daemon egress ids %v, installed %v", h.ks.daemonEgressFilterIds, want)
	}
	if !slices.Equal(h.ks.appliedSplit.DaemonIPs, ips) && !(len(ips) == 0 && len(h.ks.appliedSplit.DaemonIPs) == 0) {
		h.t.Fatalf("appliedSplit.DaemonIPs = %v, want %v", h.ks.appliedSplit.DaemonIPs, ips)
	}
}

func assertDaemonEgressFilter(t *testing.T, f *fakeFilter, ip string) {
	t.Helper()
	if f.weight != weightSplitPermit || f.action != cFWP_ACTION_PERMIT || f.sublayer != pangeaVPNSublayerKey || !isEphemeralFilter(&wtFwpmFilter0{flags: f.flags}) {
		t.Fatalf("daemon egress %q: weight=%d action=%#x flags=%#x, want an ephemeral permit under the DNS block", f.name, f.weight, f.action, f.flags)
	}
	wantAddr, err := parseV4CIDRAddrMask(ip + "/32")
	if err != nil {
		t.Fatal(err)
	}
	wantConds := 2
	switch f.layer {
	case cFWPM_LAYER_ALE_AUTH_CONNECT_V4:
	case cFWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4:
		wantConds = 3
	default:
		t.Fatalf("daemon egress %q on layer %v", f.name, f.layer)
	}
	if len(f.conditions) != wantConds {
		t.Fatalf("daemon egress %q has %d conditions, want %d", f.name, len(f.conditions), wantConds)
	}
	app, remote := f.conditions[0], f.conditions[1]
	if app.field != cFWPM_CONDITION_ALE_APP_ID || app.match != cFWP_MATCH_EQUAL || app.kind != cFWP_BYTE_BLOB_TYPE || app.blob != fakeDaemonAppID {
		t.Fatalf("daemon egress %q app condition = %+v, want ALE_APP_ID equal to the daemon image", f.name, app)
	}
	if remote.field != cFWPM_CONDITION_IP_REMOTE_ADDRESS || remote.kind != cFWP_V4_ADDR_MASK || remote.addr != wantAddr {
		t.Fatalf("daemon egress %q remote condition = %+v, want %s/32", f.name, remote, ip)
	}
	if wantConds == 3 {
		udp := f.conditions[2]
		if udp.field != cFWPM_CONDITION_IP_PROTOCOL || udp.kind != cFWP_UINT8 || udp.value != uint64(cIPPROTO_UDP) {
			t.Fatalf("inbound daemon egress %q protocol condition = %+v, want UDP only", f.name, udp)
		}
	}
}

func TestWindowsDaemonEgress_FreshArmScopesSharedEdgesToTheDaemon(t *testing.T) {
	h := newWindowsSplitHarness(t)
	if err := h.setDaemonEgress(sharedEdgeA, "10.0.0.1", "127.0.0.1", "100.64.1.1"); err != nil {
		t.Fatal(err)
	}
	// An older caller still passes the edge as an endpoint; it must not become an all-app permit.
	h.arm([]string{"203.0.113.5", sharedEdgeA}, false)

	h.assertDaemonEgress(sharedEdgeA)
	if h.endpointPermits("203.0.113.5") != 2 {
		t.Fatal("the dedicated endpoint lost its permit")
	}
	lock := transactions(h.bfe.ops)[0]
	if opIndex(lock, "add "+daemonEgressName+sharedEdgeA) < opIndex(lock, "add PangeaVPN Block All Outbound") {
		t.Fatalf("daemon egress permit is not inside the lock transaction: %v", lock)
	}
}

func TestWindowsDaemonEgress_SwapsOnAnArmedLockAndClearForgetsIt(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)

	if err := h.setDaemonEgress(sharedEdgeA); err != nil {
		t.Fatal(err)
	}
	h.assertDaemonEgress(sharedEdgeA)
	if err := h.setDaemonEgress(sharedEdgeB); err != nil {
		t.Fatal(err)
	}
	h.assertDaemonEgress(sharedEdgeB)

	// A re-arm swaps the endpoints without touching the daemon-only set.
	h.arm([]string{"203.0.113.6", sharedEdgeB}, false)
	h.assertDaemonEgress(sharedEdgeB)

	if err := h.setDaemonEgress(); err != nil {
		t.Fatal(err)
	}
	h.assertDaemonEgress()

	if err := h.setDaemonEgress(sharedEdgeA); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.disarm(); err != nil {
		t.Fatal(err)
	}
	if len(h.ks.split.DaemonIPs) != 0 || len(h.ks.daemonEgressFilterIds) != 0 || len(h.ks.shared) != 0 {
		t.Fatalf("after Clear: desired %v, tracked %v, shared %v; a later lock must not inherit them", h.ks.split.DaemonIPs, h.ks.daemonEgressFilterIds, h.ks.shared)
	}
	// Once cleared, an address a past edge resolved to can be a dedicated endpoint again.
	h.arm([]string{sharedEdgeA}, false)
	if h.endpointPermits(sharedEdgeA) != 2 {
		t.Fatal("an address a cleared session's edge had used stayed barred from a new lock")
	}
}

// Without the daemon's app id there is no way to scope the permit, so the edge stays blocked.
func TestWindowsDaemonEgress_FailsClosedWithoutAnAppID(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5"}, false)
	h.appIDErr = errors.New("image path not found")

	if err := h.setDaemonEgress(sharedEdgeA); err == nil {
		t.Fatal("SetDaemonEgress() = nil, want the failure to name the missing app id")
	}
	if got := h.bfe.withPrefix(daemonEgressName); len(got) != 0 {
		t.Fatalf("%d daemon egress filters installed without an app id", len(got))
	}
	if h.endpointPermits(sharedEdgeA) != 0 {
		t.Fatal("the edge fell back to an all-app permit")
	}
	// The failed swap narrowed what is wanted, but the edge is still known to be shared.
	h.arm([]string{"203.0.113.5", sharedEdgeA}, false)
	if h.endpointPermits(sharedEdgeA) != 0 {
		t.Fatal("a re-arm after the failed swap permitted the edge to every app")
	}
}

// A lock armed before the edge was known to be shared (an older version's state, say)
// must lose its all-app permit for it, in the engine and on disk, once the daemon learns.
func TestWindowsDaemonEgress_RetiresAnAllAppPermitTheEdgeAlreadyHad(t *testing.T) {
	h := newWindowsSplitHarness(t)
	h.arm([]string{"203.0.113.5", sharedEdgeA}, false)
	if h.endpointPermits(sharedEdgeA) == 0 {
		t.Fatal("setup: the edge should start as an endpoint")
	}

	if err := h.setDaemonEgress(sharedEdgeA); err != nil {
		t.Fatal(err)
	}
	h.assertDaemonEgress(sharedEdgeA)
	if h.endpointPermits("203.0.113.5") != 2 {
		t.Fatal("the dedicated endpoint lost its permit")
	}
	st, err := loadKillSwitchState()
	if err != nil {
		t.Fatal(err)
	}
	if slices.Contains(st.EndpointIPs, sharedEdgeA) || st.Version != KillSwitchStateVersion {
		t.Fatalf("saved state = %+v, want the edge gone and the current version", st)
	}
}
