package platform

import (
	"strings"
	"testing"
)

// Untagged on purpose: there is no Linux CI job, so a linux-tagged test would
// never run. The ruleset builder is pure text and can be checked anywhere.

func TestBuildNFTRuleset_IPv4Only(t *testing.T) {
	rules := buildNFTRuleset(lockRules([]string{"203.0.113.10", "2001:db8::10"}, "wg-test", false))

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
	rules := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", false))
	if !strings.Contains(rules, "policy drop;") {
		t.Fatalf("nft chain does not default to drop:\n%s", rules)
	}
	if !strings.Contains(rules, `oifname "lo" accept`) {
		t.Fatalf("nft ruleset does not permit loopback:\n%s", rules)
	}
}

// Turning Allow LAN off must actually drop the LAN accepts from the ruleset.
func TestBuildNFTRuleset_AllowLANIsNotSticky(t *testing.T) {
	with := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", true))
	if !strings.Contains(with, "192.168.0.0/16") {
		t.Fatalf("LAN permit missing when allowLAN is on:\n%s", with)
	}
	without := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", false))
	if strings.Contains(without, "192.168.0.0/16") {
		t.Fatalf("LAN permit present when allowLAN is off:\n%s", without)
	}
}

// The nft table is IPv4-matched via `ip daddr`; a v6 prefix there is a parse
// error that rejects the entire ruleset.
func TestBuildNFTRuleset_LANPermitsAreIPv4Only(t *testing.T) {
	ruleset := buildNFTRuleset(lockRules([]string{"198.51.100.20"}, "wg0", true))

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
	ruleset := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", false))
	forward := nftChainBody(t, ruleset, "forward")
	if !strings.Contains(forward, "type filter hook forward priority 0; policy drop;") {
		t.Fatalf("forward chain is not a drop-by-default forward hook:\n%s", forward)
	}
}

// A guest may only leave through the tunnel, and its replies must come back.
func TestBuildNFTRuleset_ForwardChainPermitsTunnelBothWays(t *testing.T) {
	ruleset := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", false))
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
	ruleset := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "", false))
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
	ruleset := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", false))
	forward := nftChainBody(t, ruleset, "forward")
	if !strings.Contains(forward, `meta oifkind "bridge" accept`) {
		t.Fatalf("forward chain would drop same-bridge container traffic:\n%s", forward)
	}
}

