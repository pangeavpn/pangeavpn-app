package platform

import (
	"strconv"
	"strings"
)

// compareVersions orders release versions by semver precedence, the same way
// the app's update policy does: "0.8.2-rc.1" sorts below "0.8.2", and numeric
// pre-release parts compare as numbers (rc.10 > rc.9). A leading "v" and any
// "+build" suffix are ignored.
func compareVersions(a, b string) int {
	ac, ap := splitVersion(a)
	bc, bp := splitVersion(b)
	for i := 0; i < len(ac) || i < len(bc); i++ {
		x, y := partAt(ac, i), partAt(bc, i)
		if x != y {
			return compareInts(x, y)
		}
	}
	if len(ap) == 0 || len(bp) == 0 {
		return compareInts(len(bp), len(ap))
	}
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if c := compareIdentifiers(ap[i], bp[i]); c != 0 {
			return c
		}
	}
	return compareInts(len(ap), len(bp))
}

func splitVersion(v string) ([]int, []string) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	core, pre, _ := strings.Cut(v, "-")
	var parts []int
	for _, p := range strings.Split(core, ".") {
		n, _ := strconv.Atoi(p)
		parts = append(parts, n)
	}
	if pre == "" {
		return parts, nil
	}
	return parts, strings.Split(pre, ".")
}

func partAt(parts []int, i int) int {
	if i < len(parts) {
		return parts[i]
	}
	return 0
}

func compareIdentifiers(a, b string) int {
	an, aErr := strconv.Atoi(a)
	bn, bErr := strconv.Atoi(b)
	switch {
	case aErr == nil && bErr == nil:
		return compareInts(an, bn)
	case aErr == nil:
		return -1
	case bErr == nil:
		return 1
	default:
		return strings.Compare(a, b)
	}
}

func compareInts(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
