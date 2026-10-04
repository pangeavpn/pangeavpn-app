package platform

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Untagged so the /proc parsing is testable off-Linux against a fake root.

// strictReversePathFilterAt reports rp_filter=1 in force on the default-route
// interface; the kernel applies the larger of conf/all and the interface's value.
func strictReversePathFilterAt(procRoot string) (bool, error) {
	all, err := readRPFilter(procRoot, "all")
	if err != nil {
		return false, err
	}
	iface := 0
	if data, err := os.ReadFile(filepath.Join(procRoot, "net", "route")); err == nil {
		if name := defaultRouteInterface(string(data)); name != "" {
			if v, err := readRPFilter(procRoot, name); err == nil {
				iface = v
			}
		}
	}
	return max(all, iface) == 1, nil
}

func readRPFilter(procRoot, conf string) (int, error) {
	data, err := os.ReadFile(filepath.Join(procRoot, "sys", "net", "ipv4", "conf", conf, "rp_filter"))
	if err != nil {
		return 0, fmt.Errorf("read rp_filter for %s: %w", conf, err)
	}
	v, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("parse rp_filter for %s: %w", conf, err)
	}
	return v, nil
}

// defaultRouteInterface picks the lowest-metric up default route from
// /proc/net/route, which lists the main table: where bypass replies arrive.
func defaultRouteInterface(procNetRoute string) string {
	const rtfUp = 0x1
	best, bestMetric := "", -1
	for i, line := range strings.Split(procNetRoute, "\n") {
		fields := strings.Fields(line)
		if i == 0 || len(fields) < 8 {
			continue
		}
		name, dest, mask := fields[0], fields[1], fields[7]
		flags, err := strconv.ParseUint(fields[3], 16, 32)
		if err != nil || flags&rtfUp == 0 || dest != "00000000" || mask != "00000000" {
			continue
		}
		metric, err := strconv.Atoi(fields[6])
		if err != nil || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
			continue
		}
		if bestMetric < 0 || metric < bestMetric {
			best, bestMetric = name, metric
		}
	}
	return best
}
