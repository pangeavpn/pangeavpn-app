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
	mustBeExcluded := []string{"10.0.0.1", "192.168.1.1", "172.16.0.5", "100.64.0.1", "169.254.0.1", "224.0.0.1", "255.255.255.255"}

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

func TestTransformWGConfigExcludeLAN_DualStackCarvesIPv6(t *testing.T) {
	input := "[Peer]\nAllowedIPs = 0.0.0.0/0, ::/0\n"
	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error for dual-stack AllowedIPs: %v", err)
	}
	if strings.Contains(out, "::/0") {
		t.Errorf("::/0 should have been carved, not passed through untouched; got:\n%s", out)
	}
	line := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "AllowedIPs ="))
	for _, ip := range []string{"fe80::1", "fc00::1", "ff02::fb"} {
		addr := netip.MustParseAddr(ip)
		for _, part := range strings.Split(line, ",") {
			p, perr := netip.ParsePrefix(strings.TrimSpace(part))
			if perr == nil && p.Contains(addr) {
				t.Errorf("LAN IPv6 %s should not be covered; got:\n%s", ip, out)
			}
		}
	}
	found := false
	for _, part := range strings.Split(line, ",") {
		p, perr := netip.ParsePrefix(strings.TrimSpace(part))
		if perr == nil && p.Contains(netip.MustParseAddr("2001:db8::1")) {
			found = true
		}
	}
	if !found {
		t.Errorf("public IPv6 should remain covered; got:\n%s", out)
	}
}

func TestTransformWGConfigExcludeLAN_LeavesEntirelyPrivatePeerUnchanged(t *testing.T) {
	input := "[Peer]\nAllowedIPs = 192.168.1.0/24\n"
	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "AllowedIPs = 192.168.1.0/24") {
		t.Errorf("entirely-private peer should pass through unchanged; got:\n%s", out)
	}
}

func TestTransformWGConfigExcludeLAN_LeavesEntirelyPrivateIPv6PeerUnchanged(t *testing.T) {
	input := "[Peer]\nAllowedIPs = fc00::/7\n"
	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "AllowedIPs = fc00::/7") {
		t.Errorf("entirely-private IPv6 peer should pass through unchanged; got:\n%s", out)
	}
}

func TestTransformWGConfigExcludeLAN_PreservesTunnelDNSInsidePrivateRange(t *testing.T) {
	input := "[Interface]\nAddress = 10.0.0.2/32\nDNS = 10.0.0.53, 10.0.0.54\n\n" +
		"[Peer]\nAllowedIPs = 0.0.0.0/0\n"
	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, ip := range []string{"10.0.0.2", "10.0.0.53", "10.0.0.54"} {
		addr := netip.MustParseAddr(ip)
		covered := false
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "AllowedIPs") {
				continue
			}
			value := strings.SplitN(line, "=", 2)[1]
			for _, part := range strings.Split(value, ",") {
				p, perr := netip.ParsePrefix(strings.TrimSpace(part))
				if perr == nil && p.Contains(addr) {
					covered = true
				}
			}
		}
		if !covered {
			t.Errorf("tunnel-internal address %s should remain routed into the tunnel; got:\n%s", ip, out)
		}
	}
	// Other 10/8 addresses (not the interface/DNS) must still be excluded.
	other := netip.MustParseAddr("10.5.5.5")
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "AllowedIPs") {
			continue
		}
		value := strings.SplitN(line, "=", 2)[1]
		for _, part := range strings.Split(value, ",") {
			p, perr := netip.ParsePrefix(strings.TrimSpace(part))
			if perr == nil && p.Contains(other) {
				t.Errorf("unrelated 10/8 LAN address should stay excluded; got:\n%s", out)
			}
		}
	}
}

func TestTransformWGConfigExcludeLAN_ExcludesCGNAT(t *testing.T) {
	input := "[Peer]\nAllowedIPs = 0.0.0.0/0\n"
	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	addr := netip.MustParseAddr("100.64.1.1")
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "AllowedIPs") {
			continue
		}
		value := strings.SplitN(line, "=", 2)[1]
		for _, part := range strings.Split(value, ",") {
			p, perr := netip.ParsePrefix(strings.TrimSpace(part))
			if perr == nil && p.Contains(addr) {
				t.Errorf("CGNAT address should be excluded; got:\n%s", out)
			}
		}
	}
}

func TestTransformWGConfigExcludeLAN_SectionHeaderWithTrailingComment(t *testing.T) {
	input := "[Peer] # eu-1\nAllowedIPs = 0.0.0.0/0\n"
	out, err := TransformWGConfigExcludeLAN(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(out, "AllowedIPs = 0.0.0.0/0\n") {
		t.Errorf("peer section with a trailing comment on its header should still be recognized as [Peer]; got:\n%s", out)
	}
}

func TestSubtractPrefix_SafeOnHostPrefix(t *testing.T) {
	p := netip.MustParsePrefix("2001:db8::1/128")
	ex := netip.MustParsePrefix("2001:db8::1/128")
	if got := subtractPrefix(p, ex); len(got) != 0 {
		t.Errorf("expected empty when host prefix equals exclude, got %v", got)
	}
}
