//go:build darwin || linux || windows

package wg

import (
	"bufio"
	"fmt"
	"net/netip"
	"strings"
)

// lanExcludeRanges are the IPv4 prefixes the "Allow LAN" toggle carves out
// of the tunnel. Covers RFC1918, link-local, multicast, and limited broadcast.
var lanExcludeRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
}

// LANExcludePrefixes returns the standard IPv4 ranges that bypass the tunnel
// when "Allow LAN" is enabled. The caller receives a copy.
func LANExcludePrefixes() []netip.Prefix {
	out := make([]netip.Prefix, len(lanExcludeRanges))
	copy(out, lanExcludeRanges)
	return out
}

// subtractPrefix returns (p \ exclude) as disjoint IPv4 prefixes. Never
// overlaps exclude; preserves the remainder of p.
func subtractPrefix(p, exclude netip.Prefix) []netip.Prefix {
	if !p.Overlaps(exclude) {
		return []netip.Prefix{p}
	}
	if exclude.Bits() <= p.Bits() && exclude.Contains(p.Addr()) {
		return nil
	}
	if p.Bits() >= 32 {
		return []netip.Prefix{p}
	}
	lowerAddr := p.Addr()
	upperAddr := setIPv4Bit(lowerAddr, p.Bits())
	newBits := p.Bits() + 1
	lower := netip.PrefixFrom(lowerAddr, newBits)
	upper := netip.PrefixFrom(upperAddr, newBits)
	return append(subtractPrefix(lower, exclude), subtractPrefix(upper, exclude)...)
}

func setIPv4Bit(addr netip.Addr, pos int) netip.Addr {
	b := addr.As4()
	b[pos/8] |= byte(1 << (7 - (pos % 8)))
	return netip.AddrFrom4(b)
}

// subtractRanges returns (inputs \ excludes) as a flat list of disjoint IPv4 prefixes.
func subtractRanges(inputs, excludes []netip.Prefix) []netip.Prefix {
	result := append([]netip.Prefix(nil), inputs...)
	for _, ex := range excludes {
		next := result[:0:0]
		for _, p := range result {
			next = append(next, subtractPrefix(p, ex)...)
		}
		result = next
	}
	return result
}

// TransformWGConfigExcludeLAN rewrites every `AllowedIPs = ...` line inside
// [Peer] sections, subtracting the LAN exclusion set. Lines outside [Peer]
// are passed through unchanged. Returns an error if any AllowedIPs entry is
// invalid or IPv6, or if subtraction leaves a peer with no allowed IPs.
func TransformWGConfigExcludeLAN(configText string) (string, error) {
	scanner := bufio.NewScanner(strings.NewReader(configText))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	var out []string
	section := ""
	for scanner.Scan() {
		rawLine := scanner.Text()
		trimmed := strings.TrimSpace(rawLine)

		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			section = strings.ToLower(strings.TrimSpace(trimmed[1 : len(trimmed)-1]))
			out = append(out, rawLine)
			continue
		}

		if section != "peer" || trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, ";") {
			out = append(out, rawLine)
			continue
		}

		idx := strings.Index(rawLine, "=")
		if idx < 0 {
			out = append(out, rawLine)
			continue
		}
		key := strings.TrimSpace(rawLine[:idx])
		if !strings.EqualFold(key, "AllowedIPs") {
			out = append(out, rawLine)
			continue
		}

		value := rawLine[idx+1:]
		comment := ""
		for i, r := range value {
			if r == '#' || r == ';' {
				comment = value[i:]
				value = value[:i]
				break
			}
		}

		inputs := make([]netip.Prefix, 0, 4)
		for _, part := range strings.Split(value, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(p)
			if err != nil {
				return "", fmt.Errorf("invalid AllowedIPs entry %q: %w", p, err)
			}
			if !prefix.Addr().Is4() {
				return "", fmt.Errorf("IPv6 AllowedIPs entry is not supported: %s", p)
			}
			inputs = append(inputs, prefix.Masked())
		}

		filtered := subtractRanges(inputs, lanExcludeRanges)
		if len(filtered) == 0 {
			return "", fmt.Errorf("AllowedIPs becomes empty after LAN exclusion on line %q", rawLine)
		}

		parts := make([]string, len(filtered))
		for i, p := range filtered {
			parts[i] = p.String()
		}

		rewritten := "AllowedIPs = " + strings.Join(parts, ", ")
		if comment != "" {
			rewritten += " " + strings.TrimSpace(comment)
		}
		out = append(out, rewritten)
	}

	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("parse wg config for lan-exclude: %w", err)
	}

	return strings.Join(out, "\n") + "\n", nil
}
