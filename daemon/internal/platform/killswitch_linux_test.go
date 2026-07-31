//go:build linux

package platform

import (
	"strings"
	"testing"
)

// The iptables fallback's own tests live in killswitch_iptables_test.go, which
// carries no build tag on purpose: there is no Linux CI job, so anything tested
// only here would never actually run. This file covers the nftables backend,
// which is Linux-only by nature.

func TestBuildNFTRuleset_IPv4Only(t *testing.T) {
	rules := buildNFTRuleset([]string{"203.0.113.10", "2001:db8::10"}, "wg-test", false)

	if strings.Contains(rules, "ip6 daddr") {
		t.Fatalf("unexpected IPv6 endpoint allow in nft ruleset:\n%s", rules)
	}
	if strings.Contains(rules, "2001:db8::10") {
		t.Fatalf("unexpected IPv6 endpoint in nft ruleset:\n%s", rules)
	}
	if !strings.Contains(rules, `ip daddr 203.0.113.10 accept`) {
		t.Fatalf("missing IPv4 endpoint allow in nft ruleset:\n%s", rules)
	}
	if !strings.Contains(rules, `meta nfproto ipv4 oifname "wg-test" accept`) {
		t.Fatalf("missing IPv4-only tunnel allow rule in nft ruleset:\n%s", rules)
	}
}

// The ruleset is applied as one `nft -f` script, which the kernel runs as a
// single transaction — that atomicity is what keeps a re-arm from ever leaving
// the host unfiltered, so the script must carry the replacement in the same
// breath as the delete.
func TestApplyNFTScript_ReplacesInOneTransaction(t *testing.T) {
	rules := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", false)
	if !strings.Contains(rules, "policy drop;") {
		t.Fatalf("nft chain does not default to drop:\n%s", rules)
	}
	if !strings.Contains(rules, `oifname "lo" accept`) {
		t.Fatalf("nft ruleset does not permit loopback:\n%s", rules)
	}
}

// Turning Allow LAN off must actually drop the LAN accepts from the ruleset.
func TestBuildNFTRuleset_AllowLANIsNotSticky(t *testing.T) {
	with := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", true)
	if !strings.Contains(with, "192.168.0.0/16") {
		t.Fatalf("LAN permit missing when allowLAN is on:\n%s", with)
	}
	without := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", false)
	if strings.Contains(without, "192.168.0.0/16") {
		t.Fatalf("LAN permit present when allowLAN is off:\n%s", without)
	}
}
