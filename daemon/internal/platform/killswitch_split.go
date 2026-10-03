package platform

import (
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strconv"
	"strings"
)

// SplitEgressGroupName is the macOS group whose sockets pf lets leave off-tunnel.
// The installer creates it with a free gid and no members.
const SplitEgressGroupName = "_pangeasplit"

// splitEgressMark is egress.SplitMark, the SO_MARK on Linux bypass sockets.
const splitEgressMark = "0x1ca6c"

// minSplitCIDRBits matches the store's floor: a wider range would leave the lock
// little more than a DNS block.
const minSplitCIDRBits = 8

var _ SplitTunnelPermitter = (*noopKillSwitch)(nil)

// splitPermits is what split tunnelling needs through the lock. Kept in memory
// only, so a fresh process never re-arms permits a dead session asked for.
type splitPermits struct {
	Egress bool
	CIDRs  []string
}

func (s splitPermits) empty() bool { return !s.Egress && len(s.CIDRs) == 0 }

func (s splitPermits) equal(o splitPermits) bool {
	return s.Egress == o.Egress && slices.Equal(s.CIDRs, o.CIDRs)
}

func (s splitPermits) clone() splitPermits {
	return splitPermits{Egress: s.Egress, CIDRs: slices.Clone(s.CIDRs)}
}

// narrowedSplit is what stays desired after a failed change: what is both enforced
// and still wanted, so removals are retried and failed additions wait to be asked again.
func narrowedSplit(applied, next splitPermits) splitPermits {
	out := splitPermits{Egress: applied.Egress && next.Egress}
	for _, cidr := range next.CIDRs {
		if slices.Contains(applied.CIDRs, cidr) {
			out.CIDRs = append(out.CIDRs, cidr)
		}
	}
	return out
}

// ksRules is every input a pf/nft/iptables render takes, so no render path can
// leave the split permits out.
type ksRules struct {
	EndpointIPs []string
	Tunnel      string
	AllowLAN    bool
	Split       splitPermits
}

func parseSplitCIDR(cidr string) (netip.Prefix, error) {
	p, err := netip.ParsePrefix(cidr)
	if err != nil {
		return netip.Prefix{}, err
	}
	if !p.Addr().Is4() {
		return netip.Prefix{}, errors.New("not IPv4")
	}
	if p.Masked() != p {
		return netip.Prefix{}, errors.New("host bits set")
	}
	if p.Bits() < minSplitCIDRBits {
		return netip.Prefix{}, fmt.Errorf("shorter than /%d", minSplitCIDRBits)
	}
	return p, nil
}

// normalizeSplitCIDRs refuses the whole set on one bad entry: the caller routes
// exactly what it asked for, so a silently dropped range would be blackholed.
func normalizeSplitCIDRs(cidrs []string) ([]string, error) {
	out := make([]string, 0, len(cidrs))
	for i, cidr := range cidrs {
		p, err := parseSplitCIDR(strings.TrimSpace(cidr))
		if err != nil {
			return nil, fmt.Errorf("split CIDR %d %q: %w", i, cidr, err)
		}
		out = append(out, p.String())
	}
	slices.Sort(out)
	return slices.Compact(out), nil
}

// renderableSplitCIDRs re-checks every CIDR right before it is written into rule
// text, whatever path it came by.
func renderableSplitCIDRs(cidrs []string) []string {
	out := make([]string, 0, len(cidrs))
	for _, cidr := range cidrs {
		p, err := parseSplitCIDR(cidr)
		if err != nil || p.String() != cidr {
			KillSwitchWarn("kill switch: skipping invalid split-tunnel CIDR %q", cidr)
			continue
		}
		out = append(out, cidr)
	}
	return out
}

func validSplitCIDR(cidr string) bool {
	p, err := parseSplitCIDR(cidr)
	return err == nil && p.String() == cidr
}

