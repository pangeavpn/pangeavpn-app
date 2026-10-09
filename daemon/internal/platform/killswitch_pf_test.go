package platform

import (
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
)

// Untagged on purpose: there is no macOS CI job, and the ruleset is pure text
// that can be checked anywhere.

func TestBuildPFRules_IPv4AndIPv6AllowRules(t *testing.T) {
	rules, err := buildPFRules(lockRules([]string{"198.51.100.20", "2001:db8::20"}, "utun9", false))
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
	if _, err := buildPFRules(lockRules(nil, "utun9; block", false)); err == nil {
		t.Fatal("expected error for invalid tunnel interface name")
	}
}

func TestBuildPFRules_LANPrefixesUseMatchingAddressFamily(t *testing.T) {
	rules, err := buildPFRules(lockRules([]string{"198.51.100.20"}, "utun9", true))
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
	rules, err := buildPFRules(lockRules(nil, "utun9", false))
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
	rules, err := buildPFRules(lockRules([]string{"198.51.100.20"}, "utun9", false))
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
	rules, err := buildPFRules(lockRules([]string{"198.51.100.20"}, "utun9", true))
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
	rules, err := buildPFRules(lockRules([]string{"198.51.100.20"}, "utun9", false))
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
	rules, err := buildPFRules(lockRules([]string{"198.51.100.20"}, "utun9", true))
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
	rules, err := buildPFRules(lockRules([]string{"198.51.100.20"}, "utun9", true))
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

	without, err := buildPFRules(lockRules([]string{"198.51.100.20"}, "utun9", false))
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if strings.Contains(without, "port { 53 853 }") {
		t.Fatalf("resolver blocks emitted with allowLAN off and no split permits:\n%s", without)
	}
}

// pfEvaluatedRules is what pf evaluates: the anchor file with the in-memory split
// anchor's rules in place of its hook.
func pfEvaluatedRules(t *testing.T, r ksRules, gid int) string {
	t.Helper()
	rules, err := buildPFRules(r)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	hook := `anchor "` + pfSplitAnchor + `"` + "\n"
	if strings.Count(rules, hook) != 1 {
		t.Fatalf("anchor file must hook the split anchor exactly once:\n%s", rules)
	}
	split := strings.Join(pfSplitRules(r.Split, gid), "\n")
	if split != "" {
		split += "\n"
	}
	return strings.Replace(rules, hook, split, 1)
}

// KS-R2-4: the boot job reloads the anchor file before the daemon runs, so split
// permits must only ever live in the kernel's split anchor.
func TestBuildPFRules_SplitRulesStayOutOfTheAnchorFile(t *testing.T) {
	for _, tunnel := range []string{"utun9", ""} {
		for _, allowLAN := range []bool{false, true} {
			rules, err := buildPFRules(splitRules([]string{"198.51.100.20"}, tunnel, allowLAN, splitBoth))
			if err != nil {
				t.Fatalf("buildPFRules: %v", err)
			}
			for _, leak := range []string{"198.51.100.0/24", "203.0.113.0/25", "group"} {
				if strings.Contains(rules, leak) {
					t.Fatalf("tunnel %q: the anchor file carries %q:\n%s", tunnel, leak, rules)
				}
			}
			hook, blockAll := pfLineIndex(rules, `anchor "split"`), pfLineIndex(rules, "block out all")
			if hook < 0 || hook > blockAll || (tunnel != "" && hook < pfLineIndex(rules, "pass out quick on utun9 all")) {
				t.Fatalf("tunnel %q: split hook at %d, want after the tunnel pass and before block out all (%d):\n%s", tunnel, hook, blockAll, rules)
			}
			if allowLAN && hook < pfLineIndex(rules, "pass in quick inet from 192.168.0.0/16") {
				t.Fatalf("split hook precedes the LAN rules:\n%s", rules)
			}
		}
	}
	split := strings.Join(pfSplitRules(splitBoth, 437), "\n")
	for _, want := range []string{"pass out quick inet to 198.51.100.0/24", "pass out quick inet to 203.0.113.0/25", "group 437"} {
		if !strings.Contains(split, want) {
			t.Fatalf("split anchor lacks %q:\n%s", want, split)
		}
	}
	if strings.Contains(split, "pass in") {
		t.Fatalf("split anchor admits unsolicited inbound from an excluded range:\n%s", split)
	}
}

