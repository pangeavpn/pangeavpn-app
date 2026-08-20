//go:build windows

package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
)

// ErrUDPPortOwnersUnsupported mirrors ports_other.go's sentinel so
// cross-platform callers can reference it unconditionally with errors.Is; the
// Windows implementations below never return it, since the lookup here is
// actually supported.
var ErrUDPPortOwnersUnsupported = errors.New("udp port owner lookup unsupported on this platform")

// taskkillProcessNotFoundExitCode is what taskkill.exe returns when the PID
// is already gone; locale-independent, unlike its stdout/stderr text.
const taskkillProcessNotFoundExitCode = 128

// protectedSystemImages are core OS processes that must never be force-killed,
// even if a stale netstat snapshot momentarily maps a port to their PID.
var protectedSystemImages = map[string]struct{}{
	"system":              {},
	"system idle process": {},
	"smss.exe":            {},
	"csrss.exe":           {},
	"wininit.exe":         {},
	"winlogon.exe":        {},
	"services.exe":        {},
	"lsass.exe":           {},
	"svchost.exe":         {},
}

func KillUDPPortOwners(ctx context.Context, port int, excludePIDs []int) ([]int, error) {
	pids, err := UDPPortOwners(ctx, port, excludePIDs)
	if err != nil {
		return nil, err
	}

	killed := make([]int, 0, len(pids))
	failures := make([]string, 0)
	for _, pid := range pids {
		// Re-check ownership right before killing: the pid list above is a
		// snapshot, and Windows may have recycled the PID in the interim.
		stillOwns, verifyErr := pidOwnsUDPPort(ctx, pid, port)
		if verifyErr != nil {
			failures = append(failures, fmt.Sprintf("pid %d: verify failed: %v", pid, verifyErr))
			continue
		}
		if !stillOwns {
			continue
		}

		image, imageErr := processImageName(ctx, pid)
		if imageErr != nil {
			failures = append(failures, fmt.Sprintf("pid %d: resolve image failed: %v", pid, imageErr))
			continue
		}
		if _, protected := protectedSystemImages[strings.ToLower(image)]; protected {
			failures = append(failures, fmt.Sprintf("pid %d: refused to kill protected system image %q", pid, image))
			continue
		}

		killOut, exitCode, killErr := runHiddenProcessCommand(ctx, "taskkill", "/PID", strconv.Itoa(pid), "/F", "/T")
		if killErr != nil {
			if exitCode == taskkillProcessNotFoundExitCode {
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
	owners, err := queryUDPPortOwners(ctx, port)
	if err != nil {
		return nil, err
	}

	exclude := map[int]struct{}{}
	for _, pid := range excludePIDs {
		exclude[pid] = struct{}{}
	}

	filtered := make([]int, 0, len(owners))
	for pid := range owners {
		if _, skip := exclude[pid]; skip {
			continue
		}
		filtered = append(filtered, pid)
	}
	sort.Ints(filtered)
	return filtered, nil
}

// pidOwnsUDPPort re-queries live netstat state for a single pid/port pair,
// used as a just-in-time check immediately before a privileged kill.
func pidOwnsUDPPort(ctx context.Context, pid int, port int) (bool, error) {
	owners, err := queryUDPPortOwners(ctx, port)
	if err != nil {
		return false, err
	}
	_, owns := owners[pid]
	return owns, nil
}

func queryUDPPortOwners(ctx context.Context, port int) (map[int]struct{}, error) {
	if port <= 0 {
		return nil, fmt.Errorf("invalid udp port: %d", port)
	}

	output, _, err := runHiddenProcessCommand(ctx, "netstat", "-ano", "-p", "udp")
	if err != nil {
		return nil, fmt.Errorf("query udp listeners: %w (%s)", err, strings.TrimSpace(output))
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
		pids[pid] = struct{}{}
	}

	return pids, nil
}

// processImageName resolves the current image name for pid, used to sanity
// check a taskkill target immediately before it runs.
func processImageName(ctx context.Context, pid int) (string, error) {
	output, _, err := runHiddenProcessCommand(ctx, "tasklist", "/FI", fmt.Sprintf("PID eq %d", pid), "/FO", "CSV", "/NH")
	if err != nil {
		return "", fmt.Errorf("query process image failed: %w (%s)", err, strings.TrimSpace(output))
	}

	firstLine := strings.TrimSpace(strings.SplitN(output, "\n", 2)[0])
	fields := strings.Split(firstLine, "\",\"")
	if len(fields) == 0 {
		return "", fmt.Errorf("no process found for pid %d", pid)
	}

	name := strings.Trim(fields[0], "\"")
	if name == "" {
		return "", fmt.Errorf("no process found for pid %d", pid)
	}
	return name, nil
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

func runHiddenProcessCommand(ctx context.Context, command string, args ...string) (string, int, error) {
	cmd := exec.CommandContext(ctx, resolveSystemCommand(command), args...)
	ConfigureBackgroundProcess(cmd)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	message := strings.TrimSpace(strings.Join([]string{stdout.String(), stderr.String()}, "\n"))

	exitCode := 0
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	} else if err != nil {
		exitCode = -1
	}
	return message, exitCode, err
}
