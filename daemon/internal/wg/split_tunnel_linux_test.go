//go:build linux

package wg

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func resetLinuxPolicyRouteRefs(t *testing.T, refs map[string]int) {
	t.Helper()
	linuxPolicyRouteMu.Lock()
	linuxPolicyRouteRefs = refs
	linuxPolicyRouteMu.Unlock()
	t.Cleanup(func() {
		linuxPolicyRouteMu.Lock()
		linuxPolicyRouteRefs = map[string]int{}
		linuxPolicyRouteMu.Unlock()
	})
}

func linuxPolicyRouteRefsSnapshot() map[string]int {
	linuxPolicyRouteMu.Lock()
	defer linuxPolicyRouteMu.Unlock()
	return maps.Clone(linuxPolicyRouteRefs)
}

type routeOpRecorder struct {
	ops    []string
	failOn string
}

func (r *routeOpRecorder) add(rp string) error {
	if rp == r.failOn {
		return errors.New("route add failed")
	}
	r.ops = append(r.ops, "add "+rp)
	return nil
}

func (r *routeOpRecorder) del(rp string) { r.ops = append(r.ops, "del "+rp) }

func TestDiffLinuxPolicyRoutes_AddsNewRoutesBeforeDroppingOldOnes(t *testing.T) {
	resetLinuxPolicyRouteRefs(t, map[string]int{"0.0.0.0/1": 1, "128.0.0.0/1": 1})
	rec := &routeOpRecorder{}

	want := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3"}
	tracked, err := diffLinuxPolicyRoutes([]string{"0.0.0.0/0"}, want, rec.add, rec.del)
	if err != nil {
		t.Fatal(err)
	}
	if wantOps := []string{"add 128.0.0.0/2", "add 192.0.0.0/3", "del 128.0.0.0/1"}; !slices.Equal(rec.ops, wantOps) {
		t.Fatalf("ops = %v, want %v", rec.ops, wantOps)
	}
	if !slices.Equal(tracked, want) {
		t.Fatalf("tracked = %v, want %v", tracked, want)
	}
	if got, wantRefs := linuxPolicyRouteRefsSnapshot(), map[string]int{"0.0.0.0/1": 1, "128.0.0.0/2": 1, "192.0.0.0/3": 1}; !maps.Equal(got, wantRefs) {
		t.Fatalf("refs = %v, want %v", got, wantRefs)
	}
}

// A second session still routing the old prefix keeps it in the kernel.
func TestDiffLinuxPolicyRoutes_LeavesARouteAnotherSessionHolds(t *testing.T) {
	resetLinuxPolicyRouteRefs(t, map[string]int{"0.0.0.0/1": 2, "128.0.0.0/1": 2})
	rec := &routeOpRecorder{}

	if _, err := diffLinuxPolicyRoutes([]string{"0.0.0.0/1", "128.0.0.0/1"}, []string{"0.0.0.0/1"}, rec.add, rec.del); err != nil {
		t.Fatal(err)
	}
	if len(rec.ops) != 0 {
		t.Fatalf("ops = %v; the shared route must stay", rec.ops)
	}
	if got, wantRefs := linuxPolicyRouteRefsSnapshot(), map[string]int{"0.0.0.0/1": 2, "128.0.0.0/1": 1}; !maps.Equal(got, wantRefs) {
		t.Fatalf("refs = %v, want %v", got, wantRefs)
	}
}

func TestDiffLinuxPolicyRoutes_FailedAddKeepsEverythingAndConvergesOnRetry(t *testing.T) {
	resetLinuxPolicyRouteRefs(t, map[string]int{"0.0.0.0/1": 1, "128.0.0.0/1": 1})
	rec := &routeOpRecorder{failOn: "192.0.0.0/3"}

	old := []string{"0.0.0.0/1", "128.0.0.0/1"}
	want := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3"}
	tracked, err := diffLinuxPolicyRoutes(old, want, rec.add, rec.del)
	if err == nil {
		t.Fatal("expected the add failure")
	}
	if wantOps := []string{"add 128.0.0.0/2"}; !slices.Equal(rec.ops, wantOps) {
		t.Fatalf("ops = %v, want %v and no deletes", rec.ops, wantOps)
	}
	if wantTracked := []string{"0.0.0.0/1", "128.0.0.0/1", "0.0.0.0/1", "128.0.0.0/2"}; !slices.Equal(tracked, wantTracked) {
		t.Fatalf("tracked = %v, want %v", tracked, wantTracked)
	}

	rec = &routeOpRecorder{}
	tracked, err = diffLinuxPolicyRoutes(tracked, want, rec.add, rec.del)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(tracked, want) {
		t.Fatalf("tracked = %v, want %v", tracked, want)
	}
	if got, wantRefs := linuxPolicyRouteRefsSnapshot(), map[string]int{"0.0.0.0/1": 1, "128.0.0.0/2": 1, "192.0.0.0/3": 1}; !maps.Equal(got, wantRefs) {
		t.Fatalf("refs after the retry = %v, want %v", got, wantRefs)
	}
	if wantOps := []string{"add 192.0.0.0/3", "del 128.0.0.0/1"}; !slices.Equal(rec.ops, wantOps) {
		t.Fatalf("retry ops = %v, want %v", rec.ops, wantOps)
	}
}

