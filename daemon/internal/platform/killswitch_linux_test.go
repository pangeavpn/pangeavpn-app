//go:build linux

package platform

import (
	"context"
	"strings"
	"testing"
)

func TestBuildNFTRuleset_IPv4Only(t *testing.T) {
	rules := buildNFTRuleset([]string{"203.0.113.10", "2001:db8::10"}, "wg-test")

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

func TestApplyIPTablesRules_InstallsIPv6DropChain(t *testing.T) {
	originalRunner := runIPTablesCommand
	defer func() {
		runIPTablesCommand = originalRunner
	}()

	type call struct {
		bin  string
		args string
	}
	calls := make([]call, 0, 24)
	runIPTablesCommand = func(_ context.Context, binary string, args ...string) error {
		calls = append(calls, call{
			bin:  binary,
			args: strings.Join(args, " "),
		})
		return nil
	}

	if err := applyIPTablesRules(context.Background(), []string{"203.0.113.5"}, "wg-test"); err != nil {
		t.Fatalf("applyIPTablesRules failed: %v", err)
	}

	required := []call{
		{bin: "ip6tables", args: "-N " + ipt6ChainName},
		{bin: "ip6tables", args: "-A " + ipt6ChainName + " -o lo -j ACCEPT"},
		{bin: "ip6tables", args: "-A " + ipt6ChainName + " -j DROP"},
		{bin: "ip6tables", args: "-I OUTPUT 1 -j " + ipt6ChainName},
	}
	for _, req := range required {
		found := false
		for _, got := range calls {
			if got.bin == req.bin && got.args == req.args {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing required command %s %s, calls=%v", req.bin, req.args, calls)
		}
	}
}

func TestRemoveIPTablesRules_RemovesIPv6Chain(t *testing.T) {
	originalRunner := runIPTablesCommand
	defer func() {
		runIPTablesCommand = originalRunner
	}()

	type call struct {
		bin  string
		args string
	}
	calls := make([]call, 0, 12)
	runIPTablesCommand = func(_ context.Context, binary string, args ...string) error {
		calls = append(calls, call{
			bin:  binary,
			args: strings.Join(args, " "),
		})
		return nil
	}

	if err := removeIPTablesRules(context.Background()); err != nil {
		t.Fatalf("removeIPTablesRules failed: %v", err)
	}

	required := []call{
		{bin: "ip6tables", args: "-D OUTPUT -j " + ipt6ChainName},
		{bin: "ip6tables", args: "-F " + ipt6ChainName},
		{bin: "ip6tables", args: "-X " + ipt6ChainName},
	}
	for _, req := range required {
		found := false
		for _, got := range calls {
			if got.bin == req.bin && got.args == req.args {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing required command %s %s, calls=%v", req.bin, req.args, calls)
		}
	}
}
