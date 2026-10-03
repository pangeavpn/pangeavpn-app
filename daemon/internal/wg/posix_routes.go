//go:build darwin || linux

package wg

import (
	"fmt"
	"net"
)

// normalizedRoutesForPrefix splits 0.0.0.0/0 into two /1 prefixes (and
// similarly for ::/0) to avoid replacing the default route.
func normalizedRoutesForPrefix(prefix string) ([]string, string, error) {
	ip, network, err := net.ParseCIDR(prefix)
	if err != nil {
		return nil, "", fmt.Errorf("invalid allowed ip %s: %w", prefix, err)
	}

	ones, bits := network.Mask.Size()
	if bits == 32 {
		if ones == 0 {
			return []string{"0.0.0.0/1", "128.0.0.0/1"}, "inet", nil
		}
		return []string{network.String()}, "inet", nil
	}
	if bits == 128 {
		if ones == 0 {
			return []string{"::/1", "8000::/1"}, "inet6", nil
		}
		return []string{network.String()}, "inet6", nil
	}

	if ip.To4() != nil {
		return []string{network.String()}, "inet", nil
	}
	return []string{network.String()}, "inet6", nil
}

// expandRoutePrefixes lists the routes AllowedIPs install; an unparseable entry is kept so adding it still fails.
func expandRoutePrefixes(prefixes []string) []string {
	var out []string
	for _, prefix := range prefixes {
		routes, _, err := normalizedRoutesForPrefix(prefix)
		if err != nil {
			out = append(out, prefix)
			continue
		}
		out = append(out, routes...)
	}
	return out
}
