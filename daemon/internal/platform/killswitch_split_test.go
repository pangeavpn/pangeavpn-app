package platform

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

func lockRules(endpointIPs []string, tunnel string, allowLAN bool) ksRules {
	return ksRules{EndpointIPs: endpointIPs, Tunnel: tunnel, AllowLAN: allowLAN}
}

func splitRules(endpointIPs []string, tunnel string, allowLAN bool, split splitPermits) ksRules {
	r := lockRules(endpointIPs, tunnel, allowLAN)
	r.Split = split
	return r
}

// Rule text that could only reach a renderer by a path the setters never take.
var hostileSplitCIDRs = []string{
	"10.9.0.0/16 accept; drop",
	"10.9.0.1/16",
	"2001:db8::/32",
	"0.0.0.0/0",
	"10.0.0.0/4",
	"not-a-cidr",
	"",
}

func TestSplitEgressMarkMatchesEgress(t *testing.T) {
	if want := fmt.Sprintf("%#x", egress.SplitMark); splitEgressMark != want {
		t.Fatalf("kill switch accepts mark %s but bypass sockets carry %s", splitEgressMark, want)
	}
}

func TestNormalizeSplitCIDRs(t *testing.T) {
	got, err := normalizeSplitCIDRs([]string{" 198.51.100.0/24", "10.1.0.0/16", "198.51.100.0/24", "203.0.113.7/32"})
	if err != nil {
		t.Fatalf("normalizeSplitCIDRs: %v", err)
	}
	want := []string{"10.1.0.0/16", "198.51.100.0/24", "203.0.113.7/32"}
	if !slices.Equal(got, want) {
		t.Fatalf("normalized = %v, want %v", got, want)
	}

	for _, bad := range []string{"203.0.113.7", "198.51.100.1/24", "2001:db8::/32", "10.0.0.0/7", "0.0.0.0/0", "x"} {
		if _, err := normalizeSplitCIDRs([]string{"10.1.0.0/16", bad}); err == nil {
			t.Errorf("%q accepted; a set with one bad range must be refused whole", bad)
		}
	}
	if got, err := normalizeSplitCIDRs(nil); err != nil || len(got) != 0 {
		t.Fatalf("empty set = %v, %v", got, err)
	}
}

func TestRenderableSplitCIDRsDropsAnythingASetterWouldRefuse(t *testing.T) {
	got := renderableSplitCIDRs(append([]string{"198.51.100.0/24"}, hostileSplitCIDRs...))
	if !slices.Equal(got, []string{"198.51.100.0/24"}) {
		t.Fatalf("renderable = %v, want only the canonical IPv4 range", got)
	}
}

// A failed change keeps removals (so the next render retries them) and drops
// additions: the desired set never ends up wider than either side.
func TestNarrowedSplit(t *testing.T) {
	prev := splitPermits{Egress: true, CIDRs: []string{"10.1.0.0/16", "10.2.0.0/16"}}
	next := splitPermits{Egress: false, CIDRs: []string{"10.2.0.0/16", "10.3.0.0/16"}}
	got := narrowedSplit(prev, next)
	if got.Egress || !slices.Equal(got.CIDRs, []string{"10.2.0.0/16"}) {
		t.Fatalf("narrowed = %+v, want no egress and only 10.2.0.0/16", got)
	}
	if got := narrowedSplit(splitPermits{}, splitPermits{Egress: true, CIDRs: []string{"10.1.0.0/16"}}); !got.empty() {
		t.Fatalf("a failed widening left %+v desired", got)
	}
}

func TestSplitRangesToKill(t *testing.T) {
	split := func(cidrs ...string) ksRules { return splitRules(nil, "utun4", false, splitPermits{CIDRs: cidrs}) }
	if got := splitRangesToKill([]string{"10.1.0.0/16", "10.2.0.0/16", "bogus"}, split("10.2.0.0/16")); !slices.Equal(got, []string{"10.1.0.0/16"}) {
		t.Fatalf("removed = %v, want [10.1.0.0/16]", got)
	}
	// KS-R2-2: a range still permitted some other way keeps its established flows.
	if got := splitRangesToKill([]string{"10.1.0.0/16"}, split("10.0.0.0/8")); len(got) != 0 {
		t.Fatalf("widening to 10.0.0.0/8 killed %v", got)
	}
	lan := splitRules(nil, "utun4", true, splitPermits{})
	if got := splitRangesToKill([]string{"192.168.1.0/24"}, lan); len(got) != 0 {
		t.Fatalf("removing a range Allow LAN still permits killed %v", got)
	}

	got := splitRangesToKill([]string{"10.0.0.0/8", "10.1.0.0/16"}, split("10.1.0.0/16"))
	assertExactlyCovers(t, got, "10.0.0.0/8", "10.1.0.0/16")
	if len(got) != 8 {
		t.Fatalf("leftover of 10.0.0.0/8 minus 10.1.0.0/16 = %v, want the 8 sibling prefixes", got)
	}

	endpoint := splitRules([]string{"203.0.113.5", "2001:db8::5"}, "", false, splitPermits{})
	assertExactlyCovers(t, splitRangesToKill([]string{"203.0.113.0/24"}, endpoint), "203.0.113.0/24", "203.0.113.5/32")
}

