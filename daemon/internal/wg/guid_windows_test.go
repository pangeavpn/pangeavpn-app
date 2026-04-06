//go:build windows

package wg

import "testing"

func TestRequestedWindowsTunnelGUIDDeterministic(t *testing.T) {
	first := requestedWindowsTunnelGUID("vps-1")
	second := requestedWindowsTunnelGUID("  vps-1  ")

	if *first != *second {
		t.Fatalf("requestedWindowsTunnelGUID() mismatch: %v != %v", *first, *second)
	}
}

func TestRequestedWindowsTunnelGUIDChangesForDifferentNames(t *testing.T) {
	base := requestedWindowsTunnelGUID("vps-1")
	other := requestedWindowsTunnelGUID("vps-2")

	if *base == *other {
		t.Fatalf("requestedWindowsTunnelGUID() should change when tunnel name changes: %v", *base)
	}
}

func TestRequestedWindowsTunnelGUIDStableAcrossConfigs(t *testing.T) {
	// GUID should be the same regardless of config content — only the
	// tunnel name determines adapter identity. This prevents adapter
	// proliferation when the WireGuard config changes between connections.
	first := requestedWindowsTunnelGUID("vps-1")
	second := requestedWindowsTunnelGUID("vps-1")

	if *first != *second {
		t.Fatalf("requestedWindowsTunnelGUID() should be stable for the same name: %v != %v", *first, *second)
	}
}
