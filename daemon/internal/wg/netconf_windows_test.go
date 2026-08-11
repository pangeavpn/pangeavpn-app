//go:build windows

package wg

import (
	"net/netip"
	"testing"
)

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

func TestWindowsDNSMatches(t *testing.T) {
	addrs := func(values ...string) []netip.Addr {
		out := make([]netip.Addr, 0, len(values))
		for _, v := range values {
			out = append(out, netip.MustParseAddr(v))
		}
		return out
	}

	tests := []struct {
		name    string
		current []netip.Addr
		want    []netip.Addr
		match   bool
	}{
		{
			name:    "exact match needs no correction",
			current: addrs("1.1.1.1", "1.0.0.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   true,
		},
		{
			// The whole point: another writer swapped our resolvers for theirs.
			name:    "taken over by someone else",
			current: addrs("192.168.1.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   false,
		},
		{
			name:    "cleared entirely",
			current: nil,
			want:    addrs("1.1.1.1"),
			match:   false,
		},
		{
			// Order is preference, so a reordered list really is a change.
			name:    "reordered",
			current: addrs("1.0.0.1", "1.1.1.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   false,
		},
		{
			name:    "ours plus an appended resolver",
			current: addrs("1.1.1.1", "1.0.0.1", "192.168.1.1"),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   false,
		},
		{
			// The tunnel is IPv4-only and bring-up clears the v6 list; a v6
			// entry reported alongside ours must not force a pointless rewrite.
			name:    "IPv6 entries are ignored",
			current: append(addrs("1.1.1.1", "1.0.0.1"), netip.MustParseAddr("fe80::1")),
			want:    addrs("1.1.1.1", "1.0.0.1"),
			match:   true,
		},
		{
			// GetAdaptersAddresses can report a v4 address in v4-mapped form.
			name:    "v4-mapped addresses compare as IPv4",
			current: []netip.Addr{netip.MustParseAddr("::ffff:1.1.1.1")},
			want:    addrs("1.1.1.1"),
			match:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := windowsDNSMatches(tc.current, tc.want); got != tc.match {
				t.Errorf("windowsDNSMatches(%v, %v) = %t, want %t", tc.current, tc.want, got, tc.match)
			}
		})
	}
}
