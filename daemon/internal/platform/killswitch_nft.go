package platform

import (
	"fmt"
	"strings"
)

const (
	nftTableName = "pangeavpn_killswitch"
	nftFamily    = "inet"
)

// Untagged so the ruleset text is testable off-Linux; applying it lives in
// killswitch_linux.go.

// buildNFTRuleset generates a complete nftables ruleset for the kill switch.
func buildNFTRuleset(endpointIPs []string, tunnelInterface string, allowLAN bool) string {
	var b strings.Builder

	fmt.Fprintf(&b, "table %s %s {\n", nftFamily, nftTableName)
	fmt.Fprintf(&b, "  chain output {\n")
	fmt.Fprintf(&b, "    type filter hook output priority 0; policy drop;\n")
	fmt.Fprintf(&b, "\n")

	// Allow loopback.
	fmt.Fprintf(&b, "    oifname \"lo\" accept\n")

	// Allow DHCP, scoped to broadcast so it can't be used to reach an
	// arbitrary remote host on udp/67, and to IPv4 only.
	fmt.Fprintf(&b, "    meta nfproto ipv4 udp sport 68 udp dport 67 ip daddr 255.255.255.255 accept\n")

	// Allow traffic to endpoint IPs.
	for _, ip := range endpointIPs {
		if strings.Contains(ip, ":") {
			continue
		}
		fmt.Fprintf(&b, "    ip daddr %s accept\n", ip)
	}

	// Allow LAN ranges so captive portals and gateway probes work on
	// restrictive WiFi. Only applied when the user opts in.
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			fmt.Fprintf(&b, "    ip daddr %s accept\n", cidr)
		}
	}

	// Allow IPv4 traffic on tunnel interface.
	if tunnelInterface != "" {
		fmt.Fprintf(&b, "    meta nfproto ipv4 oifname \"%s\" accept\n", tunnelInterface)
	}

	fmt.Fprintf(&b, "  }\n")
	writeNFTForwardChain(&b, tunnelInterface, allowLAN)
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// writeNFTForwardChain covers what the output hook never sees: packets the
// host routes for containers and VMs. Same policy as the host's own traffic.
func writeNFTForwardChain(b *strings.Builder, tunnelInterface string, allowLAN bool) {
	fmt.Fprintf(b, "  chain forward {\n")
	fmt.Fprintf(b, "    type filter hook forward priority 0; policy drop;\n")
	fmt.Fprintf(b, "\n")

	// br_netfilter runs bridged frames through this hook too; a packet leaving
	// via a bridge device never leaves the host.
	fmt.Fprintf(b, "    meta oifkind \"bridge\" accept\n")

	if tunnelInterface != "" {
		fmt.Fprintf(b, "    meta nfproto ipv4 oifname \"%s\" accept\n", tunnelInterface)
		fmt.Fprintf(b, "    meta nfproto ipv4 iifname \"%s\" accept\n", tunnelInterface)
	}

	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			fmt.Fprintf(b, "    ip daddr %s accept\n", cidr)
		}
	}

	fmt.Fprintf(b, "  }\n")
}
