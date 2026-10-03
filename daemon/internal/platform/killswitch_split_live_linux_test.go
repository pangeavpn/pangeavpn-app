//go:build linux

package platform

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// requireScratchNetns lets a test apply real rules only as root inside a network
// namespace other than init's, e.g. under `ip netns exec <ns>`.
func requireScratchNetns(t *testing.T) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	self, errSelf := os.Readlink("/proc/self/ns/net")
	initNS, errInit := os.Readlink("/proc/1/ns/net")
	if errSelf != nil || errInit != nil || self == initNS {
		t.Skip("applies real rules, so it only runs inside a scratch network namespace")
	}
}

var liveSplitCases = map[string]ksRules{
	"tunnel, split":            splitRules([]string{"203.0.113.5"}, "wg-test", false, splitBoth),
	"no tunnel, split":         splitRules([]string{"203.0.113.5"}, "", false, splitBoth),
	"allow LAN, split":         splitRules([]string{"203.0.113.5"}, "wg-test", true, splitBoth),
	"egress only":              splitRules(nil, "wg-test", false, splitPermits{Egress: true}),
	"ranges only, no endpoint": splitRules(nil, "", false, splitPermits{CIDRs: []string{"10.20.0.0/16"}}),
}

func TestLiveNFTAcceptsTheSplitRuleset(t *testing.T) {
	requireScratchNetns(t)
	ctx := context.Background()
	if !hasNFT(ctx) {
		t.Skip("no nft")
	}
	t.Cleanup(func() { _ = removeNFTRules(context.Background()) })
	for name, r := range liveSplitCases {
		if err := applyNFTRules(ctx, r); err != nil {
			t.Fatalf("%s: kernel rejected the ruleset: %v\n%s", name, err, buildNFTRuleset(r))
		}
		out, err := exec.Command("nft", "list", "table", nftFamily, nftTableName).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: list table: %v (%s)", name, err, out)
		}
		listed := string(out)
		if r.Split.Egress != strings.Contains(listed, "meta mark 0x0001ca6c accept") {
			t.Fatalf("%s: egress mark accept present=%v, want %v:\n%s", name, !r.Split.Egress, r.Split.Egress, listed)
		}
		for _, cidr := range r.Split.CIDRs {
			if !strings.Contains(listed, "ip saddr "+cidr+" ct direction reply accept") || strings.Contains(listed, "ip saddr "+cidr+" accept") {
				t.Fatalf("%s: forward reply accept for %s missing or not reply-only:\n%s", name, cidr, listed)
			}
		}
	}
}

func TestLiveIPTablesAcceptsTheSplitPlan(t *testing.T) {
	requireScratchNetns(t)
	if _, err := exec.LookPath("iptables"); err != nil {
		t.Skip("no iptables")
	}
	ctx := context.Background()
	t.Cleanup(func() { _ = removeIPTablesRules(context.Background()) })
	for name, r := range liveSplitCases {
		if err := applyIPTablesRules(ctx, r); err != nil {
			t.Fatalf("%s: iptables rejected the plan: %v", name, err)
		}
		out, err := exec.Command("iptables", "-w", "5", "-S").CombinedOutput()
		if err != nil {
			t.Fatalf("%s: iptables -S: %v (%s)", name, err, out)
		}
		listed := string(out)
		if r.Split.Egress != strings.Contains(listed, "-m mark --mark 0x1ca6c -j ACCEPT") {
			t.Fatalf("%s: egress mark accept present=%v, want %v:\n%s", name, !r.Split.Egress, r.Split.Egress, listed)
		}
		for _, cidr := range r.Split.CIDRs {
			if !strings.Contains(listed, "-s "+cidr+" -m conntrack --ctdir REPLY -j ACCEPT") || strings.Contains(listed, "-s "+cidr+" -j ACCEPT") || !strings.Contains(listed, "-d "+cidr+" -j ACCEPT") {
				t.Fatalf("%s: rules for %s missing:\n%s", name, cidr, listed)
			}
		}
		v6, err := exec.Command("ip6tables", "-w", "5", "-S").CombinedOutput()
		if err == nil && strings.Contains(string(v6), "mark") {
			t.Fatalf("%s: IPv6 chain accepts the egress mark:\n%s", name, v6)
		}
	}
}
