package platform

import (
	"fmt"
	"net"
	"net/netip"
	"regexp"
	"strings"
)

var tunnelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// pfSplitAnchor is the child anchor holding the split rules in kernel memory only:
// the boot job reloads the anchor file, which must never re-arm them.
const pfSplitAnchor = "split"

// Untagged so the ruleset text is testable off-macOS; applying it lives in
// killswitch_darwin.go.

// buildPFRules generates the PF ruleset for the kill-switch anchor file; the split
// rules go to the child anchor it hooks (pfSplitRules), never into the file.
func buildPFRules(r ksRules) (string, error) {
	tunnelInterface := r.Tunnel
	if tunnelInterface != "" && !tunnelNamePattern.MatchString(tunnelInterface) {
		return "", fmt.Errorf("invalid tunnel interface name %q", tunnelInterface)
	}

	var rules []string

	// Loopback must be stateless: macOS pf drops stateful loopback TCP
	// (unchecksummed TSO segments), which cut the app off from the daemon.
	rules = append(rules, "pass out quick on lo0 all no state")
	rules = append(rules, "pass in quick on lo0 all no state")

	// Allow traffic to VPN transport endpoint IPs, v4 and v6 alike.
	for _, ip := range r.EndpointIPs {
		if strings.Contains(ip, ":") {
			rules = append(rules, fmt.Sprintf("pass out quick inet6 proto { tcp udp } to %s", ip))
			continue
		}
		rules = append(rules, fmt.Sprintf("pass out quick inet proto { tcp udp } to %s", ip))
	}

	// DHCP: requests go to broadcast only; the reply comes back from the server.
	rules = append(rules, "pass out quick inet proto udp from any port 68 to 255.255.255.255 port 67")
	rules = append(rules, "pass in quick inet proto udp from any port 67 to any port 68")

	// The tunnel pass precedes every LAN rule so a tunnel-side resolver is not
	// caught by the LAN resolver blocks below.
	if tunnelInterface != "" {
		rules = append(rules, fmt.Sprintf("pass out quick on %s all", tunnelInterface))
	}

	if r.AllowLAN {
		rules = append(rules, pfAllowLANRules()...)
	}
	rules = append(rules, fmt.Sprintf("anchor %q", pfSplitAnchor))

	rules = append(rules, "block out all")
	rules = append(rules, "block in all")

	return strings.Join(rules, "\n") + "\n", nil
}

// pfAllowLANRules opens the LAN both ways for captive portals and local devices,
// except to resolvers: a LAN router on 53/853 would carry every lookup outside.
func pfAllowLANRules() []string {
	var rules []string
	for _, cidr := range LANAllowPrefixes {
		rules = append(rules, fmt.Sprintf("block out quick inet proto { tcp udp } to %s port { 53 853 }", cidr))
	}
	for _, cidr := range LANAllowPrefixesV6 {
		rules = append(rules, fmt.Sprintf("block out quick inet6 proto { tcp udp } to %s port { 53 853 }", cidr))
	}
	for _, cidr := range LANAllowPrefixes {
		rules = append(rules, fmt.Sprintf("pass out quick inet to %s", cidr))
		if pfUnicastSource(cidr) {
			rules = append(rules, fmt.Sprintf("pass in quick inet from %s", cidr))
		}
	}
	for _, cidr := range LANAllowPrefixesV6 {
		rules = append(rules, fmt.Sprintf("pass out quick inet6 to %s", cidr))
		if pfUnicastSource(cidr) {
			rules = append(rules, fmt.Sprintf("pass in quick inet6 from %s", cidr))
		}
	}
	return rules
}

// pfKillArgs ends flows between each local address and each range, both ways. A
// lone `-k <cidr>` would also end every outbound state whose local address is in it.
func pfKillArgs(locals []netip.Addr, cidrs []string) [][]string {
	var out [][]string
	for _, cidr := range cidrs {
		for _, local := range locals {
			host := netip.PrefixFrom(local, local.BitLen()).String()
			out = append(out, []string{"-k", host, "-k", cidr}, []string{"-k", cidr, "-k", host})
		}
	}
	return out
}

// pfKillLocalAddrs lists the host's IPv4 addresses off the tunnel: flows from the
// tunnel address stay in the tunnel, which the lock still passes.
func pfKillLocalAddrs(tunnel string) ([]netip.Addr, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []netip.Addr
	for _, iface := range ifaces {
		if iface.Name == tunnel || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			n, ok := addr.(*net.IPNet)
			if !ok {
				continue
			}
			if a, ok := netip.AddrFromSlice(n.IP.To4()); ok && !a.IsLoopback() {
				out = append(out, a)
			}
		}
	}
	return out, nil
}

// pfUnicastSource: multicast and broadcast never appear as a source address.
func pfUnicastSource(cidr string) bool {
	return cidr != "224.0.0.0/4" && cidr != "255.255.255.255/32" && cidr != "ff02::/16"
}

// pfSplitRules lets excluded ranges and the egress group's sockets out, except to
// resolvers, which stay behind the tunnel like the LAN's.
func pfSplitRules(split splitPermits, egressGID int) []string {
	cidrs := renderableSplitCIDRs(split.CIDRs)
	var rules []string
	for _, cidr := range cidrs {
		rules = append(rules, fmt.Sprintf("block out quick inet proto { tcp udp } to %s port { 53 853 }", cidr))
	}
	for _, cidr := range cidrs {
		rules = append(rules, fmt.Sprintf("pass out quick inet to %s", cidr))
		rules = append(rules, fmt.Sprintf("pass in quick inet from %s", cidr))
	}
	if split.Egress && egressGID > 0 {
		rules = append(rules, fmt.Sprintf("block out quick proto { tcp udp } to any port { 53 853 } group %d", egressGID))
		rules = append(rules, fmt.Sprintf("pass out quick inet proto { tcp udp } from any to any group %d", egressGID))
	}
	return rules
}
