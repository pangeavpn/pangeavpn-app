package platform

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEndpointIPs_IPLiteral(t *testing.T) {
	ips, err := resolveEndpointIPs(context.Background(), "192.168.1.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "192.168.1.1" {
		t.Errorf("expected [192.168.1.1], got %v", ips)
	}
}

func TestResolveEndpointIPs_IPv6Literal(t *testing.T) {
	ips, err := resolveEndpointIPs(context.Background(), "::1")
	if err != nil {
		t.Fatalf("expected IPv6 literal to be accepted, got: %v", err)
	}
	if len(ips) != 1 || ips[0] != "::1" {
		t.Errorf("expected [::1], got %v", ips)
	}
}

func TestResolveEndpointIPs_EmptyHost(t *testing.T) {
	_, err := resolveEndpointIPs(context.Background(), "")
	if err == nil {
		t.Error("expected error for empty host")
	}
}

func TestResolveEndpointIPs_HostnameIPv4(t *testing.T) {
	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, network, host string) ([]net.IP, error) {
		if network != "ip" {
			t.Fatalf("expected ip lookup network, got %q", network)
		}
		if host != "example.test" {
			t.Fatalf("expected host example.test, got %q", host)
		}
		return []net.IP{
			net.ParseIP("203.0.113.20"),
			net.ParseIP("203.0.113.20"),
			net.ParseIP("203.0.113.21"),
		}, nil
	}
	defer func() {
		lookupResolverIP = originalLookup
	}()

	ips, err := resolveEndpointIPs(context.Background(), "example.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 2 {
		t.Fatalf("expected 2 unique IPv4 IPs, got %d: %v", len(ips), ips)
	}
	for _, ip := range ips {
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.To4() == nil {
			t.Errorf("expected valid IPv4 address, got %s", ip)
		}
	}
}

func TestResolveEndpointIPs_HostnameOnlyIPv6Succeeds(t *testing.T) {
	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, network, host string) ([]net.IP, error) {
		if network != "ip" {
			t.Fatalf("expected ip lookup network, got %q", network)
		}
		if host != "ipv6-only.test" {
			t.Fatalf("expected host ipv6-only.test, got %q", host)
		}
		return []net.IP{net.ParseIP("2001:db8::1")}, nil
	}
	defer func() {
		lookupResolverIP = originalLookup
	}()

	ips, err := resolveEndpointIPs(context.Background(), "ipv6-only.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "2001:db8::1" {
		t.Fatalf("expected [2001:db8::1], got %v", ips)
	}
}

func TestResolveEndpointIPs_NoAddressesResolved(t *testing.T) {
	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, _, _ string) ([]net.IP, error) {
		return nil, nil
	}
	defer func() {
		lookupResolverIP = originalLookup
	}()

	_, err := resolveEndpointIPs(context.Background(), "empty.test")
	if err == nil {
		t.Fatal("expected error when no addresses resolved")
	}
	if !strings.Contains(err.Error(), "no addresses") {
		t.Fatalf("expected no addresses error, got: %v", err)
	}
}

func TestResolveEndpointIPs_Deduplication(t *testing.T) {
	// IP literals are already unique, so test with the same literal.
	ips, err := resolveEndpointIPs(context.Background(), "10.0.0.1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ips) != 1 {
		t.Errorf("expected 1 IP, got %d", len(ips))
	}
}

// A transport whose remote host can't be resolved must not take the whole
// Enable down with it: under an active lockdown lock the kill switch blocks
// DNS itself, so every hostname permit (naive/reality/hysteria2/snowflake)
// fails to resolve and Connect could never re-arm the switch.
func TestResolveEndpointHosts_SkipsUnresolvableHostname(t *testing.T) {
	isolateStateDir(t)
	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		if host == "reachable.test" {
			return []net.IP{net.ParseIP("203.0.113.30")}, nil
		}
		return nil, errors.New("dns blocked by kill switch")
	}
	originalWarn := EndpointResolveWarn
	var skipped []string
	EndpointResolveWarn = func(host string, _ error) {
		skipped = append(skipped, host)
	}
	defer func() {
		lookupResolverIP = originalLookup
		EndpointResolveWarn = originalWarn
	}()

	ips, err := resolveEndpointHosts(
		context.Background(),
		[]string{"198.51.100.4", "blocked.test", "reachable.test"},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"198.51.100.4", "203.0.113.30"}
	if !stringSlicesEqual(ips, want) {
		t.Fatalf("resolveEndpointHosts() = %v, want %v", ips, want)
	}
	if len(skipped) != 1 || skipped[0] != "blocked.test" {
		t.Fatalf("expected blocked.test reported as skipped, got %v", skipped)
	}
}

