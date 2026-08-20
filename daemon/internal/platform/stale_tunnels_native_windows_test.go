//go:build windows

package platform

import "testing"

func TestMatchTunnelTarget(t *testing.T) {
	targetSet := map[string]struct{}{"vps-1": {}}

	if target, ok := matchTunnelTarget("vps-1", targetSet); !ok || target != "vps-1" {
		t.Fatalf("expected exact match, got target=%q ok=%v", target, ok)
	}
	if target, ok := matchTunnelTarget("VPS-1 2", targetSet); !ok || target != "vps-1" {
		t.Fatalf("expected numbered match, got target=%q ok=%v", target, ok)
	}
	if _, ok := matchTunnelTarget("home wifi", targetSet); ok {
		t.Fatal("expected unrelated adapter alias not to match")
	}
}

func TestHasDuplicateAdapter(t *testing.T) {
	if hasDuplicateAdapter("vps-1", []string{"vps-1"}) {
		t.Fatal("a single, non-numbered adapter must not be treated as a duplicate")
	}
	if !hasDuplicateAdapter("vps-1", []string{"vps-1 2"}) {
		t.Fatal("a single numbered adapter must be treated as a duplicate")
	}
	if !hasDuplicateAdapter("vps-1", []string{"vps-1", "vps-1"}) {
		t.Fatal("more than one matching adapter must be treated as a duplicate")
	}
}