func pfLineIndex(rules, want string) int {
	for i, line := range strings.Split(rules, "\n") {
		if line == want {
			return i
		}
	}
	return -1
}

// Excluded ranges pass out (replies by state), but a resolver inside one is still
// blocked first; every split rule lands before block out all, tunnel or not.
func TestBuildPFRules_SplitRangesPassButResolversInThemDoNot(t *testing.T) {
	split := splitPermits{CIDRs: []string{"198.51.100.0/24", "203.0.113.0/25"}}
	for _, tunnel := range []string{"utun9", ""} {
		rules := pfEvaluatedRules(t, splitRules([]string{"198.51.100.20"}, tunnel, false, split), 0)
		blockAll := pfLineIndex(rules, "block out all")
		for _, cidr := range split.CIDRs {
			block := pfLineIndex(rules, "block out quick inet proto { tcp udp } to "+cidr+" port { 53 853 }")
			out := pfLineIndex(rules, "pass out quick inet to "+cidr)
			if block < 0 || out < 0 {
				t.Fatalf("tunnel %q: rules for %s missing:\n%s", tunnel, cidr, rules)
			}
			if !(block < out && out < blockAll) {
				t.Fatalf("tunnel %q: %s order wrong (block=%d out=%d blockAll=%d):\n%s", tunnel, cidr, block, out, blockAll, rules)
			}
			if pfLineIndex(rules, "pass in quick inet from "+cidr) >= 0 {
				t.Fatalf("tunnel %q: %s admits unsolicited inbound:\n%s", tunnel, cidr, rules)
			}
		}
		if tunnel != "" && pfLineIndex(rules, "pass out quick on utun9 all") > pfLineIndex(rules, "block out quick inet proto { tcp udp } to 198.51.100.0/24 port { 53 853 }") {
			t.Fatalf("a resolver block precedes the tunnel pass, so tunnel DNS would break:\n%s", rules)
		}
		if strings.Contains(rules, "group") {
			t.Fatalf("group rule rendered without the egress permit:\n%s", rules)
		}
	}
}

func TestBuildPFRules_SplitEgressPassesTheGroupExceptToResolvers(t *testing.T) {
	rules := pfEvaluatedRules(t, splitRules([]string{"198.51.100.20"}, "utun9", false, splitPermits{Egress: true}), 437)
	block := pfLineIndex(rules, "block out quick proto { tcp udp } to any port { 53 853 } group 437")
	pass := pfLineIndex(rules, "pass out quick inet proto { tcp udp } from any to any group 437")
	blockAll := pfLineIndex(rules, "block out all")
	if block < 0 || pass < 0 || !(block < pass && pass < blockAll) {
		t.Fatalf("group rules missing or misordered (block=%d pass=%d blockAll=%d):\n%s", block, pass, blockAll, rules)
	}
	if pfLineIndex(rules, "pass out quick on utun9 all") > block {
		t.Fatalf("group rules precede the tunnel pass:\n%s", rules)
	}
}

// Without a resolved group there is nothing safe to match on: no pass at all.
func TestBuildPFRules_SplitEgressWithoutAGroupRendersNothing(t *testing.T) {
	rules := pfEvaluatedRules(t, splitRules(nil, "utun9", false, splitPermits{Egress: true}), 0)
	if strings.Contains(rules, "group") {
		t.Fatalf("egress pass rendered with no group:\n%s", rules)
	}
	if off := pfEvaluatedRules(t, splitRules(nil, "utun9", false, splitPermits{}), 437); strings.Contains(off, "group") {
		t.Fatalf("egress pass rendered with the permit off:\n%s", off)
	}
}

func TestBuildPFRules_RevalidatesSplitCIDRs(t *testing.T) {
	split := splitPermits{CIDRs: append([]string{"198.51.100.0/24"}, hostileSplitCIDRs...)}
	rules := pfEvaluatedRules(t, splitRules(nil, "utun9", false, split), 0)
	for _, bad := range hostileSplitCIDRs {
		if bad != "" && strings.Contains(rules, bad) {
			t.Errorf("invalid range %q reached the anchor:\n%s", bad, rules)
		}
	}
	if pfLineIndex(rules, "pass out quick inet to 198.51.100.0/24") < 0 {
		t.Fatalf("valid range lost alongside the invalid ones:\n%s", rules)
	}
	if strings.Contains(rules, "pass out quick inet to \n") || strings.Contains(rules, "from \n") {
		t.Fatalf("empty range rendered:\n%s", rules)
	}
}

