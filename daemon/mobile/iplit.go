package mobile

// IP literal checks. Ports apps/desktop/src/shared/ipLiteral.ts.

import (
	"regexp"
	"strconv"
	"strings"
)

var (
	ipv4Literal = regexp.MustCompile(`^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$`)
	ipv6Group   = regexp.MustCompile(`^[0-9a-fA-F]{1,4}$`)
)

// isIPv4Literal rejects leading-zero octets, since Go's dialer would too.
func isIPv4Literal(host string) bool {
	match := ipv4Literal.FindStringSubmatch(host)
	if match == nil {
		return false
	}
	for _, octet := range match[1:] {
		value, err := strconv.Atoi(octet)
		if err != nil || value > 255 || (len(octet) > 1 && octet[0] == '0') {
			return false
		}
	}
	return true
}

// isIPv6Literal is deliberately permissive: a false positive only means the
// host is dialled verbatim instead of substituting the node.
func isIPv6Literal(host string) bool {
	bare := host
	if strings.HasPrefix(bare, "[") && strings.HasSuffix(bare, "]") {
		bare = bare[1 : len(bare)-1]
	}
	if !strings.Contains(bare, ":") {
		return false
	}
	halves := strings.Split(bare, "::")
	if len(halves) > 2 {
		return false
	}
	var groups []string
	for _, half := range halves {
		if half != "" {
			groups = append(groups, strings.Split(half, ":")...)
		}
	}
	if len(halves) == 1 && len(groups) != 8 {
		return false
	}
	if len(halves) == 2 && len(groups) >= 8 {
		return false
	}
	for _, group := range groups {
		if !ipv6Group.MatchString(group) {
			return false
		}
	}
	return true
}

func isIPLiteral(host string) bool {
	return isIPv4Literal(host) || isIPv6Literal(host)
}