func TestDiffLinuxPolicyRoutes_InvalidWantTouchesNothing(t *testing.T) {
	resetLinuxPolicyRouteRefs(t, map[string]int{"0.0.0.0/1": 1})
	rec := &routeOpRecorder{}
	tracked, err := diffLinuxPolicyRoutes([]string{"0.0.0.0/1"}, []string{"10.0.0.0/8", "bogus"}, rec.add, rec.del)
	if err == nil || len(rec.ops) != 0 || !slices.Equal(tracked, []string{"0.0.0.0/1"}) {
		t.Fatalf("err = %v ops = %v tracked = %v", err, rec.ops, tracked)
	}
}

func table51820IPv4(t *testing.T, linkIndex int) []string {
	t.Helper()
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: policyRoutingTable}, netlink.RT_FILTER_TABLE)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range routes {
		if r.LinkIndex != linkIndex {
			t.Errorf("route %s on link %d, want the tunnel's %d", r.Dst, r.LinkIndex, linkIndex)
		}
		out = append(out, r.Dst.String())
	}
	slices.Sort(out)
	return out
}

// requireThrowawayNetns lets live tests run only as root inside a namespace made for them, e.g.
// PANGEA_WG_NETNS_TEST=1 ip netns exec <ns> x.test -test.run Live.
func requireThrowawayNetns(t *testing.T) {
	t.Helper()
	if os.Getenv("PANGEA_WG_NETNS_TEST") != "1" || os.Geteuid() != 0 {
		t.Skip("set PANGEA_WG_NETNS_TEST=1 and run as root inside a throwaway network namespace")
	}
	self, _ := os.Readlink("/proc/self/ns/net")
	root, _ := os.Readlink("/proc/1/ns/net")
	if self == "" || self == root {
		t.Skip("refusing to touch the root network namespace")
	}
	t.Setenv("PANGEA_APP_SUPPORT_DIR", t.TempDir())
	resetLinuxPolicyRouteRefs(t, map[string]int{})
}

func TestApplyAllowedIPs_LinuxLiveMovesTable51820InPlace(t *testing.T) {
	requireThrowawayNetns(t)

	const name = "pgwgsplit0"
	if err := netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: name}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = deleteLinuxInterface(name) })
	link, err := netlink.LinkByName(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := netlink.LinkSetUp(link); err != nil {
		t.Fatal(err)
	}

	initial := []string{"0.0.0.0/0"}
	if err := addLinuxPolicyRouting(name, initial); err != nil {
		t.Fatal(err)
	}
	dev := newTestDevice(t, liveSplitConfig)
	session := &tunnelSession{interfaceName: name, device: dev, linuxAllowedIPs: initial}
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	key := sanitizeTunnelName("pangea0")
	m.storeSession(key, session)
	storeLinuxExtra(key, &linuxSessionExtra{})
	t.Cleanup(func() {
		takeLinuxExtra(key)
		removeLinuxPolicyRouting(name, session.linuxAllowedIPs)
	})

	if got, want := table51820IPv4(t, link.Attrs().Index), []string{"0.0.0.0/1", "128.0.0.0/1"}; !slices.Equal(got, want) {
		t.Fatalf("baseline table = %v, want %v", got, want)
	}

	excluded := strings.Replace(liveSplitConfig, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 0.0.0.0/1, 128.0.0.0/2, 192.0.0.0/3", 1)
	if err := m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{TunnelName: "pangea0", ConfigText: excluded}); err != nil {
		t.Fatal(err)
	}
	want := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3"}
	if got := table51820IPv4(t, link.Attrs().Index); !slices.Equal(got, want) {
		t.Fatalf("table = %v, want %v", got, want)
	}
	if allowed, endpoint := deviceAllowedIPs(t, dev); !slices.Equal(allowed, want) || endpoint != "127.0.0.1:7" {
		t.Fatalf("device allowed = %v endpoint %q", allowed, endpoint)
	}
	if !slices.Equal(session.linuxAllowedIPs, want) {
		t.Fatalf("session tracks %v, want %v", session.linuxAllowedIPs, want)
	}
	path, err := linuxSessionStateFile(key)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var persisted linuxPersistedSession
	if err := json.Unmarshal(data, &persisted); err != nil || !slices.Equal(persisted.AllowedIPs, want) || persisted.InterfaceName != name {
		t.Fatalf("persisted = %+v, %v", persisted, err)
	}
	v6, err := netlink.RouteListFiltered(netlink.FAMILY_V6, &netlink.Route{Table: policyRoutingTable}, netlink.RT_FILTER_TABLE)
	if err != nil || len(v6) != 1 {
		t.Fatalf("the IPv6 blackhole must survive an apply: %v, %v", v6, err)
	}

	if err := m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{TunnelName: "pangea0", ConfigText: liveSplitConfig}); err != nil {
		t.Fatal(err)
	}
	if got, back := table51820IPv4(t, link.Attrs().Index), []string{"0.0.0.0/1", "128.0.0.0/1"}; !slices.Equal(got, back) {
		t.Fatalf("table after undo = %v, want %v", got, back)
	}

	removeLinuxPolicyRouting(name, session.linuxAllowedIPs)
	session.linuxAllowedIPs = nil
	if got := table51820IPv4(t, link.Attrs().Index); len(got) != 0 {
		t.Fatalf("teardown left %v in table 51820", got)
	}
}