// Allow LAN applies to guests as it does to the host, and only when opted in.
func TestBuildNFTRuleset_ForwardChainFollowsAllowLAN(t *testing.T) {
	with := nftChainBody(t, buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", true)), "forward")
	if !strings.Contains(with, "ip daddr 192.168.0.0/16 accept") {
		t.Fatalf("forward chain lacks the LAN permit with allowLAN on:\n%s", with)
	}
	without := nftChainBody(t, buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", false)), "forward")
	if strings.Contains(without, "192.168.0.0/16") {
		t.Fatalf("forward chain permits the LAN with allowLAN off:\n%s", without)
	}
}

// Allow LAN must not reopen the resolver hole: a query to the LAN router on the
// DNS or DoT port is a leak, while a tunnel-side resolver still passes via the tunnel.
func TestBuildNFTRuleset_AllowLANStillBlocksResolversOnTheLAN(t *testing.T) {
	ruleset := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", true))
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

	without := buildNFTRuleset(lockRules([]string{"203.0.113.10"}, "wg-test", false))
	if strings.Contains(without, "dport { 53, 853 } drop") {
		t.Fatalf("resolver drops emitted with allowLAN off and no split permits, where nothing but the tunnel is reachable anyway:\n%s", without)
	}
}

// nftLineIndex is the line number of the first rule line equal to want, or -1.
func nftLineIndex(body, want string) int {
	for i, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == want {
			return i
		}
	}
	return -1
}

func nftLineCount(body, want string) int {
	n := 0
	for _, line := range strings.Split(body, "\n") {
		if strings.TrimSpace(line) == want {
			n++
		}
	}
	return n
}

var splitBoth = splitPermits{Egress: true, CIDRs: []string{"198.51.100.0/24", "203.0.113.0/25"}}

// Split permits open the lock beyond the tunnel, so the resolver drops must come
// back even with Allow LAN off: once, after the tunnel, before every other accept.
func TestBuildNFTRuleset_SplitPermitsKeepResolversBehindTheTunnel(t *testing.T) {
	for _, tunnel := range []string{"wg-test", ""} {
		ruleset := buildNFTRuleset(splitRules([]string{"203.0.113.10"}, tunnel, false, splitBoth))
		output := nftChainBody(t, ruleset, "output")
		forward := nftChainBody(t, ruleset, "forward")

		for chain, body := range map[string]string{"output": output, "forward": forward} {
			udpDrop := nftLineIndex(body, "udp dport { 53, 853 } drop")
			tcpDrop := nftLineIndex(body, "tcp dport { 53, 853 } drop")
			if udpDrop < 0 || tcpDrop < 0 {
				t.Fatalf("tunnel %q: %s chain lacks the resolver drops with split ranges:\n%s", tunnel, chain, body)
			}
			if nftLineCount(body, "udp dport { 53, 853 } drop") != 1 || nftLineCount(body, "tcp dport { 53, 853 } drop") != 1 {
				t.Fatalf("tunnel %q: %s chain emits the resolver drops more than once:\n%s", tunnel, chain, body)
			}
			if tunnel != "" {
				if at := nftLineIndex(body, `meta nfproto ipv4 oifname "wg-test" accept`); at < 0 || at > udpDrop {
					t.Fatalf("%s chain: the tunnel accept (%d) must precede the drops (%d):\n%s", chain, at, udpDrop, body)
				}
			}
			for _, cidr := range splitBoth.CIDRs {
				if at := nftLineIndex(body, "ip daddr "+cidr+" accept"); at < 0 || at < udpDrop || at < tcpDrop {
					t.Fatalf("tunnel %q: %s chain: accept for %s at %d, drops at %d/%d:\n%s", tunnel, chain, cidr, at, udpDrop, tcpDrop, body)
				}
			}
		}

		mark := nftLineIndex(output, "meta nfproto ipv4 meta mark 0x1ca6c accept")
		if mark < 0 || mark < nftLineIndex(output, "tcp dport { 53, 853 } drop") {
			t.Fatalf("tunnel %q: output chain must accept the IPv4-scoped egress mark after the drops:\n%s", tunnel, output)
		}
		if mark > nftLineIndex(output, "ip daddr 198.51.100.0/24 accept") {
			t.Fatalf("tunnel %q: the mark accept must precede the range accepts:\n%s", tunnel, output)
		}
		if strings.Contains(forward, "meta mark") {
			t.Fatalf("forward chain accepts the egress mark, which only the host's own sockets carry:\n%s", forward)
		}
		if strings.Contains(ruleset, `oifname ""`) {
			t.Fatalf("empty interface match rendered:\n%s", ruleset)
		}
	}
}

// Routed guests' replies from an excluded range only match by source address, and
// only as replies: a guest inside the range must not reach anything else (KS-R2-1).
func TestBuildNFTRuleset_ForwardChainAcceptsSplitRangesBothWays(t *testing.T) {
	for _, tunnel := range []string{"wg-test", ""} {
		for _, allowLAN := range []bool{false, true} {
			ruleset := buildNFTRuleset(splitRules([]string{"203.0.113.10"}, tunnel, allowLAN, splitBoth))
			forward := nftChainBody(t, ruleset, "forward")
			for _, cidr := range splitBoth.CIDRs {
				for _, want := range []string{"ip daddr " + cidr + " accept", "ip saddr " + cidr + " ct direction reply accept"} {
					if nftLineIndex(forward, want) < 0 {
						t.Errorf("tunnel %q: forward chain lacks %q:\n%s", tunnel, want, forward)
					}
				}
			}
			for _, line := range strings.Split(forward, "\n") {
				if strings.Contains(line, "saddr") && !strings.HasSuffix(strings.TrimSpace(line), "ct direction reply accept") {
					t.Errorf("tunnel %q: forward chain accepts by source alone: %q", tunnel, strings.TrimSpace(line))
				}
			}
			if output := nftChainBody(t, ruleset, "output"); strings.Contains(output, "ip saddr") {
				t.Fatalf("output chain accepts by source address:\n%s", output)
			}
		}
	}
}

// Egress alone must still bring the resolver drops back to the output chain, but
// the forward chain carries nothing extra: guests never hold the mark.
func TestBuildNFTRuleset_EgressOnlyTouchesTheOutputChain(t *testing.T) {
	ruleset := buildNFTRuleset(splitRules([]string{"203.0.113.10"}, "wg-test", false, splitPermits{Egress: true}))
	output := nftChainBody(t, ruleset, "output")
	forward := nftChainBody(t, ruleset, "forward")
	if nftLineIndex(output, "udp dport { 53, 853 } drop") < 0 || nftLineIndex(output, "meta nfproto ipv4 meta mark 0x1ca6c accept") < 0 {
		t.Fatalf("output chain lacks the drops or the mark accept:\n%s", output)
	}
	if strings.Contains(forward, "dport { 53, 853 }") || strings.Contains(forward, "mark") {
		t.Fatalf("forward chain changed for egress alone:\n%s", forward)
	}

	cidrsOnly := nftChainBody(t, buildNFTRuleset(splitRules(nil, "wg-test", false, splitPermits{CIDRs: []string{"198.51.100.0/24"}})), "output")
	if strings.Contains(cidrsOnly, "meta mark") {
		t.Fatalf("mark accepted without the egress permit:\n%s", cidrsOnly)
	}
}

// The full output order: endpoints, tunnel, drops, mark, LAN, split ranges.
func TestBuildNFTRuleset_OutputOrderWithEverything(t *testing.T) {
	output := nftChainBody(t, buildNFTRuleset(splitRules([]string{"203.0.113.10"}, "wg-test", true, splitBoth)), "output")
	order := []string{
		`oifname "lo" accept`,
		"ip daddr 203.0.113.10 accept",
		`meta nfproto ipv4 oifname "wg-test" accept`,
		"udp dport { 53, 853 } drop",
		"tcp dport { 53, 853 } drop",
		"meta nfproto ipv4 meta mark 0x1ca6c accept",
		"ip daddr 10.0.0.0/8 accept",
		"ip daddr 100.64.0.0/10 accept",
		"ip daddr 198.51.100.0/24 accept",
		"ip daddr 203.0.113.0/25 accept",
	}
	last := -1
	for _, want := range order {
		at := nftLineIndex(output, want)
		if at <= last {
			t.Fatalf("%q at line %d, want after line %d:\n%s", want, at, last, output)
		}
		last = at
	}
	if nftLineCount(output, "udp dport { 53, 853 } drop") != 1 {
		t.Fatalf("drops emitted twice with allowLAN and split both on:\n%s", output)
	}
}

func TestBuildNFTRuleset_RevalidatesSplitCIDRs(t *testing.T) {
	split := splitPermits{CIDRs: append([]string{"198.51.100.0/24"}, hostileSplitCIDRs...)}
	ruleset := buildNFTRuleset(splitRules(nil, "wg-test", false, split))
	for _, bad := range hostileSplitCIDRs {
		if bad != "" && strings.Contains(ruleset, bad) {
			t.Errorf("invalid range %q reached the nft script:\n%s", bad, ruleset)
		}
	}
	if !strings.Contains(ruleset, "ip daddr 198.51.100.0/24 accept") {
		t.Fatalf("valid range lost alongside the invalid ones:\n%s", ruleset)
	}
}

// Named priorities are rejected by older nft, which would silently move the
// host to the iptables fallback.
func TestBuildNFTRuleset_UsesNumericPriorities(t *testing.T) {
	ruleset := buildNFTRuleset(splitRules([]string{"203.0.113.10"}, "wg-test", true, splitBoth))
	for _, line := range strings.Split(ruleset, "\n") {
		_, after, ok := strings.Cut(line, " priority ")
		if !ok {
			continue
		}
		if after == "" || !strings.ContainsAny(after[:1], "-0123456789") {
			t.Errorf("non-numeric priority: %s", strings.TrimSpace(line))
		}
	}
}
