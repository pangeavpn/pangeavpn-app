//go:build darwin

package wg

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"

	"golang.zx2c4.com/wireguard/conn/bindtest"
	"golang.zx2c4.com/wireguard/device"
	"golang.zx2c4.com/wireguard/tun/tuntest"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

// The guard runs before any IpcSet or route exec, so this never touches the host.
func TestTrySwitchInPlaceDarwin_RebuildsWhenTheTunnelAddressChanged(t *testing.T) {
	wrapper := &fakeSplitWrapper{Device: tuntest.NewChannelTUN().TUN(), info: TunnelInfo{Addresses: []netip.Addr{netip.MustParseAddr("10.7.0.9")}}}
	dev := device.NewDevice(wrapper, bindtest.NewChannelBinds()[0], device.NewLogger(device.LogLevelSilent, ""))
	t.Cleanup(dev.Close)

	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	key := sanitizeTunnelName("pangea0")
	m.storeSession(key, &tunnelSession{interfaceName: "utun9", device: dev, tunDevice: wrapper, deviceMTU: device.DefaultMTU})
	storeDarwinExtra(key, &darwinSessionExtra{allowedIPs: []string{"0.0.0.0/0"}})
	t.Cleanup(func() { takeDarwinExtra(key) })

	if m.trySwitchInPlaceDarwin(context.Background(), key, splitTestConfig(t, ""), []string{"0.0.0.0/0"}) {
		t.Fatal("a new tunnel address must fall back to a rebuild, which re-wraps the TUN")
	}
	logged := false
	for _, entry := range m.logs.Since(0) {
		logged = logged || strings.Contains(entry.Msg, "tunnel address changed")
	}
	if !logged || len(wrapper.updates) != 0 {
		t.Fatalf("logged=%t updates=%v", logged, wrapper.updates)
	}
}

type darwinRouteStub struct {
	ops    []string
	failOn string
	onAdd  func()
}

func stubDarwinAllowedIPRoutes(t *testing.T, failOn string) *darwinRouteStub {
	t.Helper()
	stub := &darwinRouteStub{failOn: failOn}
	prevAdd, prevRemove := addDarwinAllowedIPRoutesFn, removeDarwinAllowedIPRoutesFn
	addDarwinAllowedIPRoutesFn = func(_ context.Context, iface string, prefixes []string) error {
		for _, p := range prefixes {
			if p == stub.failOn {
				return errors.New("route add failed")
			}
			stub.ops = append(stub.ops, "add "+iface+" "+p)
			if stub.onAdd != nil {
				stub.onAdd()
			}
		}
		return nil
	}
	removeDarwinAllowedIPRoutesFn = func(iface string, prefixes []string) {
		for _, p := range prefixes {
			stub.ops = append(stub.ops, "remove "+iface+" "+p)
		}
	}
	t.Cleanup(func() { addDarwinAllowedIPRoutesFn, removeDarwinAllowedIPRoutesFn = prevAdd, prevRemove })
	return stub
}

func darwinApplySession(t *testing.T) (*wireGuardGoManager, *tunnelSession, *darwinSessionExtra) {
	t.Helper()
	m := &wireGuardGoManager{logs: state.NewLogStore(64), sessions: map[string]*tunnelSession{}}
	key := sanitizeTunnelName("pangea0")
	session := &tunnelSession{interfaceName: "utun9", device: newTestDevice(t, liveSplitConfig)}
	m.storeSession(key, session)
	extra := &darwinSessionExtra{allowedIPs: []string{"0.0.0.0/0"}}
	storeDarwinExtra(key, extra)
	t.Cleanup(func() { takeDarwinExtra(key) })
	return m, session, extra
}