// A real TUN through startLinux: the session keeps the hook's wrapper, and a live apply moves the
// routes without re-creating the device.
func TestStartLinux_LiveWrapsTheTUNAndAppliesAllowedIPsInPlace(t *testing.T) {
	requireThrowawayNetns(t)
	hook := &fakeSplitHook{}
	m := newWireGuardGoManager(state.NewLogStore(256))
	m.SetSplitTunnelHook(hook)

	config := strings.Replace(liveSplitConfig, "Endpoint = 127.0.0.1:7\n", "", 1)
	profile := state.WireGuardProfile{TunnelName: "pgwgsplit1", ConfigText: config}
	if err := m.Start(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Stop(context.Background(), profile) })

	session, ok := m.session(sanitizeTunnelName(profile.TunnelName))
	if !ok || len(hook.wrapped) != 1 || session.tunDevice != tun.Device(hook.wrapped[0]) {
		t.Fatalf("session tun = %T, wraps = %d; want the hook's wrapper", session.tunDevice, len(hook.wrapped))
	}
	wrapper := hook.wrapped[0]
	if info := wrapper.info; info.Name != session.interfaceName || info.MTU != device.DefaultMTU ||
		!slices.Equal(info.Addresses, []netip.Addr{netip.MustParseAddr("10.7.0.2")}) || len(info.DNS) != 0 {
		t.Fatalf("info = %+v, want %s with the config's address and the default MTU", info, session.interfaceName)
	}
	link, err := netlink.LinkByName(session.interfaceName)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := table51820IPv4(t, link.Attrs().Index), []string{"0.0.0.0/1", "128.0.0.0/1"}; !slices.Equal(got, want) {
		t.Fatalf("table = %v, want %v", got, want)
	}

	carved := strings.Replace(config, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 0.0.0.0/1, 128.0.0.0/2, 192.0.0.0/3", 1)
	if err := m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{TunnelName: profile.TunnelName, ConfigText: carved}); err != nil {
		t.Fatal(err)
	}
	if got, want := table51820IPv4(t, link.Attrs().Index), []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3"}; !slices.Equal(got, want) {
		t.Fatalf("table after apply = %v, want %v", got, want)
	}
	if after, _ := m.session(sanitizeTunnelName(profile.TunnelName)); after != session || len(hook.wrapped) != 1 || wrapper.closed.Load() {
		t.Fatal("a live apply must keep the device and its wrapper")
	}

	if err := m.Stop(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	if !wrapper.closed.Load() {
		t.Fatal("stopping the tunnel must close the wrapper")
	}
	routes, err := netlink.RouteListFiltered(netlink.FAMILY_V4, &netlink.Route{Table: policyRoutingTable}, netlink.RT_FILTER_TABLE)
	if err != nil || len(routes) != 0 {
		t.Fatalf("teardown left %d routes in table %d (%v)", len(routes), policyRoutingTable, err)
	}
}