// splitRangesToKill lists the parts of prev that r permits in no form: another
// range, Allow LAN or an endpoint still covering a part keeps its flows alive.
func splitRangesToKill(prev []string, r ksRules) []string {
	var permitted []netip.Prefix
	for _, cidr := range renderableSplitCIDRs(r.Split.CIDRs) {
		permitted = append(permitted, netip.MustParsePrefix(cidr))
	}
	if r.AllowLAN {
		for _, cidr := range LANAllowPrefixes {
			permitted = append(permitted, netip.MustParsePrefix(cidr))
		}
	}
	for _, ip := range r.EndpointIPs {
		if addr, err := netip.ParseAddr(ip); err == nil && addr.Is4() {
			permitted = append(permitted, netip.PrefixFrom(addr, 32))
		}
	}
	var out []string
	for _, cidr := range prev {
		if !validSplitCIDR(cidr) {
			continue
		}
		for _, left := range subtractPrefixes(netip.MustParsePrefix(cidr), permitted) {
			out = append(out, left.String())
		}
	}
	return out
}

// subtractPrefixes returns the IPv4 prefixes covering p minus every prefix in minus.
func subtractPrefixes(p netip.Prefix, minus []netip.Prefix) []netip.Prefix {
	split := false
	for _, q := range minus {
		if q.Bits() <= p.Bits() && q.Contains(p.Addr()) {
			return nil
		}
		split = split || (q.Bits() > p.Bits() && p.Contains(q.Addr()))
	}
	if !split {
		return []netip.Prefix{p}
	}
	upper := p.Addr().As4()
	upper[p.Bits()/8] |= 0x80 >> (p.Bits() % 8)
	return append(
		subtractPrefixes(netip.PrefixFrom(p.Addr(), p.Bits()+1), minus),
		subtractPrefixes(netip.PrefixFrom(netip.AddrFrom4(upper), p.Bits()+1), minus)...)
}

// Below this sit macOS's own shared groups (wheel 0, staff 20, admin 80).
const minSplitEgressGID = 100

func splitEgressGIDFromString(s string) (int, error) {
	gid, err := strconv.ParseUint(strings.TrimSpace(s), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("group %s has an invalid gid %q", SplitEgressGroupName, s)
	}
	if gid < minSplitEgressGID {
		return 0, fmt.Errorf("group %s has gid %d, below %d; refusing a shared system group", SplitEgressGroupName, gid, minSplitEgressGID)
	}
	return int(gid), nil
}

// dsclGroupHasMembers reads `dscl . -read /Groups/<name>` output; any member
// could newgrp into the gid and send through the pf pass.
func dsclGroupHasMembers(out string) bool {
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok || (key != "GroupMembership" && key != "GroupMembers") {
			continue
		}
		if strings.TrimSpace(value) != "" {
			return true
		}
		if i+1 < len(lines) && strings.HasPrefix(lines[i+1], " ") && strings.TrimSpace(lines[i+1]) != "" {
			return true
		}
	}
	return false
}

// dsclSearchRecords reads `dscl /Search -search <path> PrimaryGroupID <gid>`
// output: each matching record starts an unindented "name  PrimaryGroupID = (" line.
func dsclSearchRecords(out string) []string {
	var names []string
	for line := range strings.SplitSeq(out, "\n") {
		name, _, ok := strings.Cut(line, "PrimaryGroupID")
		if ok && line[0] != ' ' && line[0] != '\t' && strings.TrimSpace(name) != "" {
			names = append(names, strings.TrimSpace(name))
		}
	}
	return names
}

// splitEgressDirectoryCheck refuses a gid that the directory search finds on any
// user, or on any group besides the one local _pangeasplit.
func splitEgressDirectoryCheck(gid int, usersOut, groupsOut string) error {
	if users := dsclSearchRecords(usersOut); len(users) > 0 {
		return fmt.Errorf("gid %d of group %s is the primary group of %d users", gid, SplitEgressGroupName, len(users))
	}
	groups := dsclSearchRecords(groupsOut)
	if len(groups) > 1 || (len(groups) == 1 && groups[0] != SplitEgressGroupName) {
		return fmt.Errorf("gid %d of group %s is also held by another directory group", gid, SplitEgressGroupName)
	}
	return nil
}

// dsclPrimaryGIDUsers reads `dscl . -list /Users PrimaryGroupID` output.
func dsclPrimaryGIDUsers(out string, gid int) []string {
	want := strconv.Itoa(gid)
	var users []string
	for line := range strings.SplitSeq(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == want {
			users = append(users, fields[0])
		}
	}
	return users
}
