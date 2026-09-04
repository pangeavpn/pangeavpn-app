package platform

import (
	"fmt"
	"regexp"
	"strings"
)

var tunnelNamePattern = regexp.MustCompile(`^[a-zA-Z0-9]+$`)

// Untagged so the ruleset text is testable off-macOS; applying it lives in
// killswitch_darwin.go.

// buildPFRules generates the PF ruleset for the kill-switch anchor.
func buildPFRules(endpointIPs []string, tunnelInterface string, allowLAN bool) (string, error) {
	if tunnelInterface != "" && !tunnelNamePattern.MatchString(tunnelInterface) {
		return "", fmt.Errorf("invalid tunnel interface name %q", tunnelInterface)
	}

	var rules []string

	// Loopback must be stateless: macOS pf drops stateful loopback TCP
	// (unchecksummed TSO segments), which cut the app off from the daemon.
	rules = append(rules, "pass out quick on lo0 all no state")
	rules = append(rules, "pass in quick on lo0 all no state")

	// Allow traffic to VPN transport endpoint IPs, v4 and v6 alike.
	for _, ip := range endpointIPs {
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

	if allowLAN {
		rules = append(rules, pfAllowLANRules()...)
	}

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

// pfUnicastSource: multicast and broadcast never appear as a source address.
func pfUnicastSource(cidr string) bool {
	return cidr != "224.0.0.0/4" && cidr != "255.255.255.255/32" && cidr != "ff02::/16"
}
