//go:build windows

package wg

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/windows/tunnel/winipcfg"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

type fakeWindowsSplitWrapper struct {
	*fakeSplitWrapper
	luid uint64
}

func (w fakeWindowsSplitWrapper) ForceMTU(mtu int) { w.Device.(mtuForcer).ForceMTU(mtu) }

func (w fakeWindowsSplitWrapper) LUID() uint64 { return w.luid }

func wrappedAt(addr string) *fakeSplitWrapper {
	return &fakeSplitWrapper{Device: newChannelTun(), info: TunnelInfo{Addresses: []netip.Addr{netip.MustParseAddr(addr)}}}
}

func TestTrySwitchInPlace_RebuildsWhenTheTunnelAddressChanged(t *testing.T) {
	configured := stubConfigureWindowsInterface(t)
	session := newLiveSession(t, wrappedAt("10.7.0.9"), 1420)
	m := newSwitchManager(session)

	if m.trySwitchInPlace(context.Background(), testSwitchTunnelKey, switchConfig(t, ""), nil) {
		t.Fatal("a new tunnel address must fall back to a rebuild, which re-wraps the TUN")
	}
	if len(*configured) != 0 {
		t.Fatalf("the interface was reconfigured before the guard: %v", *configured)
	}
	if !loggedLine(m.logs, "tunnel address changed") {
		t.Fatalf("missing rebuild log line: %+v", m.logs.Since(0))
	}
}

func TestTrySwitchInPlace_HandsTheWrapperTheNewDNSAndMTU(t *testing.T) {
	stubConfigureWindowsInterface(t)
	wrapper := wrappedAt("10.7.0.2")
	session := newLiveSession(t, wrapper, 1420)
	m := newSwitchManager(session)

	parsed := switchConfig(t, "DNS = 10.7.0.1")
	if !m.trySwitchInPlace(context.Background(), testSwitchTunnelKey, parsed, nil) {
		t.Fatalf("same address must switch in place: %+v", m.logs.Since(0))
	}
	if len(wrapper.updates) != 1 {
		t.Fatalf("UpdateTunnelInfo calls = %d, want 1", len(wrapper.updates))
	}
	got := wrapper.updates[0]
	if got.Name != testSwitchTunnelKey || got.MTU != 1420 || !slices.Equal(got.DNS, []netip.Addr{netip.MustParseAddr("10.7.0.1")}) ||
		!slices.Equal(got.Addresses, []netip.Addr{netip.MustParseAddr("10.7.0.2")}) {
		t.Fatalf("UpdateTunnelInfo(%+v)", got)
	}
}

func TestMatchDeviceMTU_ResizesThroughTheSplitWrapper(t *testing.T) {
	inner := &fakeResizableTun{Device: newChannelTun()}
	wrapper := fakeWindowsSplitWrapper{fakeSplitWrapper: &fakeSplitWrapper{Device: inner}}
	session := newLiveSession(t, wrapper, 1420)
	m := newSwitchManager(session)

	if !m.matchDeviceMTU(session, 1280) {
		t.Fatalf("a wrapper that forwards ForceMTU must resize in place: %+v", m.logs.Since(0))
	}
	if !slices.Equal(inner.forced, []int{1280}) {
		t.Fatalf("inner ForceMTU calls = %v, want [1280]", inner.forced)
	}
}

func TestWindowsInterfaceLUID_AsksTheWrapper(t *testing.T) {
	wrapper := fakeWindowsSplitWrapper{fakeSplitWrapper: &fakeSplitWrapper{Device: newChannelTun()}, luid: 77}
	luid, err := windowsInterfaceLUID(wrapper, "no-such-adapter")
	if err != nil || luid != 77 {
		t.Fatalf("windowsInterfaceLUID = %d, %v; want the wrapper's 77", luid, err)
	}
}

type routeSyncCall struct {
	luid       uint64
	addresses  []string
	allowedIPs []string
}

func stubWindowsAllowedIPRoutes(t *testing.T, err error) *[]routeSyncCall {
	t.Helper()
	var calls []routeSyncCall
	prev := syncWindowsAllowedIPRoutesFn
	syncWindowsAllowedIPRoutesFn = func(luid uint64, addresses, allowedIPs []string) error {
		calls = append(calls, routeSyncCall{luid, addresses, allowedIPs})
		return err
	}
	t.Cleanup(func() { syncWindowsAllowedIPRoutesFn = prev })
	return &calls
}

func TestApplyAllowedIPs_WindowsMovesTheDeviceThenTheRoutes(t *testing.T) {
	calls := stubWindowsAllowedIPRoutes(t, nil)
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	dev := newTestDevice(t, liveSplitConfig)
	m.storeSession(sanitizeTunnelName("pangea0"), &tunnelSession{interfaceName: "pangea0", device: dev, windowsLUID: 42})

	config := strings.Replace(liveSplitConfig, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 0.0.0.0/1, 128.0.0.0/2, 192.0.0.0/3", 1)
	if err := m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{TunnelName: "pangea0", ConfigText: config}); err != nil {
		t.Fatal(err)
	}
	want := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3"}
	if allowed, endpoint := deviceAllowedIPs(t, dev); !slices.Equal(allowed, want) || endpoint != "127.0.0.1:7" {
		t.Fatalf("device allowed = %v endpoint %q, want %v on the same endpoint", allowed, endpoint, want)
	}
	if len(*calls) != 1 {
		t.Fatalf("route syncs = %d, want 1", len(*calls))
	}
	call := (*calls)[0]
	if call.luid != 42 || !slices.Equal(call.addresses, []string{"10.7.0.2/32"}) || !slices.Equal(call.allowedIPs, want) {
		t.Fatalf("route sync = %+v", call)
	}
}