func TestResolveEndpointHosts_ErrorsWhenNothingResolvesAndNoPriorState(t *testing.T) {
	isolateStateDir(t)
	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, _, _ string) ([]net.IP, error) {
		return nil, errors.New("dns blocked by kill switch")
	}
	defer func() { lookupResolverIP = originalLookup }()

	if _, err := resolveEndpointHosts(context.Background(), []string{"blocked.test"}); err == nil {
		t.Fatal("expected an error when no endpoint host resolves and there is no prior state")
	}
}

// A wholesale DNS failure must not strand the lock un-armable when a prior
// endpoint set exists on disk (e.g. Connect re-arming an active lockdown).
func TestResolveEndpointHosts_FallsBackToPersistedOnWholesaleFailure(t *testing.T) {
	isolateStateDir(t)
	if err := saveKillSwitchState(KillSwitchState{Active: true, EndpointIPs: []string{"203.0.113.9"}}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, _, _ string) ([]net.IP, error) {
		return nil, errors.New("dns blocked by kill switch")
	}
	defer func() { lookupResolverIP = originalLookup }()

	ips, err := resolveEndpointHosts(context.Background(), []string{"blocked.test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !stringSlicesEqual(ips, []string{"203.0.113.9"}) {
		t.Fatalf("expected fallback to persisted IPs, got %v", ips)
	}
}

// A partial DNS failure must union the resolved set with the persisted one
// rather than replacing it, so a transient failure can't narrow a live lock.
func TestResolveEndpointHosts_UnionsWithPersistedOnPartialFailure(t *testing.T) {
	isolateStateDir(t)
	if err := saveKillSwitchState(KillSwitchState{Active: true, EndpointIPs: []string{"203.0.113.9"}}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, _, host string) ([]net.IP, error) {
		if host == "reachable.test" {
			return []net.IP{net.ParseIP("203.0.113.30")}, nil
		}
		return nil, errors.New("dns blocked by kill switch")
	}
	defer func() { lookupResolverIP = originalLookup }()

	ips, err := resolveEndpointHosts(context.Background(), []string{"blocked.test", "reachable.test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"203.0.113.30", "203.0.113.9"}
	if !stringSlicesEqual(ips, want) {
		t.Fatalf("resolveEndpointHosts() = %v, want %v", ips, want)
	}
}

func TestNoopKillSwitch(t *testing.T) {
	ks := &noopKillSwitch{}

	if err := ks.Enable(context.Background(), []string{"203.0.113.4"}, false, false); err != nil {
		t.Errorf("enable should not fail: %v", err)
	}
	if err := ks.Update(context.Background(), TunnelRef{Name: "wg0"}); err != nil {
		t.Errorf("update should not fail: %v", err)
	}
	if err := ks.Clear(context.Background()); err != nil {
		t.Errorf("clear should not fail: %v", err)
	}
	if ks.Active() {
		t.Error("noop should never be active")
	}
}

// isolateStateDir points the kill-switch state file at a temp directory for the
// duration of a test. Without it these tests write to — and then delete — the
// state file of a real installation on the developer's machine, which for an
// engaged Lockdown lock destroys the Locked record and makes the next daemon
// start clear the lock as stale.
func isolateStateDir(t *testing.T) {
	t.Helper()
	path := filepath.Join(t.TempDir(), killSwitchStateFile)
	previous := killSwitchStatePathFn
	killSwitchStatePathFn = func() (string, error) { return path, nil }
	t.Cleanup(func() { killSwitchStatePathFn = previous })
}

func TestKillSwitchStatePersistence(t *testing.T) {
	isolateStateDir(t)
	// This tests the shared state persistence helpers.
	// Save, load, remove cycle.
	st := KillSwitchState{
		Active:      true,
		EndpointIPs: []string{"203.0.113.4", "203.0.113.8"},
	}

	if err := saveKillSwitchState(st); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	loaded, err := loadKillSwitchState()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}

	if !loaded.Active {
		t.Error("expected active=true")
	}
	if len(loaded.EndpointIPs) != 2 {
		t.Errorf("expected 2 endpoint IPs, got %d", len(loaded.EndpointIPs))
	}
	if loaded.EndpointIPs[0] != "203.0.113.4" || loaded.EndpointIPs[1] != "203.0.113.8" {
		t.Errorf("unexpected endpoint IPs: %v", loaded.EndpointIPs)
	}

	if err := removeKillSwitchState(); err != nil {
		t.Fatalf("remove failed: %v", err)
	}

	afterRemove, err := loadKillSwitchState()
	if err != nil {
		t.Fatalf("load after remove failed: %v", err)
	}
	if afterRemove.Active {
		t.Error("expected active=false after remove")
	}
}

// TestPersistLockedUpgrade_RecordsLockdownOnUnchangedRules covers the one way
// this design can fail open: Enable short-circuits when the permit set is
// unchanged, so a lock re-armed as a Lockdown lock would never get Locked onto
// disk, and startup reconciliation would clear a deliberate lockdown as crash
// leftover — restoring unprotected internet the user asked to keep shut.
func TestPersistLockedUpgrade_RecordsLockdownOnUnchangedRules(t *testing.T) {
	isolateStateDir(t)

	engaged := KillSwitchState{Active: true, EndpointIPs: []string{"203.0.113.4"}, Locked: false}
	if err := saveKillSwitchState(engaged); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if err := persistLockedUpgrade(engaged, true); err != nil {
		t.Fatalf("persistLockedUpgrade: %v", err)
	}

	loaded, err := loadKillSwitchState()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if !loaded.Locked {
		t.Fatal("Locked was not persisted; startup reconciliation would clear this lock as stale")
	}
	if !loaded.Active || len(loaded.EndpointIPs) != 1 || loaded.EndpointIPs[0] != "203.0.113.4" {
		t.Fatalf("unrelated state was disturbed: %+v", loaded)
	}
}

// An explicit Lockdown-off request (locked=false passed by the caller, not
// echoed from disk) must actually clear a stale Locked=true on disk, or
// reconciliation re-applies a block-all lock the user turned off.
func TestPersistLockedUpgrade_LowersOnExplicitUnlock(t *testing.T) {
	isolateStateDir(t)

	locked := KillSwitchState{Active: true, EndpointIPs: []string{"203.0.113.4"}, Locked: true}
	if err := saveKillSwitchState(locked); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	if err := persistLockedUpgrade(locked, false); err != nil {
		t.Fatalf("persistLockedUpgrade: %v", err)
	}

	loaded, err := loadKillSwitchState()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.Locked {
		t.Fatal("Locked was not lowered by an explicit unlock request")
	}
}

// A corrupt state file must be distinguishable from a missing one, so
// reconciliation can probe the platform instead of assuming nothing engaged.
func TestLoadKillSwitchState_CorruptFileIsDistinguishedFromMissing(t *testing.T) {
	isolateStateDir(t)

	path, err := killSwitchStatePath()
	if err != nil {
		t.Fatalf("killSwitchStatePath: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	_, err = loadKillSwitchState()
	if err == nil {
		t.Fatal("expected an error for a corrupt state file")
	}
	if !errors.Is(err, ErrKillSwitchStateUnreadable) {
		t.Fatalf("expected ErrKillSwitchStateUnreadable, got: %v", err)
	}
}

func TestLoadKillSwitchState_MissingFileIsNotAnError(t *testing.T) {
	isolateStateDir(t)

	st, err := loadKillSwitchState()
	if err != nil {
		t.Fatalf("expected no error for a missing state file, got: %v", err)
	}
	if st.Active {
		t.Fatal("expected zero-value state for a missing file")
	}
}

func TestUpdateTunnelInterfaceState(t *testing.T) {
	isolateStateDir(t)

	if err := saveKillSwitchState(KillSwitchState{Active: true, EndpointIPs: []string{"203.0.113.4"}}); err != nil {
		t.Fatalf("save failed: %v", err)
	}

	updated, err := updateTunnelInterfaceState("utun7")
	if err != nil {
		t.Fatalf("updateTunnelInterfaceState: %v", err)
	}
	if updated.TunnelInterface != "utun7" {
		t.Fatalf("expected utun7, got %q", updated.TunnelInterface)
	}

	loaded, err := loadKillSwitchState()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if loaded.TunnelInterface != "utun7" || !loaded.Active {
		t.Fatalf("unexpected persisted state: %+v", loaded)
	}
}

func TestUpdateTunnelInterfaceState_PropagatesUnreadableError(t *testing.T) {
	isolateStateDir(t)

	path, err := killSwitchStatePath()
	if err != nil {
		t.Fatalf("killSwitchStatePath: %v", err)
	}
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write corrupt state: %v", err)
	}

	if _, err := updateTunnelInterfaceState("utun7"); err == nil {
		t.Fatal("expected an error when the prior state is unreadable")
	}
}

// The Windows, nftables and iptables kill switches all feed LANAllowPrefixes
// straight into IPv4-only rules, so a v6 entry there fails the whole arm.
func TestLANAllowPrefixesAreFamilySeparated(t *testing.T) {
	for _, cidr := range LANAllowPrefixes {
		if strings.Contains(cidr, ":") {
			t.Errorf("LANAllowPrefixes must be IPv4 only, got %s", cidr)
		}
	}
	if len(LANAllowPrefixesV6) == 0 {
		t.Fatal("LANAllowPrefixesV6 must not be empty")
	}
	for _, cidr := range LANAllowPrefixesV6 {
		if !strings.Contains(cidr, ":") {
			t.Errorf("LANAllowPrefixesV6 must be IPv6 only, got %s", cidr)
		}
	}
}