// assertExactlyCovers checks got is disjoint, inside whole, and covers all of it but hole.
func assertExactlyCovers(t *testing.T, got []string, whole, hole string) {
	t.Helper()
	w, h := netip.MustParsePrefix(whole), netip.MustParsePrefix(hole)
	var size uint64
	for i, cidr := range got {
		p := netip.MustParsePrefix(cidr)
		if p.Overlaps(h) || !w.Contains(p.Addr()) || p.Bits() < w.Bits() {
			t.Fatalf("%s is outside %s or overlaps %s: %v", cidr, whole, hole, got)
		}
		for _, other := range got[i+1:] {
			if p.Overlaps(netip.MustParsePrefix(other)) {
				t.Fatalf("%s and %s overlap: %v", cidr, other, got)
			}
		}
		size += 1 << (32 - p.Bits())
	}
	if want := uint64(1)<<(32-w.Bits()) - uint64(1)<<(32-h.Bits()); size != want {
		t.Fatalf("%v covers %d addresses, want %d", got, size, want)
	}
}

func TestSplitPermitsEqualTreatsNilAndEmptyAlike(t *testing.T) {
	if !(splitPermits{}).equal(splitPermits{CIDRs: []string{}}) {
		t.Fatal("an empty CIDR list differs from none, so every re-arm would re-render")
	}
	a := splitPermits{CIDRs: []string{"10.1.0.0/16"}}
	b := a.clone()
	b.CIDRs[0] = "10.2.0.0/16"
	if a.CIDRs[0] != "10.1.0.0/16" {
		t.Fatal("clone shares its CIDR slice with the original")
	}
}

func TestSplitEgressGIDFromString(t *testing.T) {
	if gid, err := splitEgressGIDFromString(" 437\n"); err != nil || gid != 437 {
		t.Fatalf("gid = %d, %v; want 437", gid, err)
	}
	for _, bad := range []string{"0", "20", "80", "-1", "abc", ""} {
		if _, err := splitEgressGIDFromString(bad); err == nil {
			t.Errorf("gid %q accepted; pf would pass a shared group's sockets", bad)
		}
	}
}

func TestDsclGroupHasMembers(t *testing.T) {
	cases := map[string]bool{
		"PrimaryGroupID: 437\nRecordName: _pangeasplit\n":                               false,
		"GroupMembership:\nPrimaryGroupID: 437\n":                                       false,
		"GroupMembership: alice\nPrimaryGroupID: 437\n":                                 true,
		"GroupMembership:\n alice bob\nPrimaryGroupID: 437\n":                           true,
		"GroupMembers: ABCDEF01-2345-6789-ABCD-EF0123456789\nPrimaryGroupID: 437\n":     true,
		"RealName:\n PangeaVPN split tunnelling\nPrimaryGroupID: 437\n":                 false,
		"dsAttrTypeNative:GroupMembership: x\nPrimaryGroupID: 437\n":                    false,
		"PrimaryGroupID: 437\r\nGroupMembership: carol\r\nRecordName: _pangeasplit\r\n": true,
	}
	for out, want := range cases {
		if got := dsclGroupHasMembers(out); got != want {
			t.Errorf("dsclGroupHasMembers(%q) = %v, want %v", out, got, want)
		}
	}
}

func TestDsclPrimaryGIDUsers(t *testing.T) {
	out := "_www                    70\nalice                   20\nmallory                 437\n_pangea                 4370\n"
	if got := dsclPrimaryGIDUsers(out, 437); !slices.Equal(got, []string{"mallory"}) {
		t.Fatalf("users = %v, want [mallory]", got)
	}
	if got := dsclPrimaryGIDUsers(out, 438); len(got) != 0 {
		t.Fatalf("users = %v, want none", got)
	}
}

