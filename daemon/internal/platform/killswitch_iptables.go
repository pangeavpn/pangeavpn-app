package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// Each family alternates between two chain names so a rebuild can be staged
// under the one that isn't live.
const (
	iptChainName     = "PANGEAVPN_KS"
	iptChainNameAlt  = "PANGEAVPN_KS_B"
	ipt6ChainName    = "PANGEAVPN_KS6"
	ipt6ChainNameAlt = "PANGEAVPN_KS6_B"
)

// Wait for the xtables lock rather than failing; a lock failure is otherwise
// indistinguishable from "rule not present".
const iptWaitSeconds = "5"

// BestEffort commands may fail (clearing leftovers, retiring a chain that may
// not exist); everything else aborts the apply.
type iptablesCommand struct {
	Binary     string
	Args       []string
	BestEffort bool
	Purpose    string
}

func withWait(args []string) []string {
	return append([]string{"-w", iptWaitSeconds}, args...)
}

func ipt4(purpose string, args ...string) iptablesCommand {
	return iptablesCommand{Binary: "iptables", Args: withWait(args), Purpose: purpose}
}

func ipt4Optional(args ...string) iptablesCommand {
	return iptablesCommand{Binary: "iptables", Args: withWait(args), BestEffort: true}
}

func ipt6(purpose string, args ...string) iptablesCommand {
	return iptablesCommand{Binary: "ip6tables", Args: withWait(args), Purpose: purpose}
}

func ipt6Optional(args ...string) iptablesCommand {
	return iptablesCommand{Binary: "ip6tables", Args: withWait(args), BestEffort: true}
}

func iptablesStagingChain(live string) string {
	if live == iptChainName {
		return iptChainNameAlt
	}
	return iptChainName
}

func iptables6StagingChain(live string) string {
	if live == ipt6ChainName {
		return ipt6ChainNameAlt
	}
	return ipt6ChainName
}

// iptablesApplyPlan installs the rules without ever leaving OUTPUT unfiltered.
// iptables has no transaction, so ordering is the safety property: build the
// replacement under an unreferenced name, hook it up only after its terminal
// DROP, then retire the old one. Rebuilding in place would leave the host
// unfiltered for the length of the rebuild.
//
// IPv6 is rebuilt too (a jump says nothing about whether the chain behind it
// still ends in DROP) and runs first, so a v6 failure aborts before the v4
// permits move to the new server.
func iptablesApplyPlan(
	staging string,
	staging6 string,
	endpointIPs []string,
	tunnelInterface string,
	allowLAN bool,
) []iptablesCommand {
	plan := make([]iptablesCommand, 0, 32)

	plan = append(plan,
		ipt6Optional("-D", "OUTPUT", "-j", staging6),
		ipt6Optional("-F", staging6),
		ipt6Optional("-X", staging6),
		ipt6("create IPv6 chain", "-N", staging6),
		ipt6("allow IPv6 loopback", "-A", staging6, "-o", "lo", "-j", "ACCEPT"),
		ipt6("add IPv6 drop rule", "-A", staging6, "-j", "DROP"),
		ipt6("insert IPv6 jump", "-I", "OUTPUT", "1", "-j", staging6),
	)
	for _, stale := range []string{ipt6ChainName, ipt6ChainNameAlt} {
		if stale == staging6 {
			continue
		}
		plan = append(plan,
			ipt6Optional("-D", "OUTPUT", "-j", stale),
			ipt6Optional("-F", stale),
			ipt6Optional("-X", stale),
		)
	}

	plan = append(plan,
		ipt4Optional("-D", "OUTPUT", "-j", staging),
		ipt4Optional("-F", staging),
		ipt4Optional("-X", staging),
		ipt4("create chain", "-N", staging),
		ipt4("allow loopback", "-A", staging, "-o", "lo", "-j", "ACCEPT"),
		ipt4("allow DHCP", "-A", staging, "-p", "udp", "--sport", "68", "--dport", "67", "-j", "ACCEPT"),
	)

	for _, ip := range endpointIPs {
		if strings.Contains(ip, ":") {
			continue // v6 endpoints unsupported; the v6 chain blocks all.
		}
		plan = append(plan, ipt4("allow endpoint "+ip, "-A", staging, "-d", ip, "-j", "ACCEPT"))
	}

	if allowLAN {
		for _, cidr := range LANAllowPrefixes {
			plan = append(plan, ipt4("allow LAN "+cidr, "-A", staging, "-d", cidr, "-j", "ACCEPT"))
		}
	}

	if tunnelInterface != "" {
		plan = append(plan, ipt4("allow tunnel interface", "-A", staging, "-o", tunnelInterface, "-j", "ACCEPT"))
	}

	// Complete from here; only now is it reachable.
	plan = append(plan,
		ipt4("add drop rule", "-A", staging, "-j", "DROP"),
		ipt4("insert jump", "-I", "OUTPUT", "1", "-j", staging),
	)

	for _, stale := range []string{iptChainName, iptChainNameAlt} {
		if stale == staging {
			continue
		}
		plan = append(plan,
			ipt4Optional("-D", "OUTPUT", "-j", stale),
			ipt4Optional("-F", stale),
			ipt4Optional("-X", stale),
		)
	}

	return plan
}

