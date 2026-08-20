//go:build darwin

package platform

import (
	"strings"
	"testing"
)

func TestBuildPFRules_IPv4AndIPv6AllowRules(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20", "2001:db8::20"}, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}

	if !strings.Contains(rules, "pass out quick inet proto { tcp udp } to 198.51.100.20") {
		t.Fatalf("missing IPv4 endpoint allow rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick inet6 proto { tcp udp } to 2001:db8::20") {
		t.Fatalf("missing IPv6 endpoint allow rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick inet proto udp from any port 68 to any port 67") {
		t.Fatalf("missing DHCP allow rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick on utun9 inet all") {
		t.Fatalf("missing tunnel allow rule:\n%s", rules)
	}
}

func TestBuildPFRules_RejectsInvalidTunnelName(t *testing.T) {
	if _, err := buildPFRules(nil, "utun9; block", false); err == nil {
		t.Fatal("expected error for invalid tunnel interface name")
	}
}