// KS-R2-5: a directory user whose primary gid is the group's carries it on every
// process, and the reverse lookup only ever sees the local group first.
func TestSplitEgressDirectoryCheck(t *testing.T) {
	local := "_pangeasplit\t\tPrimaryGroupID = (\n    437\n)\n"
	if err := splitEgressDirectoryCheck(437, "", local); err != nil {
		t.Fatalf("only the memberless local group holds the gid, yet: %v", err)
	}
	if err := splitEgressDirectoryCheck(437, "", ""); err != nil {
		t.Fatalf("an unreachable directory refused egress: %v", err)
	}
	refused := map[string][2]string{
		"directory user":                   {"mallory\t\tPrimaryGroupID = (\n    437\n)\n", local},
		"one-line directory user":          {"mallory  PrimaryGroupID = (437)\n", local},
		"directory group sharing the gid":  {"", local + "staff-ldap\t\tPrimaryGroupID = (\n    437\n)\n"},
		"directory group of the same name": {"", local + local},
	}
	for name, out := range refused {
		if err := splitEgressDirectoryCheck(437, out[0], out[1]); err == nil {
			t.Errorf("%s: egress allowed", name)
		}
	}
}

func writeProcFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const procNetRouteHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

func TestStrictReversePathFilterAt(t *testing.T) {
	routes := procNetRouteHeader +
		"wlan0\t00000000\t0101A8C0\t0003\t0\t0\t600\t00000000\t0\t0\t0\n" +
		"eth0\t00000000\t0100A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"eth0\t0000A8C0\t00000000\t0001\t0\t0\t100\t00FFFFFF\t0\t0\t0\n"
	cases := []struct {
		name      string
		all, eth0 string
		want      bool
	}{
		{"all strict", "1", "0", true},
		{"interface strict", "0", "1", true},
		{"loose wins over strict", "2", "1", false},
		{"off", "0", "0", false},
		{"loose", "2", "2", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeProcFile(t, root, "net/route", routes)
			writeProcFile(t, root, "sys/net/ipv4/conf/all/rp_filter", tc.all+"\n")
			writeProcFile(t, root, "sys/net/ipv4/conf/eth0/rp_filter", tc.eth0+"\n")
			writeProcFile(t, root, "sys/net/ipv4/conf/wlan0/rp_filter", "1\n")
			got, err := strictReversePathFilterAt(root)
			if err != nil {
				t.Fatalf("strictReversePathFilterAt: %v", err)
			}
			if got != tc.want {
				t.Fatalf("strict = %v, want %v (all=%s eth0=%s; wlan0 is not the cheapest default)", got, tc.want, tc.all, tc.eth0)
			}
		})
	}
}

func TestStrictReversePathFilterAt_WithoutADefaultRouteUsesAll(t *testing.T) {
	root := t.TempDir()
	writeProcFile(t, root, "net/route", procNetRouteHeader)
	writeProcFile(t, root, "sys/net/ipv4/conf/all/rp_filter", "1\n")
	if got, err := strictReversePathFilterAt(root); err != nil || !got {
		t.Fatalf("strict = %v, %v; want true from conf/all alone", got, err)
	}
}

func TestStrictReversePathFilterAt_UnreadableIsAnError(t *testing.T) {
	if _, err := strictReversePathFilterAt(t.TempDir()); err == nil {
		t.Fatal("a missing conf/all/rp_filter was reported as a clean answer")
	}
	root := t.TempDir()
	writeProcFile(t, root, "sys/net/ipv4/conf/all/rp_filter", "strict\n")
	if _, err := strictReversePathFilterAt(root); err == nil {
		t.Fatal("an unparsable rp_filter was reported as a clean answer")
	}
}

func TestDefaultRouteInterface(t *testing.T) {
	routes := procNetRouteHeader +
		"down0\t00000000\t0101A8C0\t0002\t0\t0\t1\t00000000\t0\t0\t0\n" +
		"../x\t00000000\t0101A8C0\t0003\t0\t0\t2\t00000000\t0\t0\t0\n" +
		"wg0\t00000080\t00000000\t0001\t0\t0\t0\t00000080\t0\t0\t0\n" +
		"enp3s0\t00000000\t0100A8C0\t0003\t0\t0\t100\t00000000\t0\t0\t0\n" +
		"short line\n"
	if got := defaultRouteInterface(routes); got != "enp3s0" {
		t.Fatalf("default route interface = %q, want enp3s0", got)
	}
	if got := defaultRouteInterface(procNetRouteHeader); got != "" {
		t.Fatalf("no default route gave %q", got)
	}
	if strings.Contains(defaultRouteInterface(routes), "/") {
		t.Fatal("an interface name with a path separator was returned")
	}
}

func TestNoopKillSwitchPermitsSplitTunnel(t *testing.T) {
	ks := &noopKillSwitch{}
	if err := ks.SetSplitEgress(t.Context(), true); err != nil {
		t.Fatalf("SetSplitEgress: %v", err)
	}
	if err := ks.SetSplitCIDRs(t.Context(), []string{"10.1.0.0/16"}); err != nil {
		t.Fatalf("SetSplitCIDRs: %v", err)
	}
}
