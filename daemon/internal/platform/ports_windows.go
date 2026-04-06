//go:build windows

package platform

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

func KillUDPPortOwners(ctx context.Context, port int, excludePIDs []int) ([]int, error) {
	pids, err := UDPPortOwners(ctx, port, excludePIDs)
	if err != nil {
		return nil, err
	}

	killed := make([]int, 0, len(pids))
	failures := make([]string, 0)
	for _, pid := range pids {
		killOut, killErr := runHiddenProcessCommand(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/F", "/T")
		if killErr != nil {
			lower := strings.ToLower(killOut)
			if strings.Contains(lower, "not found") || strings.Contains(lower, "no running instance") {
				continue
			}
			failures = append(failures, fmt.Sprintf("pid %d: %v (%s)", pid, killErr, strings.TrimSpace(killOut)))
			continue
		}
		killed = append(killed, pid)
	}

	if len(failures) > 0 {
		return killed, fmt.Errorf("%s", strings.Join(failures, "; "))
	}
	return killed, nil
}

func UDPPortOwners(ctx context.Context, port int, excludePIDs []int) ([]int, error) {
	if port <= 0 {
		return nil, fmt.Errorf("invalid udp port: %d", port)
	}

	output, err := runHiddenProcessCommand(ctx, "netstat", "-ano", "-p", "udp")
	if err != nil {
		return nil, fmt.Errorf("query udp listeners: %w (%s)", err, strings.TrimSpace(output))
	}

	exclude := map[int]struct{}{}
	for _, pid := range excludePIDs {
		exclude[pid] = struct{}{}
	}

	pids := map[int]struct{}{}
	for _, line := range strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), "\n") {
		fields := strings.Fields(strings.TrimSpace(line))
		if len(fields) < 4 {
			continue
		}
		if !strings.EqualFold(fields[0], "UDP") {
			continue
		}
		if !addressMatchesPort(fields[1], port) {
			continue
		}

		pid, convErr := strconv.Atoi(fields[len(fields)-1])
		if convErr != nil || pid <= 4 {
			continue
		}
		if _, skip := exclude[pid]; skip {
			continue
		}
		pids[pid] = struct{}{}
	}

	owners := make([]int, 0, len(pids))
	for pid := range pids {
		owners = append(owners, pid)
	}
	sort.Ints(owners)
	return owners, nil
}

func addressMatchesPort(address string, port int) bool {
	needle := strconv.Itoa(port)
	lastColon := strings.LastIndex(address, ":")
	if lastColon < 0 {
		return false
	}
	candidate := strings.TrimSuffix(address[lastColon+1:], "]")
	return candidate == needle
}

func runHiddenProcessCommand(ctx context.Context, command string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, command, args...)
	ConfigureBackgroundProcess(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	message := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))
	return message, err
}
