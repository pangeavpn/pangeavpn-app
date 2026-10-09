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

// LANExcludePrefixesV6 is the IPv6 counterpart of LANExcludePrefixes.
func LANExcludePrefixesV6() []netip.Prefix {
	out := make([]netip.Prefix, len(lanExcludeRangesV6))
	copy(out, lanExcludeRangesV6)
	return out
}

// CountExcludeRoutes is how many IPv4 routes the tunnel needs once excludes are carved out of
// 0.0.0.0/0, taking the worse of Allow LAN on (LAN carved too) and off (private ranges split).
func CountExcludeRoutes(excludes []netip.Prefix) int {
	all := []netip.Prefix{netip.PrefixFrom(netip.IPv4Unspecified(), 0)}
	masked := maskedPrefixes(excludes)
	withLAN := len(subtractRanges(all, append(LANExcludePrefixes(), masked...)))
	return max(withLAN, len(subtractRanges(all, masked)))
}

func maskedPrefixes(prefixes []netip.Prefix) []netip.Prefix {
	out := make([]netip.Prefix, 0, len(prefixes))
	for _, p := range prefixes {
		if p.IsValid() {
			out = append(out, p.Masked())
		}
	}
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
			if !p.Overlaps(ex) {
				next = append(next, p)
				continue
			}
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

// collectTunnelPrefixes finds the Interface Address/DNS entries, which must
// stay routed into the tunnel regardless of the LAN exclusion set.
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
		rawKey, rawValue, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		key := strings.TrimSpace(rawKey)
		value := strings.TrimSpace(rawValue)
		if !strings.EqualFold(key, "Address") && !strings.EqualFold(key, "DNS") {
			continue
		}
		for part := range strings.SplitSeq(value, ",") {
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

// TransformWGConfigExcludeLAN subtracts the LAN exclusion set from each
// [Peer] AllowedIPs line, keeping the tunnel's own address/DNS routed.
func TransformWGConfigExcludeLAN(configText string) (string, error) {
	return TransformWGConfigExclude(configText, lanExcludeRanges, lanExcludeRangesV6, nil)
}

// TransformWGConfigExclude subtracts v4 from each [Peer] AllowedIPs line's IPv4 entries and v6 from
// its IPv6 ones, then re-includes keep and the config's own Address/DNS so they stay in the tunnel.
func TransformWGConfigExclude(configText string, v4, v6 []netip.Prefix, keep []netip.Prefix) (string, error) {
	if len(v4) == 0 && len(v6) == 0 {
		return configText, nil
	}
	excludes4 := maskedPrefixes(v4)
	excludes6 := maskedPrefixes(v6)
	var keep4, keep6 []netip.Prefix
	for _, k := range maskedPrefixes(append(collectTunnelPrefixes(configText), keep...)) {
		if k.Addr().Is4() {
			keep4 = append(keep4, k)
		} else {
			keep6 = append(keep6, k)
		}
	}

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

		rawKey, rawValue, ok := strings.Cut(rawLine, "=")
		if !ok {
			out = append(out, rawLine)
			continue
		}
		key := strings.TrimSpace(rawKey)
		if !strings.EqualFold(key, "AllowedIPs") {
			out = append(out, rawLine)
			continue
		}

		value := rawValue
		comment := ""
		for i, r := range value {
			if r == '#' || r == ';' {
				comment = value[i:]
				value = value[:i]
				break
			}
		}

		var v4Inputs, v6Passthrough []netip.Prefix
		var v4Raw, v6Raw []string
		for part := range strings.SplitSeq(value, ",") {
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
				v4Raw = append(v4Raw, p)
			} else {
				v6Passthrough = append(v6Passthrough, prefix.Masked())
				v6Raw = append(v6Raw, p)
			}
		}

		var parts []string
		switch {
		case len(v4Inputs) == 0:
		case len(excludes4) == 0:
			parts = append(parts, v4Raw...)
		default:
			filtered := reinclude(subtractRanges(v4Inputs, excludes4), keep4)
			if len(filtered) == 0 {
				// Entirely private peer (e.g. site-to-site): leave it unchanged.
				filtered = v4Inputs
			}
			for _, p := range filtered {
				parts = append(parts, p.String())
			}
		}
		switch {
		case len(v6Passthrough) == 0:
		case len(excludes6) == 0:
			parts = append(parts, v6Raw...)
		default:
			v6Filtered := subtractRanges(v6Passthrough, excludes6)
			if len(v6Filtered) == 0 {
				parts = append(parts, v6Raw...)
			} else {
				for _, p := range reinclude(v6Filtered, keep6) {
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

// CountAllowedIPv4 reports how many IPv4 entries the config's [Peer] AllowedIPs lines carry.
func CountAllowedIPv4(configText string) (int, error) {
	scanner := bufio.NewScanner(strings.NewReader(configText))
	scanner.Buffer(make([]byte, 0, 1024), 1024*1024)
	count := 0
	section := ""
	for scanner.Scan() {
		trimmed := strings.TrimSpace(scanner.Text())
		if header, ok := sectionHeader(trimmed); ok {
			section = header
			continue
		}
		if section != "peer" {
			continue
		}
		key, value, ok := parseKeyValue(trimmed)
		if !ok || !strings.EqualFold(key, "AllowedIPs") {
			continue
		}
		for _, part := range splitCSV(value) {
			prefix, err := parseAllowedIPEntry(part)
			if err != nil {
				return 0, fmt.Errorf("invalid AllowedIPs entry %q: %w", part, err)
			}
			if prefix.Addr().Is4() {
				count++
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return 0, fmt.Errorf("parse wg config for allowed-ips: %w", err)
	}
	return count, nil
}
