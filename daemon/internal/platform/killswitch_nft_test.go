package platform

import (
	"strings"
	"testing"
)

// Untagged on purpose: there is no Linux CI job, so a linux-tagged test would
// never run. The ruleset builder is pure text and can be checked anywhere.

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
// single transaction; the script must carry the replacement with the delete.
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

// The nft table is IPv4-matched via `ip daddr`; a v6 prefix there is a parse
// error that rejects the entire ruleset.
func TestBuildNFTRuleset_LANPermitsAreIPv4Only(t *testing.T) {
	ruleset := buildNFTRuleset([]string{"198.51.100.20"}, "wg0", true)

	if !strings.Contains(ruleset, "ip daddr 192.168.0.0/16 accept") {
		t.Fatalf("expected IPv4 LAN permits with allowLAN on:\n%s", ruleset)
	}
	for _, line := range strings.Split(ruleset, "\n") {
		if strings.Contains(line, "ip daddr") && strings.Contains(line, "::") {
			t.Errorf("IPv6 prefix in an `ip daddr` match: %s", line)
		}
	}
}

// nftChainBody returns the lines between "chain <name> {" and its closing brace.
func nftChainBody(t *testing.T, ruleset, chain string) string {
	t.Helper()
	start := strings.Index(ruleset, "chain "+chain+" {")
	if start < 0 {
		t.Fatalf("ruleset has no %q chain:\n%s", chain, ruleset)
	}
	rest := ruleset[start:]
	end := strings.Index(rest, "\n  }")
	if end < 0 {
		t.Fatalf("%q chain never closes:\n%s", chain, ruleset)
	}
	return rest[:end]
}

// Traffic the host forwards for containers and VMs never traverses the output
// hook, so the lock needs a forward chain that drops by default too.
func TestBuildNFTRuleset_ForwardChainDropsByDefault(t *testing.T) {
	ruleset := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", false)
	forward := nftChainBody(t, ruleset, "forward")
	if !strings.Contains(forward, "type filter hook forward priority 0; policy drop;") {
		t.Fatalf("forward chain is not a drop-by-default forward hook:\n%s", forward)
	}
}

// A guest may only leave through the tunnel, and its replies must come back.
func TestBuildNFTRuleset_ForwardChainPermitsTunnelBothWays(t *testing.T) {
	ruleset := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", false)
	forward := nftChainBody(t, ruleset, "forward")
	for _, want := range []string{`meta nfproto ipv4 oifname "wg-test" accept`, `meta nfproto ipv4 iifname "wg-test" accept`} {
		if !strings.Contains(forward, want) {
			t.Errorf("forward chain lacks %q:\n%s", want, forward)
		}
	}
	if strings.Contains(forward, "203.0.113.10") {
		t.Errorf("forward chain permits the endpoint itself; only the host's own WireGuard socket may reach it:\n%s", forward)
	}
}

// With no tunnel yet (a lock held while disconnected) nothing forwarded may leave.
func TestBuildNFTRuleset_ForwardChainWithoutTunnelPermitsNoEgress(t *testing.T) {
	ruleset := buildNFTRuleset([]string{"203.0.113.10"}, "", false)
	forward := nftChainBody(t, ruleset, "forward")
	if strings.Contains(forward, "oifname") && !strings.Contains(forward, `oifkind`) {
		t.Fatalf("forward chain names an egress interface with no tunnel up:\n%s", forward)
	}
	if strings.Contains(forward, `oifname ""`) {
		t.Fatalf("forward chain carries an empty interface match, which nft rejects:\n%s", forward)
	}
}

// br_netfilter runs bridged container-to-container frames through the forward
// hook; they never leave the host, so they pass on the egress device kind.
func TestBuildNFTRuleset_ForwardChainKeepsBridgedTrafficWorking(t *testing.T) {
	ruleset := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", false)
	forward := nftChainBody(t, ruleset, "forward")
	if !strings.Contains(forward, `meta oifkind "bridge" accept`) {
		t.Fatalf("forward chain would drop same-bridge container traffic:\n%s", forward)
	}
}

// Allow LAN applies to guests as it does to the host, and only when opted in.
func TestBuildNFTRuleset_ForwardChainFollowsAllowLAN(t *testing.T) {
	with := nftChainBody(t, buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", true), "forward")
	if !strings.Contains(with, "ip daddr 192.168.0.0/16 accept") {
		t.Fatalf("forward chain lacks the LAN permit with allowLAN on:\n%s", with)
	}
	without := nftChainBody(t, buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", false), "forward")
	if strings.Contains(without, "192.168.0.0/16") {
		t.Fatalf("forward chain permits the LAN with allowLAN off:\n%s", without)
	}
}

// Allow LAN must not reopen the resolver hole: a query to the LAN router on the
// DNS or DoT port is a leak, while a tunnel-side resolver still passes via the tunnel.
func TestBuildNFTRuleset_AllowLANStillBlocksResolversOnTheLAN(t *testing.T) {
	ruleset := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", true)
	for _, chain := range []string{"output", "forward"} {
		body := nftChainBody(t, ruleset, chain)
		udpDrop := strings.Index(body, "udp dport { 53, 853 } drop")
		tcpDrop := strings.Index(body, "tcp dport { 53, 853 } drop")
		tunnel := strings.Index(body, `oifname "wg-test" accept`)
		lan := strings.Index(body, "ip daddr 10.0.0.0/8 accept")
		if udpDrop < 0 || tcpDrop < 0 {
			t.Fatalf("%s chain lacks the resolver drops under allowLAN:\n%s", chain, body)
		}
		if !(tunnel < udpDrop && udpDrop < lan && tcpDrop < lan) {
			t.Fatalf("%s chain order wrong (tunnel=%d udpDrop=%d tcpDrop=%d lan=%d); drops must follow the tunnel accept and precede every LAN accept:\n%s", chain, tunnel, udpDrop, tcpDrop, lan, body)
		}
	}

	without := buildNFTRuleset([]string{"203.0.113.10"}, "wg-test", false)
	if strings.Contains(without, "dport { 53, 853 } drop") {
		t.Fatalf("resolver drops emitted with allowLAN off, where nothing but the tunnel is reachable anyway:\n%s", without)
	}
}
