//go:build darwin

package platform

import (
	"strings"
	"testing"
)

func TestBuildPFRules_IPv4OnlyAllowRules(t *testing.T) {
	rules := buildPFRules([]string{"198.51.100.20", "2001:db8::20"}, "utun9")

	if !strings.Contains(rules, "pass out quick inet proto {tcp, udp} to 198.51.100.20") {
		t.Fatalf("missing IPv4 endpoint allow rule:\n%s", rules)
	}
	if strings.Contains(rules, "2001:db8::20") {
		t.Fatalf("unexpected IPv6 endpoint rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick inet proto udp from any port 68 to any port 67") {
		t.Fatalf("missing IPv4 DHCP allow rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick inet on utun9 all") {
		t.Fatalf("missing IPv4-only tunnel allow rule:\n%s", rules)
	}
}