// verifyPFAnchorLive keys on a block ... out all line; no split rule may look like one.
func TestBuildPFRules_SplitRulesCannotPassForTheBlockAll(t *testing.T) {
	rules := pfEvaluatedRules(t, splitRules(nil, "", false, splitBoth), 437)
	for _, line := range strings.Split(rules, "\n") {
		if strings.Contains(line, "block") && strings.Contains(line, "out all") && line != "block out all" {
			t.Fatalf("split rule %q would satisfy the live-anchor check", line)
		}
	}
}

// pfState mirrors XNU's DIOCKILLSTATES view: an outbound state's source is its
// local address, an inbound one's its remote address.
type pfState struct {
	out           bool
	local, remote netip.Addr
	desc          string
}

// pfKillMatches ports XNU's predicate: one -k leaves the destination zeroed, matching anything.
func pfKillMatches(args []string, s pfState) bool {
	var keys []netip.Prefix
	for i := 0; i+1 < len(args); i += 2 {
		keys = append(keys, netip.MustParsePrefix(args[i+1]))
	}
	src, dst := s.local, s.remote
	if !s.out {
		src, dst = s.remote, s.local
	}
	return keys[0].Contains(src) && (len(keys) < 2 || keys[1].Contains(dst))
}

// KS-R2-2: ending a removed range's flows must never reach a state whose local
// address merely lies inside it, like the transport's own connection.
func TestPFKillArgs_EndOnlyFlowsBetweenTheHostAndTheRange(t *testing.T) {
	mac := netip.MustParseAddr("192.168.1.5")
	states := []pfState{
		{true, mac, netip.MustParseAddr("203.0.113.10"), "transport to the endpoint"},
		{true, mac, netip.MustParseAddr("198.51.100.7"), "flow to another excluded range"},
		{true, netip.MustParseAddr("10.66.0.2"), netip.MustParseAddr("93.184.216.34"), "tunnelled flow"},
		{true, mac, netip.MustParseAddr("192.168.1.20"), "flow into the range"},
		{false, mac, netip.MustParseAddr("192.168.1.20"), "flow from the range"},
	}
	args := pfKillArgs([]netip.Addr{mac}, []string{"192.168.1.0/24"})
	for _, a := range args {
		if strings.Count(strings.Join(a, " "), "-k") != 2 || !slices.Contains(a, mac.String()+"/32") {
			t.Errorf("pfctl %v is not scoped to a local address on one side", a)
		}
	}
	var killed []string
	for _, s := range states {
		if slices.ContainsFunc(args, func(a []string) bool { return pfKillMatches(a, s) }) {
			killed = append(killed, s.desc)
		}
	}
	if want := []string{"flow into the range", "flow from the range"}; !slices.Equal(killed, want) {
		t.Fatalf("killed %q, want only %q", killed, want)
	}
	if got := pfKillArgs(nil, []string{"192.168.1.0/24"}); len(got) != 0 {
		t.Fatalf("no local address still ran %v", got)
	}
}

func TestPFKillLocalAddrsSkipsLoopbackAndTheTunnel(t *testing.T) {
	all, err := pfKillLocalAddrs("")
	if err != nil {
		t.Fatal(err)
	}
	for _, a := range all {
		if !a.Is4() || a.IsLoopback() {
			t.Fatalf("local addresses include %s", a)
		}
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		addrs, _ := iface.Addrs()
		var own []netip.Addr
		for _, addr := range addrs {
			if n, ok := addr.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() {
				a, _ := netip.AddrFromSlice(n.IP.To4())
				own = append(own, a)
			}
		}
		if len(own) == 0 {
			continue
		}
		rest, err := pfKillLocalAddrs(iface.Name)
		if err != nil {
			t.Fatal(err)
		}
		for _, a := range own {
			if slices.Contains(rest, a) {
				t.Fatalf("tunnel %s's own address %s would have its flows killed", iface.Name, a)
			}
		}
		return
	}
	t.Skip("no interface with an IPv4 address")
}
