//go:build darwin || linux || windows

package wg

import (
	"net/netip"
	"strings"
	"testing"
)

func TestSubtractPrefix_NoOverlap(t *testing.T) {
	p := netip.MustParsePrefix("8.0.0.0/8")
	ex := netip.MustParsePrefix("10.0.0.0/8")
	result := subtractPrefix(p, ex)
	if len(result) != 1 || result[0] != p {
		t.Errorf("expected [%s], got %v", p, result)
	}
}

func TestSubtractPrefix_FullyContained(t *testing.T) {
	p := netip.MustParsePrefix("10.0.0.0/8")
	ex := netip.MustParsePrefix("0.0.0.0/0")
	if got := subtractPrefix(p, ex); len(got) != 0 {
		t.Errorf("expected empty, got %v", got)
	}
}

func TestSubtractPrefix_SplitsDefaultRoute(t *testing.T) {
	p := netip.MustParsePrefix("0.0.0.0/0")
	ex := netip.MustParsePrefix("10.0.0.0/8")
	result := subtractPrefix(p, ex)
	// Verify no remainder overlaps the exclusion and the union covers (0.0.0.0/0 \ 10.0.0.0/8).
	for _, r := range result {
		if r.Overlaps(ex) {
			t.Errorf("result %s overlaps exclusion %s", r, ex)
		}
	}
	// Verify 10.x.x.x is not covered.
	for _, r := range result {
		if r.Contains(netip.MustParseAddr("10.1.2.3")) {
			t.Errorf("prefix %s should not contain 10.1.2.3", r)
		}
	}
	// Verify a non-excluded address is covered.
	found := false
	for _, r := range result {
		if r.Contains(netip.MustParseAddr("8.8.8.8")) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("8.8.8.8 should be covered by result %v", result)
	}
}

func TestSubtractRanges_AllStandardLAN(t *testing.T) {
	inputs := []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
	result := subtractRanges(inputs, lanExcludeRanges)

	mustBeCovered := []string{"8.8.8.8", "1.1.1.1", "203.0.113.5"}
	mustBeExcluded := []string{"10.0.0.1", "192.168.1.1", "172.16.0.5", "169.254.0.1", "224.0.0.1", "255.255.255.255"}

	for _, ip := range mustBeCovered {
		addr := netip.MustParseAddr(ip)
		covered := false
		for _, r := range result {
			if r.Contains(addr) {
				covered = true
				break
			}
		}
		if !covered {
			t.Errorf("public IP %s should be covered", ip)
		}
	}
	for _, ip := range mustBeExcluded {
		addr := netip.MustParseAddr(ip)
		for _, r := range result {
			if r.Contains(addr) {
				t.Errorf("LAN IP %s should not be covered (matched %s)", ip, r)
			}
		}
	}
}

func TestTransformWGConfigExcludeLAN_ZeroRoute(t *testing.T) {
	input := "[Interface]\nPrivateKey = abc=\nAddress = 10.0.0.2/32\n\n[Peer]\nPublicKey = xyz=\nAllowedIPs = 0.0.0.0/0\nEndpoint = 1.2.3.4:443\n"

	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "[Interface]") || !strings.Contains(out, "Address = 10.0.0.2/32") {
		t.Errorf("non-peer lines should pass through unchanged; got:\n%s", out)
	}
	if strings.Contains(out, "AllowedIPs = 0.0.0.0/0") {
		t.Errorf("0.0.0.0/0 should have been rewritten; got:\n%s", out)
	}
	if !strings.Contains(out, "AllowedIPs = ") {
		t.Errorf("AllowedIPs line missing; got:\n%s", out)
	}
	// The first prefix after 0.0.0.0/0 minus 10.0.0.0/8 should be 0.0.0.0/5.
	if !strings.Contains(out, "0.0.0.0/5") {
		t.Errorf("expected 0.0.0.0/5 in output; got:\n%s", out)
	}
}

func TestTransformWGConfigExcludeLAN_PreservesNonPeerAllowedIPs(t *testing.T) {
	// AllowedIPs in non-Peer sections (hypothetical) should not be touched.
	input := "[Interface]\nAllowedIPs = 10.0.0.0/8\n"
	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "AllowedIPs = 10.0.0.0/8") {
		t.Errorf("non-peer AllowedIPs should pass through; got:\n%s", out)
	}
}

func TestTransformWGConfigExcludeLAN_RejectsIPv6(t *testing.T) {
	input := "[Peer]\nAllowedIPs = ::/0\n"
	if _, err := TransformWGConfigExcludeLAN(input); err == nil {
		t.Fatal("expected error for IPv6 AllowedIPs")
	}
}

func TestTransformWGConfigExcludeLAN_RejectsEmptyResult(t *testing.T) {
	input := "[Peer]\nAllowedIPs = 192.168.1.0/24\n"
	if _, err := TransformWGConfigExcludeLAN(input); err == nil {
		t.Fatal("expected error when AllowedIPs becomes empty after subtraction")
	}
}
