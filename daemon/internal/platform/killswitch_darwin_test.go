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
	if !strings.Contains(rules, "pass out quick on utun9 all") {
		t.Fatalf("missing tunnel allow rule:\n%s", rules)
	}
}

func TestBuildPFRules_RejectsInvalidTunnelName(t *testing.T) {
	if _, err := buildPFRules(nil, "utun9; block", false); err == nil {
		t.Fatal("expected error for invalid tunnel interface name")
	}
}

func TestBuildPFRules_LANPrefixesUseMatchingAddressFamily(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", true)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}

	if !strings.Contains(rules, "pass out quick inet to 192.168.0.0/16") {
		t.Fatalf("missing IPv4 LAN allow rule:\n%s", rules)
	}
	for _, cidr := range []string{"fe80::/10", "ff02::/16", "fc00::/7"} {
		if !strings.Contains(rules, "pass out quick inet6 to "+cidr) {
			t.Fatalf("IPv6 LAN prefix %s not emitted as inet6:\n%s", cidr, rules)
		}
		if strings.Contains(rules, "pass out quick inet to "+cidr) {
			t.Fatalf("IPv6 LAN prefix %s emitted as inet, pfctl will reject it:\n%s", cidr, rules)
		}
	}
}

func TestBuildPFRules_TunnelRuleCoversBothFamilies(t *testing.T) {
	rules, err := buildPFRules(nil, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if !strings.Contains(rules, "pass out quick on utun9 all") {
		t.Fatalf("tunnel rule should not be pinned to one address family:\n%s", rules)
	}
}
