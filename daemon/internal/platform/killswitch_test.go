package platform

import (
	"context"
	"errors"
	"net"
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
	_, err := resolveEndpointIPs(context.Background(), "::1")
	if err == nil {
		t.Fatal("expected IPv6 literal to be rejected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "ipv6") {
		t.Fatalf("expected IPv6 error, got: %v", err)
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
		if network != "ip4" {
			t.Fatalf("expected ip4 lookup network, got %q", network)
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

func TestResolveEndpointIPs_HostnameOnlyIPv6Fails(t *testing.T) {
	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, network, host string) ([]net.IP, error) {
		if network != "ip4" {
			t.Fatalf("expected ip4 lookup network, got %q", network)
		}
		if host != "ipv6-only.test" {
			t.Fatalf("expected host ipv6-only.test, got %q", host)
		}
		return nil, nil
	}
	defer func() {
		lookupResolverIP = originalLookup
	}()

	_, err := resolveEndpointIPs(context.Background(), "ipv6-only.test")
	if err == nil {
		t.Fatal("expected no-IPv4 resolution error")
	}
	if !strings.Contains(err.Error(), "no IPv4 addresses") {
		t.Fatalf("expected no IPv4 addresses error, got: %v", err)
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

func TestResolveEndpointHosts_ErrorsWhenNothingResolves(t *testing.T) {
	originalLookup := lookupResolverIP
	lookupResolverIP = func(_ context.Context, _, _ string) ([]net.IP, error) {
		return nil, errors.New("dns blocked by kill switch")
	}
	defer func() { lookupResolverIP = originalLookup }()

	if _, err := resolveEndpointHosts(context.Background(), []string{"blocked.test"}); err == nil {
		t.Fatal("expected an error when no endpoint host resolves")
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
	t.Setenv(appSupportDirOverrideEnv, t.TempDir())
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

// A lock never silently stops being a Lockdown lock: dropping Locked would make
// a deliberate lockdown look like crash leftover to the next startup.
func TestPersistLockedUpgrade_NeverClearsAnExistingLock(t *testing.T) {
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
	if !loaded.Locked {
		t.Fatal("an engaged Lockdown lock was downgraded")
	}
}
