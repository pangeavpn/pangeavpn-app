//go:build linux

package wg

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestIsDefaultDst(t *testing.T) {
	tests := []struct {
		name      string
		dst       *net.IPNet
		isDefault bool
	}{
		{name: "nil destination is the default route", dst: nil, isDefault: true},
		{
			name:      "0.0.0.0/0 is the default route",
			dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(0, 32)},
			isDefault: true,
		},
		{
			// The bypass itself: never mistake it for a default.
			name:      "a host route is not the default",
			dst:       &net.IPNet{IP: net.ParseIP("203.0.113.9"), Mask: net.CIDRMask(32, 32)},
			isDefault: false,
		},
		{
			name:      "a half-default is not the default",
			dst:       &net.IPNet{IP: net.IPv4zero, Mask: net.CIDRMask(1, 32)},
			isDefault: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isDefaultDst(tc.dst); got != tc.isDefault {
				t.Errorf("isDefaultDst = %t, want %t", got, tc.isDefault)
			}
		})
	}
}

// TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp proves a session with nothing
// recorded never touches netlink; the guard runs on every health tick.
func TestEnsureSessionEndpointRoutes_NoRoutesIsANoOp(t *testing.T) {
	repaired, err := ensureSessionEndpointRoutes(context.Background(), &tunnelSession{interfaceName: "pangea0"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repaired {
		t.Error("repaired = true with no recorded routes")
	}
}

// TestLinuxBypassGateway_ReadsTheLiveTable exercises the election against
// whatever the machine actually has. It asserts shape rather than a specific
// gateway, since CI runners differ, but a reported gateway must be a usable
// unicast address rather than the unspecified one a tunnel route carries.
func TestLinuxBypassGateway_ReadsTheLiveTable(t *testing.T) {
	gateway, found, err := linuxBypassGateway(0)
	if err != nil {
		t.Fatalf("read default gateway: %v", err)
	}
	if !found {
		t.Skip("no default route with a gateway on this host")
	}
	// A nil gw is valid here: a link-scope default (PPP, mobile broadband)
	// carries no gateway address, matching linuxDefaultGateway's behavior.
	if gateway.gw != nil && gateway.gw.IsUnspecified() {
		t.Errorf("gateway = %v, want nil or a usable unicast address", gateway.gw)
	}
	if gateway.linkIndex <= 0 {
		t.Errorf("linkIndex = %d, want a real interface", gateway.linkIndex)
	}
}

// TestLinuxBypassGateway_SkipsTheTunnelsOwnDefault proves the election cannot
// pick the tunnel it is meant to bypass. Excluding the live default's own link
// must change the answer rather than return it again.
func TestLinuxBypassGateway_SkipsTheTunnelsOwnDefault(t *testing.T) {
	gateway, found, err := linuxBypassGateway(0)
	if err != nil || !found {
		t.Skip("no default route with a gateway on this host")
	}

	excluded, found, err := linuxBypassGateway(gateway.linkIndex)
	if err != nil {
		t.Fatalf("read default gateway: %v", err)
	}
	if found && excluded.linkIndex == gateway.linkIndex {
		t.Errorf("excluded link %d but the election returned it anyway", gateway.linkIndex)
	}
}

func TestGatewaysEqual(t *testing.T) {
	a := net.ParseIP("192.0.2.1")
	b := net.ParseIP("192.0.2.1")
	c := net.ParseIP("192.0.2.2")

	if !gatewaysEqual(nil, nil) {
		t.Error("nil, nil should be equal (both link-scope defaults)")
	}
	if gatewaysEqual(a, nil) || gatewaysEqual(nil, a) {
		t.Error("a real gateway should never equal a link-scope nil")
	}
	if !gatewaysEqual(a, b) {
		t.Error("equal IPs should compare equal")
	}
	if gatewaysEqual(a, c) {
		t.Error("different IPs should not compare equal")
	}
}

func TestRouteOwnershipKey_DistinguishesFamilyAndDestination(t *testing.T) {
	a := routeOwnershipKey(routeSpec{family: "inet", destination: "203.0.113.9"})
	b := routeOwnershipKey(routeSpec{family: "inet6", destination: "203.0.113.9"})
	c := routeOwnershipKey(routeSpec{family: "inet", destination: "203.0.113.10"})
	if a == b || a == c || b == c {
		t.Errorf("keys collided: %q %q %q", a, b, c)
	}
}

// TestLinuxPolicyRouteRefCounting proves a shared prefix survives one
// session's teardown and is deleted only once the last reference releases.
func TestLinuxPolicyRouteRefCounting(t *testing.T) {
	const prefix = "198.51.100.0/24"

	linuxPolicyRouteMu.Lock()
	linuxPolicyRouteRefs = map[string]int{}
	linuxPolicyRouteMu.Unlock()

	incrementLinuxPolicyRouteRef(prefix)
	incrementLinuxPolicyRouteRef(prefix)

	if decrementLinuxPolicyRouteRef(prefix) {
		t.Fatal("released too early: another session still holds a reference")
	}
	if !decrementLinuxPolicyRouteRef(prefix) {
		t.Fatal("should release on the last reference")
	}
	if decrementLinuxPolicyRouteRef(prefix) {
		t.Error("decrementing an already-released prefix should not report a release")
	}
}

// TestAtomicReplaceFile_ReplacesLinkNotTarget proves the write swaps the
// directory entry rather than following a generator's symlink to its target.
func TestAtomicReplaceFile_ReplacesLinkNotTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "stub-resolv.conf")
	link := filepath.Join(dir, "resolv.conf")

	if err := os.WriteFile(target, []byte("nameserver 127.0.0.53\n"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("create symlink: %v", err)
	}

	if err := atomicReplaceFile(link, []byte("nameserver 10.0.0.1\n"), 0o644); err != nil {
		t.Fatalf("atomicReplaceFile: %v", err)
	}

	info, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Error("link should have been replaced by a regular file, not left as a symlink")
	}

	targetData, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(targetData) != "nameserver 127.0.0.53\n" {
		t.Errorf("target content changed: %q", targetData)
	}

	linkData, err := os.ReadFile(link)
	if err != nil {
		t.Fatalf("read link: %v", err)
	}
	if string(linkData) != "nameserver 10.0.0.1\n" {
		t.Errorf("link content = %q, want the new content", linkData)
	}
}

