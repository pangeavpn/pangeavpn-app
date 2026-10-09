//go:build darwin || linux

package wg

import (
	"net/netip"
	"slices"
	"testing"
)

func carvedAllowedIPs(t *testing.T, exclude string) []string {
	t.Helper()
	text, err := TransformWGConfigExclude(liveSplitConfig, []netip.Prefix{netip.MustParsePrefix(exclude)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := parseUserlandConfig(text)
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := validateParsedIPv4Only(parsed)
	if err != nil {
		t.Fatal(err)
	}
	return allowed
}

// A /sbin/route add failing mid-carve must not leave the ranges after it off the tunnel once the retry reports success.
func TestSyncTrackedAllowedIPs_RetryConvergesOnARealCarve(t *testing.T) {
	carved := carvedAllowedIPs(t, "203.0.113.0/24")
	if !slices.Contains(carved, "200.0.0.0/7") {
		t.Fatalf("carve %v lacks the route this test fails", carved)
	}
	k := newFakeRouteTable(expandRoutePrefixes, "0.0.0.0/0")
	tracked := []string{"0.0.0.0/0"}
	checkConverges(t, k, &tracked, carved, "200.0.0.0/7")
	checkConverges(t, k, &tracked, []string{"0.0.0.0/0"}, "128.0.0.0/1")
	checkConverges(t, k, &tracked, carved, "224.0.0.0/3")
}

func TestExpandRoutePrefixes(t *testing.T) {
	got := expandRoutePrefixes([]string{"0.0.0.0/0", "10.1.2.3/8", "::/0", "bogus"})
	want := []string{"0.0.0.0/1", "128.0.0.0/1", "10.0.0.0/8", "::/1", "8000::/1", "bogus"}
	if !slices.Equal(got, want) {
		t.Fatalf("expandRoutePrefixes = %v, want %v", got, want)
	}
}