// A Stop waits on guardMu before tearing down, so the route sync must hold it; /status only takes m.mu.
func TestApplyAllowedIPs_HoldsTheGuardLockNotTheManagerLock(t *testing.T) {
	started, block := make(chan struct{}), make(chan struct{})
	prev := syncWindowsAllowedIPRoutesFn
	syncWindowsAllowedIPRoutesFn = func(uint64, []string, []string) error {
		close(started)
		<-block
		return nil
	}
	t.Cleanup(func() { syncWindowsAllowedIPRoutesFn = prev })
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	key := sanitizeTunnelName("pangea0")
	m.storeSession(key, &tunnelSession{interfaceName: "pangea0", device: newTestDevice(t, liveSplitConfig), windowsLUID: 42})

	errc := make(chan error, 1)
	go func() {
		errc <- m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{TunnelName: "pangea0", ConfigText: liveSplitConfig})
	}()
	<-started
	guarded := !m.guardMu.TryLock()
	if !guarded {
		m.guardMu.Unlock()
	}
	looked := make(chan struct{})
	go func() { m.session(key); close(looked) }()
	select {
	case <-looked:
	case <-time.After(2 * time.Second):
		t.Error("manager lock held across the route sync; /status would stall")
	}
	close(block)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if !guarded {
		t.Fatal("the route sync ran without guardMu, so a Stop could tear the session down under it")
	}
}

func TestApplyAllowedIPs_WindowsReportsARouteFailure(t *testing.T) {
	stubWindowsAllowedIPRoutes(t, errors.New("add route 1.0.0.0/8: access denied"))
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	m.storeSession(sanitizeTunnelName("pangea0"), &tunnelSession{interfaceName: "pangea0", device: newTestDevice(t, liveSplitConfig), windowsLUID: 42})

	err := m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{TunnelName: "pangea0", ConfigText: liveSplitConfig})
	if err == nil || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("err = %v, want the route failure surfaced for a retry", err)
	}
}

func plannedDestinations(routes []*winipcfg.RouteData) []string {
	out := make([]string, 0, len(routes))
	for _, r := range routes {
		out = append(out, r.Destination.String())
	}
	return out
}

// A live edit adds new routes before deleting old ones, so nothing meant for the tunnel briefly leaves
// it; Windows' Local rows stay even when the profile names another address.
func TestPlanWindowsAllowedIPSync_AddsBeforeDeleting(t *testing.T) {
	const tunnel, other = winipcfg.LUID(53 << 48), winipcfg.LUID(71 << 48)
	table := []winipcfg.MibIPforwardRow2{
		routeRow(t, tunnel, "10.7.0.2/32", "0.0.0.0", 256, winipcfg.RouteProtocolLocal),
		routeRow(t, tunnel, "10.7.0.3/32", "0.0.0.0", 256, winipcfg.RouteProtocolLocal),
		routeRow(t, tunnel, "224.0.0.0/4", "0.0.0.0", 256, winipcfg.RouteProtocolLocal),
		routeRow(t, tunnel, "0.0.0.0/1", "0.0.0.0", 0, winipcfg.RouteProtocolNetMgmt),
		routeRow(t, tunnel, "128.0.0.0/1", "0.0.0.0", 0, winipcfg.RouteProtocolNetMgmt),
		routeRow(t, tunnel, "64.0.0.0/2", "0.0.0.0", 5, winipcfg.RouteProtocolNetMgmt),
		routeRow(t, other, "128.0.0.0/2", "10.3.0.1", 0, winipcfg.RouteProtocolNetMgmt),
	}
	addresses := []netip.Prefix{netip.MustParsePrefix("10.7.0.2/32")}
	addFirst, stale, addAfter := planWindowsAllowedIPSync(table, tunnel, addresses, onLink("0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/2", "64.0.0.0/2"))

	if got, want := plannedDestinations(addFirst), []string{"128.0.0.0/2", "192.0.0.0/2"}; !slices.Equal(got, want) {
		t.Errorf("addFirst = %v, want %v", got, want)
	}
	if got, want := prefixStrings(stale), []string{"128.0.0.0/1/m0", "64.0.0.0/2/m5"}; !slices.Equal(got, want) {
		t.Errorf("stale = %v, want %v", got, want)
	}
	if got, want := plannedDestinations(addAfter), []string{"64.0.0.0/2"}; !slices.Equal(got, want) {
		t.Errorf("addAfter = %v, want %v (a metric fix must follow its delete)", got, want)
	}
}
