//go:build windows

package wg

import "testing"

func TestParseWindowsPrefixes_PreservesInterfaceHostBits(t *testing.T) {
	v4, v6, err := parseWindowsPrefixes([]string{"10.0.0.2/24", "10.0.0.2/24"})
	if err != nil {
		t.Fatalf("parseWindowsPrefixes returned error: %v", err)
	}

	if len(v6) != 0 {
		t.Fatalf("expected no IPv6 prefixes, got %v", v6)
	}
	if len(v4) != 1 {
		t.Fatalf("expected one IPv4 prefix, got %d: %v", len(v4), v4)
	}
	if got, want := v4[0].String(), "10.0.0.2/24"; got != want {
		t.Fatalf("expected interface prefix %q, got %q", want, got)
	}
}

func TestParseWindowsRoutePrefixes_MasksNetworks(t *testing.T) {
	v4, v6, err := parseWindowsRoutePrefixes([]string{"10.0.0.2/24", "10.0.0.0/24"})
	if err != nil {
		t.Fatalf("parseWindowsRoutePrefixes returned error: %v", err)
	}

	if len(v6) != 0 {
		t.Fatalf("expected no IPv6 prefixes, got %v", v6)
	}
	if len(v4) != 1 {
		t.Fatalf("expected one IPv4 prefix, got %d: %v", len(v4), v4)
	}
	if got, want := v4[0].String(), "10.0.0.0/24"; got != want {
		t.Fatalf("expected route prefix %q, got %q", want, got)
	}
}
