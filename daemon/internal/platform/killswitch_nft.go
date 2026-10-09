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
func buildNFTRuleset(r ksRules) string {
	var b strings.Builder
	cidrs := renderableSplitCIDRs(r.Split.CIDRs)

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
	for _, ip := range r.EndpointIPs {
		if strings.Contains(ip, ":") {
			continue
		}
		fmt.Fprintf(&b, "    ip daddr %s accept\n", ip)
	}

	// Allow IPv4 traffic on tunnel interface.
	if r.Tunnel != "" {
		fmt.Fprintf(&b, "    meta nfproto ipv4 oifname \"%s\" accept\n", r.Tunnel)
	}

	if r.AllowLAN || len(cidrs) > 0 || r.Split.Egress {
		writeNFTResolverDrops(&b)
	}
	if r.Split.Egress {
		fmt.Fprintf(&b, "    meta nfproto ipv4 meta mark %s accept\n", splitEgressMark)
	}

	// Allow LAN ranges so captive portals and gateway probes work on
	// restrictive WiFi. Only applied when the user opts in.
	if r.AllowLAN {
		for _, cidr := range LANAllowPrefixes {
			fmt.Fprintf(&b, "    ip daddr %s accept\n", cidr)
		}
	}
	for _, cidr := range cidrs {
		fmt.Fprintf(&b, "    ip daddr %s accept\n", cidr)
	}

	fmt.Fprintf(&b, "  }\n")
	writeNFTForwardChain(&b, r.Tunnel, r.AllowLAN, cidrs)
	fmt.Fprintf(&b, "}\n")

	return b.String()
}

// writeNFTResolverDrops keeps lookups behind the tunnel once anything else may
// leave. Placed after the tunnel accept, so only an off-tunnel resolver is caught.
func writeNFTResolverDrops(b *strings.Builder) {
	fmt.Fprintf(b, "    udp dport { 53, 853 } drop\n")
	fmt.Fprintf(b, "    tcp dport { 53, 853 } drop\n")
}

// writeNFTForwardChain covers what the output hook never sees: packets the
// host routes for containers and VMs. Same policy as the host's own traffic.
func writeNFTForwardChain(b *strings.Builder, tunnelInterface string, allowLAN bool, cidrs []string) {
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

	if allowLAN || len(cidrs) > 0 {
		writeNFTResolverDrops(b)
	}
	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			fmt.Fprintf(b, "    ip daddr %s accept\n", cidr)
		}
	}
	// Routed guests' replies come back by source: oifkind only covers bridged ones.
	// Reply direction only, or a guest inside the range could reach anywhere.
	for _, cidr := range cidrs {
		fmt.Fprintf(b, "    ip daddr %s accept\n", cidr)
		fmt.Fprintf(b, "    ip saddr %s ct direction reply accept\n", cidr)
	}

	fmt.Fprintf(b, "  }\n")
}