func TestLinuxPersistedDNS_RoundTrip(t *testing.T) {
	override := &linuxDNSOverride{
		mode:           linuxDNSModeResolvConf,
		interfaceName:  "pangea0",
		resolvConfPath: "/etc/resolv.conf",
		resolvConfData: []byte("nameserver 1.1.1.1\n"),
		resolvConfMode: 0o644,
		resolvConfHad:  true,
	}
	backup := linuxResolvSymlinkBackup{wasSymlink: true, target: "../run/systemd/resolve/stub-resolv.conf"}

	persisted := newLinuxPersistedDNS(override, backup)
	data, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var roundTripped linuxPersistedDNS
	if err := json.Unmarshal(data, &roundTripped); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	gotOverride, gotBackup := roundTripped.toOverride()
	if gotOverride.mode != override.mode || gotOverride.interfaceName != override.interfaceName ||
		gotOverride.resolvConfPath != override.resolvConfPath || string(gotOverride.resolvConfData) != string(override.resolvConfData) ||
		gotOverride.resolvConfMode != override.resolvConfMode || gotOverride.resolvConfHad != override.resolvConfHad {
		t.Errorf("override round-trip mismatch: got %+v, want %+v", gotOverride, override)
	}
	if gotBackup != backup {
		t.Errorf("backup round-trip mismatch: got %+v, want %+v", gotBackup, backup)
	}
}

// TestPersistLinuxSessionState_RoundTrip proves a session's pre-state
// survives the on-disk format restoreOrphanedLinuxNetworkState relies on.
func TestPersistLinuxSessionState_RoundTrip(t *testing.T) {
	t.Setenv("PANGEA_APP_SUPPORT_DIR", t.TempDir())

	routes := []routeSpec{{family: "inet", destination: "203.0.113.9"}}
	ownership := map[string]routeOwnership{
		routeOwnershipKey(routes[0]): {ownsMain: true, ownsPolicy: false},
	}
	persisted := newLinuxPersistedSession("pangea0", []string{"0.0.0.0/1", "128.0.0.0/1"}, routes, ownership, nil, linuxResolvSymlinkBackup{})

	const tunnelKey = "test-tunnel"
	if err := persistLinuxSessionState(tunnelKey, persisted); err != nil {
		t.Fatalf("persist: %v", err)
	}

	path, err := linuxSessionStateFile(tunnelKey)
	if err != nil {
		t.Fatalf("state file path: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read state file: %v", err)
	}

	var loaded linuxPersistedSession
	if err := json.Unmarshal(data, &loaded); err != nil {
		t.Fatalf("unmarshal state file: %v", err)
	}
	if loaded.InterfaceName != "pangea0" || len(loaded.EndpointRoutes) != 1 || !loaded.EndpointRoutes[0].OwnsMain || loaded.EndpointRoutes[0].OwnsPolicy {
		t.Errorf("loaded session mismatch: %+v", loaded)
	}

	clearLinuxSessionState(tunnelKey)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("state file should be gone after clear, stat err = %v", err)
	}
}
