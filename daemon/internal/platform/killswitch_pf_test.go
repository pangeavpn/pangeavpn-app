package platform

import (
	"strings"
	"testing"
)

// Untagged on purpose: there is no macOS CI job, and the ruleset is pure text
// that can be checked anywhere.

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
	if !strings.Contains(rules, "pass out quick inet proto udp from any port 68 to 255.255.255.255 port 67") {
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

// The shipped outage: stateful lo0 filtering made macOS pf drop loopback TCP
// the moment the lock armed, so the app could no longer reach its own daemon.
func TestBuildPFRules_LoopbackIsStatelessBothDirections(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if !strings.Contains(rules, "pass out quick on lo0 all no state") {
		t.Fatalf("outbound lo0 rule must be stateless:\n%s", rules)
	}
	if !strings.Contains(rules, "pass in quick on lo0 all no state") {
		t.Fatalf("inbound lo0 rule must exist and be stateless:\n%s", rules)
	}
}

// Unscoped, any root process bound to port 68 could reach any host on udp/67.
func TestBuildPFRules_DHCPRequestIsScopedToBroadcast(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", true)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if strings.Contains(rules, "to any port 67") {
		t.Fatalf("DHCP request rule reaches any host:\n%s", rules)
	}
}

// Windows blocks unsolicited inbound; the pf lock should too, keeping only what
// the lock itself needs: loopback, the DHCP reply, and (opted in) the LAN.
func TestBuildPFRules_BlocksInboundByDefault(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if !strings.Contains(rules, "block in all") {
		t.Fatalf("no inbound block:\n%s", rules)
	}
	if !strings.Contains(rules, "pass in quick inet proto udp from any port 67 to any port 68") {
		t.Fatalf("the DHCP reply cannot get through an inbound block without its own pass:\n%s", rules)
	}
	if strings.Contains(rules, "pass in quick inet from") || strings.Contains(rules, "pass in quick inet6 from") {
		t.Fatalf("inbound LAN passes emitted without allowLAN:\n%s", rules)
	}
}

func TestBuildPFRules_AllowLANAdmitsTheLANInbound(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", true)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	for _, want := range []string{"pass in quick inet from 192.168.0.0/16", "pass in quick inet6 from fe80::/10"} {
		if !strings.Contains(rules, want) {
			t.Fatalf("missing %q: Allow LAN means the LAN can reach this host too:\n%s", want, rules)
		}
	}
}

// Allow LAN must not reopen the resolver hole: DNS/DoT to a LAN address is
// blocked, while the tunnel pass ahead of it keeps a tunnel-side resolver working.
func TestBuildPFRules_AllowLANStillBlocksResolversOnTheLAN(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", true)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	tunnel := strings.Index(rules, "pass out quick on utun9 all")
	v4Block := strings.Index(rules, "block out quick inet proto { tcp udp } to 192.168.0.0/16 port { 53 853 }")
	v6Block := strings.Index(rules, "block out quick inet6 proto { tcp udp } to fc00::/7 port { 53 853 }")
	lan := strings.Index(rules, "pass out quick inet to 10.0.0.0/8")
	if v4Block < 0 || v6Block < 0 {
		t.Fatalf("resolver blocks missing under allowLAN:\n%s", rules)
	}
	if !(tunnel < v4Block && v4Block < lan) {
		t.Fatalf("order wrong (tunnel=%d block=%d lan=%d); the block must follow the tunnel pass and precede every LAN pass:\n%s", tunnel, v4Block, lan, rules)
	}

	without, err := buildPFRules([]string{"198.51.100.20"}, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if strings.Contains(without, "port { 53 853 }") {
		t.Fatalf("resolver blocks emitted with allowLAN off:\n%s", without)
	}
}