func iptablesRemovePlan() []iptablesCommand {
	plan := make([]iptablesCommand, 0, 12)
	for _, chain := range []string{iptChainName, iptChainNameAlt} {
		plan = append(plan,
			ipt4Optional("-D", "OUTPUT", "-j", chain),
			ipt4Optional("-F", chain),
			ipt4Optional("-X", chain),
		)
	}
	for _, chain := range []string{ipt6ChainName, ipt6ChainNameAlt} {
		plan = append(plan,
			ipt6Optional("-D", "OUTPUT", "-j", chain),
			ipt6Optional("-F", chain),
			ipt6Optional("-X", chain),
		)
	}
	return plan
}

// A chain is live only if the jump exists AND it still ends in DROP.
func iptablesLiveChainProbe(chain string) (jump []string, terminal []string) {
	return withWait([]string{"-C", "OUTPUT", "-j", chain}),
		withWait([]string{"-C", chain, "-j", "DROP"})
}

// Execution lives here rather than killswitch_linux.go so it stays testable
// without a Linux machine — there is no Linux CI job.

var runIPTablesCommand = func(ctx context.Context, binary string, args ...string) error {
	cmd := exec.CommandContext(ctx, binary, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (%s)", binary, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Refuses rather than guesses when the live chain is unknown: guessing "nothing
// installed" while a chain is live would stage into it and tear it down.
func applyIPTablesRules(ctx context.Context, endpointIPs []string, tunnelInterface string, allowLAN bool) error {
	live, ok := liveIPTablesChain(ctx, "iptables", iptChainName, iptChainNameAlt)
	if !ok {
		return errors.New("cannot determine the live IPv4 kill-switch chain; refusing to rebuild")
	}
	live6, ok := liveIPTablesChain(ctx, "ip6tables", ipt6ChainName, ipt6ChainNameAlt)
	if !ok {
		return errors.New("cannot determine the live IPv6 kill-switch chain; refusing to rebuild")
	}

	plan := iptablesApplyPlan(
		iptablesStagingChain(live),
		iptables6StagingChain(live6),
		endpointIPs, tunnelInterface, allowLAN,
	)
	for _, cmd := range plan {
		err := runIPTablesCommand(ctx, cmd.Binary, cmd.Args...)
		if err != nil && !cmd.BestEffort {
			return fmt.Errorf("%s: %w", cmd.Purpose, err)
		}
	}
	return nil
}

// Returns "" when neither chain is live; ok=false when the probes couldn't
// answer, which callers must not read as "absent".
func liveIPTablesChain(ctx context.Context, binary, primary, alternate string) (string, bool) {
	for _, chain := range []string{primary, alternate} {
		jumpArgs, terminalArgs := iptablesLiveChainProbe(chain)

		hooked, ok := probeIPTablesRule(ctx, binary, jumpArgs)
		if !ok {
			return "", false
		}
		if !hooked {
			continue
		}
		complete, ok := probeIPTablesRule(ctx, binary, terminalArgs)
		if !ok {
			return "", false
		}
		if complete {
			return chain, true
		}
	}
	return "", true
}

// Only exit 1 means "no such rule". Anything else (2 usage, 3/4 resource or
// xtables lock, exec failure) means the question went unanswered.
func probeIPTablesRule(ctx context.Context, binary string, args []string) (present bool, determined bool) {
	err := runIPTablesCommand(ctx, binary, args...)
	if err == nil {
		return true, true
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
		return false, true
	}
	return false, false
}

// Best-effort per command, but failures are aggregated so Clear can report a
// half-teardown — that's how a jump ends up pointing at an emptied chain.
func removeIPTablesRules(ctx context.Context) error {
	var failures []string
	for _, cmd := range iptablesRemovePlan() {
		err := runIPTablesCommand(ctx, cmd.Binary, cmd.Args...)
		if err == nil {
			continue
		}
		if gone, determined := iptablesTargetAbsent(ctx, cmd); determined && gone {
			continue
		}
		failures = append(failures, fmt.Sprintf("%s %s: %v", cmd.Binary, strings.Join(cmd.Args, " "), err))
	}
	if len(failures) > 0 {
		return fmt.Errorf("iptables teardown incomplete: %s", strings.Join(failures, "; "))
	}
	return nil
}

// Checks whether a failed teardown step had nothing to do, without re-running
// the mutation. Deletes are checked with -C; chain flush/delete with -S.
func iptablesTargetAbsent(ctx context.Context, cmd iptablesCommand) (absent bool, determined bool) {
	args := cmd.Args
	if len(args) >= 2 && args[0] == "-w" {
		args = args[2:]
	}
	if len(args) < 2 {
		return false, false
	}
	switch args[0] {
	case "-D":
		present, ok := probeIPTablesRule(ctx, cmd.Binary, withWait(append([]string{"-C"}, args[1:]...)))
		return !present, ok
	case "-F", "-X":
		present, ok := probeIPTablesRule(ctx, cmd.Binary, withWait([]string{"-S", args[1]}))
		return !present, ok
	}
	return false, false
}
