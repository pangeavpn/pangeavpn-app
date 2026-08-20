//go:build darwin || linux || windows

package wg

import (
	"bufio"
	"fmt"
	"net/netip"
	"strings"
)

// lanExcludeRanges are the IPv4 prefixes the "Allow LAN" toggle carves out
// of the tunnel. Covers RFC1918, CGNAT, link-local, multicast, and limited broadcast.
var lanExcludeRanges = []netip.Prefix{
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
}

// lanExcludeRangesV6 are the IPv6 analogues of lanExcludeRanges: link-local,
// unique local, and multicast.
var lanExcludeRangesV6 = []netip.Prefix{
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("ff00::/8"),
}

// LANExcludePrefixes returns the standard IPv4 ranges that bypass the tunnel
// when "Allow LAN" is enabled. The caller receives a copy.
func LANExcludePrefixes() []netip.Prefix {
	out := make([]netip.Prefix, len(lanExcludeRanges))
	copy(out, lanExcludeRanges)
	return out
}

// subtractPrefix returns (p \ exclude) as disjoint prefixes of p's own
// family. Never overlaps exclude; preserves the remainder of p.
func subtractPrefix(p, exclude netip.Prefix) []netip.Prefix {
	if p.Addr().Is4() != exclude.Addr().Is4() || !p.Overlaps(exclude) {
		return []netip.Prefix{p}
	}
	if exclude.Bits() <= p.Bits() && exclude.Contains(p.Addr()) {
		return nil
	}
	bitLen := 32
	if !p.Addr().Is4() {
		bitLen = 128
	}
	if p.Bits() >= bitLen {
		return nil
	}
	lowerAddr := p.Addr()
	upperAddr := setBit(lowerAddr, p.Bits())
	newBits := p.Bits() + 1
	lower := netip.PrefixFrom(lowerAddr, newBits)
	upper := netip.PrefixFrom(upperAddr, newBits)
	return append(subtractPrefix(lower, exclude), subtractPrefix(upper, exclude)...)
}

// setBit returns addr with bit pos (0-indexed from the MSB) set, working for
// both IPv4 and IPv6 addresses.
func setBit(addr netip.Addr, pos int) netip.Addr {
	b := addr.AsSlice()
	b[pos/8] |= byte(1 << (7 - (pos % 8)))
	out, _ := netip.AddrFromSlice(b)
	return out
}

// subtractRanges returns (inputs \ excludes) as a flat list of disjoint prefixes.
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

// reinclude appends any of the keep prefixes not already covered by result,
// so tunnel-internal addresses (interface address, DNS) survive LAN exclusion.
func reinclude(result []netip.Prefix, keep []netip.Prefix) []netip.Prefix {
	out := append([]netip.Prefix(nil), result...)
	for _, k := range keep {
		covered := false
		for _, r := range out {
			if r.Bits() <= k.Bits() && r.Contains(k.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			out = append(out, k)
		}
	}
	return out
}

// collectTunnelPrefixes scans the raw config for the Interface Address and
// DNS entries, which must stay routed into the tunnel regardless of the LAN
// exclusion set.
func collectTunnelPrefixes(configText string) []netip.Prefix {
	var keep []netip.Prefix
	scanner := bufio.NewScanner(strings.NewReader(configText))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	section := ""
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if header, ok := sectionHeader(trimmed); ok {
			section = header
			continue
		}
		if section != "interface" {
			continue
		}
		idx := strings.Index(trimmed, "=")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		if !strings.EqualFold(key, "Address") && !strings.EqualFold(key, "DNS") {
			continue
		}
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if p, err := netip.ParsePrefix(part); err == nil && p.Addr().Is4() {
				keep = append(keep, netip.PrefixFrom(p.Addr(), 32))
				continue
			}
			if addr, err := netip.ParseAddr(part); err == nil && addr.Is4() {
				keep = append(keep, netip.PrefixFrom(addr, 32))
			}
		}
	}
	return keep
}

// sectionHeader reports whether trimmed is an ini-style section header,
// tolerating a trailing comment, and returns the lowercased section name.
func sectionHeader(trimmed string) (string, bool) {
	if !strings.HasPrefix(trimmed, "[") {
		return "", false
	}
	end := strings.IndexByte(trimmed, ']')
	if end < 0 {
		return "", false
	}
	return strings.ToLower(strings.TrimSpace(trimmed[1:end])), true
}

// TransformWGConfigExcludeLAN rewrites every `AllowedIPs = ...` line inside
// [Peer] sections, subtracting the LAN exclusion set while keeping the
// tunnel's own address and DNS servers routed. Lines outside [Peer] are
// passed through unchanged, as are IPv6 entries. Returns an error only if an
// AllowedIPs entry is invalid.
func TransformWGConfigExcludeLAN(configText string) (string, error) {
	keep := collectTunnelPrefixes(configText)

	scanner := bufio.NewScanner(strings.NewReader(configText))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)

	var out []string
	section := ""
	for scanner.Scan() {
		rawLine := scanner.Text()
		trimmed := strings.TrimSpace(rawLine)

		if header, ok := sectionHeader(trimmed); ok {
			section = header
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

		var v4Inputs, v6Passthrough []netip.Prefix
		var v6Raw []string
		for _, part := range strings.Split(value, ",") {
			p := strings.TrimSpace(part)
			if p == "" {
				continue
			}
			prefix, err := netip.ParsePrefix(p)
			if err != nil {
				return "", fmt.Errorf("invalid AllowedIPs entry %q: %w", p, err)
			}
			if prefix.Addr().Is4() {
				v4Inputs = append(v4Inputs, prefix.Masked())
			} else {
				v6Passthrough = append(v6Passthrough, prefix.Masked())
				v6Raw = append(v6Raw, p)
			}
		}

		var parts []string
		if len(v4Inputs) > 0 {
			filtered := reinclude(subtractRanges(v4Inputs, lanExcludeRanges), keep)
			if len(filtered) == 0 {
				// Entirely private peer (e.g. site-to-site): leave it unchanged.
				for _, p := range v4Inputs {
					parts = append(parts, p.String())
				}
			} else {
				for _, p := range filtered {
					parts = append(parts, p.String())
				}
			}
		}
		if len(v6Passthrough) > 0 {
			v6Filtered := subtractRanges(v6Passthrough, lanExcludeRangesV6)
			if len(v6Filtered) == 0 {
				parts = append(parts, v6Raw...)
			} else {
				for _, p := range v6Filtered {
					parts = append(parts, p.String())
				}
			}
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
