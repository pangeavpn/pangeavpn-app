package platform

import (
	"context"
	"slices"
	"strings"
	"testing"
)

func TestNormalizeDaemonIPs_KeepsOnlyPublicAddresses(t *testing.T) {
	got := normalizeDaemonIPs([]string{
		"172.67.220.125", "104.21.24.200", " 104.21.24.200 ", "::ffff:104.21.24.200",
		"10.1.2.3", "192.168.0.3", "172.16.0.1", "100.64.0.9", "127.0.0.1", "169.254.1.1",
		"0.0.0.0", "255.255.255.255", "224.0.0.1", "fc00::1", "fe80::1", "not-an-ip",
		"2606:4700::6810:18c8",
	})
	want := []string{"104.21.24.200", "172.67.220.125", "2606:4700::6810:18c8"}
	if !slices.Equal(got, want) {
		t.Fatalf("normalizeDaemonIPs() = %v, want %v", got, want)
	}
}

// An edge name answering with a hub or node address leaves that address dedicated.
func TestResolveDaemonEgressHosts_LeavesDedicatedAddressesAlone(t *testing.T) {
	got := resolveDaemonEgressHosts(context.Background(), []string{"104.21.24.200", "203.0.113.5"}, []string{"203.0.113.5", "garbage"})
	if !slices.Equal(got, []string{"104.21.24.200"}) {
		t.Fatalf("resolveDaemonEgressHosts() = %v, want the dedicated address left out", got)
	}
}

func TestWithoutDaemonIPs_StripsSharedEdgesFromAllAppPermits(t *testing.T) {
	got := withoutDaemonIPs([]string{"203.0.113.5", "104.21.24.200", "::ffff:172.67.220.125"}, []string{"104.21.24.200", "172.67.220.125"})
	if !slices.Equal(got, []string{"203.0.113.5"}) {
		t.Fatalf("withoutDaemonIPs() = %v, want only the dedicated endpoint", got)
	}
}

func TestNarrowedSplit_KeepsOnlyDaemonIPsBothWantedAndApplied(t *testing.T) {
	got := narrowedSplit(splitPermits{DaemonIPs: []string{"104.21.24.200"}}, splitPermits{DaemonIPs: []string{"104.21.24.200", "172.67.220.125"}})
	if !slices.Equal(got.DaemonIPs, []string{"104.21.24.200"}) {
		t.Fatalf("narrowedSplit().DaemonIPs = %v", got.DaemonIPs)
	}
}

// pf can't match a process, so it matches the egress broker's memberless group, which only
// the daemon's pinned sockets carry: root's own sockets and any user's must hit block-all.
func TestPFDaemonEgress_PassesOnlyTheBrokersGroup(t *testing.T) {
	rules := pfSplitRules(splitPermits{DaemonIPs: []string{"104.21.24.200", "2606:4700::6810:18c8", "10.0.0.1", "1.2.3.4 port 53"}}, 4242)
	want := []string{
		"block out quick inet proto { tcp udp } to 104.21.24.200 port { 53 853 }",
		"pass out quick inet proto tcp to 104.21.24.200 group 4242",
	}
	if !slices.Equal(rules, want) {
		t.Fatalf("pfSplitRules() = %q, want %q", rules, want)
	}
	if rules := pfSplitRules(splitPermits{DaemonIPs: []string{"104.21.24.200"}}, 0); len(rules) != 0 {
		t.Fatalf("with no verified group the edge still got rules: %v", rules)
	}
	main, err := buildPFRules(ksRules{EndpointIPs: []string{"203.0.113.5"}, Split: splitPermits{DaemonIPs: []string{"104.21.24.200"}}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(main, "104.21.24.200") {
		t.Fatalf("the persisted anchor file names a daemon-only edge, which the boot job would re-arm: %q", main)
	}
}

func TestPruneSharedFromState_KeepsSharedEdgesOffDisk(t *testing.T) {
	isolateStateDir(t)
	if err := saveKillSwitchState(KillSwitchState{Active: true, Locked: true, EndpointIPs: []string{"104.21.24.200", "203.0.113.5"}}); err != nil {
		t.Fatal(err)
	}
	pruneSharedFromState([]string{"104.21.24.200"})
	st, err := loadKillSwitchState()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(st.EndpointIPs, []string{"203.0.113.5"}) || !st.Locked {
		t.Fatalf("saved state = %+v, want the edge gone and Lockdown kept", st)
	}
}

func TestNFTDaemonEgress_AcceptsMarkedSocketsOnly(t *testing.T) {
	ruleset := buildNFTRuleset(ksRules{EndpointIPs: []string{"203.0.113.5"}, Tunnel: "wg0", Split: splitPermits{DaemonIPs: []string{"104.21.24.200", "2606:4700::1"}}})
	if !strings.Contains(ruleset, "ip daddr 104.21.24.200 meta mark 0x1ca6c accept") {
		t.Fatalf("no marked-socket accept for the edge:\n%s", ruleset)
	}
	if strings.Contains(ruleset, "ip daddr 104.21.24.200 accept") || strings.Contains(ruleset, "2606:4700::1") {
		t.Fatalf("the edge is reachable by unmarked sockets, or IPv6 leaked into the v4 rules:\n%s", ruleset)
	}
	drop := strings.Index(ruleset, "udp dport { 53, 853 } drop")
	accept := strings.Index(ruleset, "ip daddr 104.21.24.200 meta mark")
	if drop < 0 || drop > accept {
		t.Fatalf("resolver drops must precede the edge accept so it can't carry DNS:\n%s", ruleset)
	}
}

func TestIPTablesDaemonEgress_AcceptsMarkedSocketsOnly(t *testing.T) {
	plan := iptablesApplyPlan(iptChainName, ipt6ChainName, ksRules{EndpointIPs: []string{"203.0.113.5"}, Split: splitPermits{DaemonIPs: []string{"104.21.24.200"}}})
	var marked, plain, resolverDrop bool
	for _, cmd := range plan {
		line := strings.Join(cmd.Args, " ")
		if strings.Contains(line, "-d 104.21.24.200") {
			if strings.Contains(line, "--mark "+splitEgressMark) {
				marked = true
			} else {
				plain = true
			}
		}
		if strings.Contains(line, "--dport 53") && !marked {
			resolverDrop = true
		}
	}
	if !marked || plain || !resolverDrop {
		t.Fatalf("marked=%v plain=%v resolverDropFirst=%v; want only a marked accept, after the resolver drops", marked, plain, resolverDrop)
	}
}
