package platform

import (
	"context"
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

func TestNoopKillSwitch(t *testing.T) {
	ks := &noopKillSwitch{}

	if err := ks.Enable(context.Background(), "1.2.3.4", false); err != nil {
		t.Errorf("enable should not fail: %v", err)
	}
	if err := ks.Update(context.Background(), "wg0"); err != nil {
		t.Errorf("update should not fail: %v", err)
	}
	if err := ks.Clear(context.Background()); err != nil {
		t.Errorf("clear should not fail: %v", err)
	}
	if ks.Active() {
		t.Error("noop should never be active")
	}
}

func TestKillSwitchStatePersistence(t *testing.T) {
	// This tests the shared state persistence helpers.
	// Save, load, remove cycle.
	st := KillSwitchState{
		Active:      true,
		EndpointIPs: []string{"1.2.3.4", "5.6.7.8"},
		PreviousPolicy: map[string]string{
			"domainprofile": "AllowOutbound",
		},
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
	if loaded.EndpointIPs[0] != "1.2.3.4" || loaded.EndpointIPs[1] != "5.6.7.8" {
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