func TestApplyAllowedIPs_DarwinMovesTheDeviceThenTheRoutes(t *testing.T) {
	stub := stubDarwinAllowedIPRoutes(t, "")
	m, session, extra := darwinApplySession(t)

	carved := strings.Replace(liveSplitConfig, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 0.0.0.0/1, 128.0.0.0/2, 192.0.0.0/3", 1)
	if err := m.ApplyAllowedIPs(context.Background(), state.WireGuardProfile{TunnelName: "pangea0", ConfigText: carved}); err != nil {
		t.Fatal(err)
	}
	want := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3"}
	if allowed, endpoint := deviceAllowedIPs(t, session.device); !slices.Equal(allowed, want) || endpoint != "127.0.0.1:7" {
		t.Fatalf("device allowed = %v endpoint %q", allowed, endpoint)
	}
	if wantOps := []string{"add utun9 128.0.0.0/2", "add utun9 192.0.0.0/3", "remove utun9 128.0.0.0/1"}; !slices.Equal(stub.ops, wantOps) {
		t.Fatalf("route ops = %v, want %v", stub.ops, wantOps)
	}
	if !slices.Equal(extra.allowedIPs, want) {
		t.Fatalf("tracked = %v, want %v", extra.allowedIPs, want)
	}
}

func TestApplyAllowedIPs_DarwinRetryAddsTheRoutesAFailureSkipped(t *testing.T) {
	stub := stubDarwinAllowedIPRoutes(t, "192.0.0.0/3")
	m, _, extra := darwinApplySession(t)

	carved := strings.Replace(liveSplitConfig, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 0.0.0.0/1, 128.0.0.0/2, 192.0.0.0/3", 1)
	profile := state.WireGuardProfile{TunnelName: "pangea0", ConfigText: carved}
	if err := m.ApplyAllowedIPs(context.Background(), profile); err == nil {
		t.Fatal("expected the route failure")
	}
	if wantOps := []string{"add utun9 128.0.0.0/2"}; !slices.Equal(stub.ops, wantOps) {
		t.Fatalf("a failed add must not remove anything: %v", stub.ops)
	}
	if want := []string{"0.0.0.0/0", "128.0.0.0/2"}; !slices.Equal(extra.allowedIPs, want) {
		t.Fatalf("tracked = %v, want only the installed routes %v", extra.allowedIPs, want)
	}

	stub.failOn, stub.ops = "", nil
	if err := m.ApplyAllowedIPs(context.Background(), profile); err != nil {
		t.Fatal(err)
	}
	if wantOps := []string{"add utun9 192.0.0.0/3", "remove utun9 128.0.0.0/1"}; !slices.Equal(stub.ops, wantOps) {
		t.Fatalf("retry route ops = %v, want %v", stub.ops, wantOps)
	}
	if want := []string{"0.0.0.0/1", "128.0.0.0/2", "192.0.0.0/3"}; !slices.Equal(extra.allowedIPs, want) {
		t.Fatalf("tracked = %v, want %v", extra.allowedIPs, want)
	}
}

// Disconnect cancels a live apply; the per-route /sbin/route loop must not run on to the end.
func TestApplyAllowedIPs_DarwinStopsRoutingOnceCancelled(t *testing.T) {
	stub := stubDarwinAllowedIPRoutes(t, "")
	m, _, extra := darwinApplySession(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stub.onAdd = cancel

	carved := strings.Replace(liveSplitConfig, "AllowedIPs = 0.0.0.0/0", "AllowedIPs = 0.0.0.0/1, 128.0.0.0/2, 192.0.0.0/3", 1)
	if err := m.ApplyAllowedIPs(ctx, state.WireGuardProfile{TunnelName: "pangea0", ConfigText: carved}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if wantOps := []string{"add utun9 128.0.0.0/2"}; !slices.Equal(stub.ops, wantOps) {
		t.Fatalf("route ops = %v, want %v", stub.ops, wantOps)
	}
	if want := []string{"0.0.0.0/0", "128.0.0.0/2"}; !slices.Equal(extra.allowedIPs, want) {
		t.Fatalf("tracked = %v, want %v", extra.allowedIPs, want)
	}
}

// A cancelled Connect returns before the first exec, so this never touches the host's routes.
func TestAddDarwinAllowedIPRoutes_StopsOnceCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := addDarwinAllowedIPRoutes(ctx, "utun-test-none", []string{"0.0.0.0/0", "10.0.0.0/8"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
