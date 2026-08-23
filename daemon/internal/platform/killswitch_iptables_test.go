package platform

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// Replays a command plan against a model of the kernel's chain state, so a test
// can assert what the real firewall looked like at every step. Untagged on
// purpose: the fallback is Linux-only but there is no Linux CI job.
type iptablesModel struct {
	chains      map[string][]string
	outputJumps []string
}

func newIPTablesModel() *iptablesModel {
	return &iptablesModel{chains: map[string][]string{}}
}

// stripWait drops the leading "-w N" that every command carries.
func stripWait(args []string) []string {
	if len(args) >= 2 && args[0] == "-w" {
		return args[2:]
	}
	return args
}

func (m *iptablesModel) apply(rawArgs []string) {
	args := stripWait(rawArgs)
	switch {
	case len(args) >= 2 && args[0] == "-N":
		if _, exists := m.chains[args[1]]; !exists {
			m.chains[args[1]] = nil
		}
	case len(args) >= 2 && args[0] == "-F":
		m.chains[args[1]] = nil
	case len(args) >= 2 && args[0] == "-X":
		delete(m.chains, args[1])
	case len(args) >= 5 && args[0] == "-I" && args[1] == "OUTPUT":
		m.outputJumps = append([]string{args[len(args)-1]}, m.outputJumps...)
	case len(args) >= 4 && args[0] == "-D" && args[1] == "OUTPUT":
		target := args[len(args)-1]
		for i, jump := range m.outputJumps {
			if jump == target {
				m.outputJumps = append(m.outputJumps[:i], m.outputJumps[i+1:]...)
				break
			}
		}
	case len(args) >= 2 && args[0] == "-A":
		m.chains[args[1]] = append(m.chains[args[1]], strings.Join(args[2:], " "))
	}
}

// liveChain returns the rules of the chain OUTPUT reaches that actually ends in
// a DROP. A jump to a half-built chain filters nothing.
func (m *iptablesModel) liveChain(prefix string) []string {
	for _, jump := range m.outputJumps {
		if !strings.HasPrefix(jump, prefix) {
			continue
		}
		rules := m.chains[jump]
		if len(rules) > 0 && rules[len(rules)-1] == "-j DROP" {
			return rules
		}
	}
	return nil
}

func (m *iptablesModel) liveChainName(prefix string) string {
	for _, jump := range m.outputJumps {
		if !strings.HasPrefix(jump, prefix) {
			continue
		}
		rules := m.chains[jump]
		if len(rules) > 0 && rules[len(rules)-1] == "-j DROP" {
			return jump
		}
	}
	return ""
}

// Matched by exact name: PANGEAVPN_KS6 is not a PANGEAVPN_KS chain.
func (m *iptablesModel) filtering(binary string) bool {
	for _, jump := range m.outputJumps {
		if isV6Chain(jump) != (binary == "ip6tables") {
			continue
		}
		rules := m.chains[jump]
		if len(rules) > 0 && rules[len(rules)-1] == "-j DROP" {
			return true
		}
	}
	return false
}

func isV6Chain(name string) bool {
	return name == ipt6ChainName || name == ipt6ChainNameAlt
}

// run feeds a plan through the model, reporting the first command after which a
// family was left without a complete filtering chain (guard on when armed).
func (m *iptablesModel) run(plan []iptablesCommand, armed bool) string {
	breach := ""
	for _, cmd := range plan {
		m.apply(cmd.Args)
		if !armed || breach != "" {
			continue
		}
		if !m.filtering("iptables") {
			breach = "iptables " + strings.Join(cmd.Args, " ")
		} else if !m.filtering("ip6tables") {
			breach = "ip6tables " + strings.Join(cmd.Args, " ")
		}
	}
	return breach
}

func (m *iptablesModel) v4Live() string {
	for _, jump := range m.outputJumps {
		if isV6Chain(jump) {
			continue
		}
		rules := m.chains[jump]
		if len(rules) > 0 && rules[len(rules)-1] == "-j DROP" {
			return jump
		}
	}
	return ""
}

func (m *iptablesModel) v6Live() string {
	for _, jump := range m.outputJumps {
		if !isV6Chain(jump) {
			continue
		}
		rules := m.chains[jump]
		if len(rules) > 0 && rules[len(rules)-1] == "-j DROP" {
			return jump
		}
	}
	return ""
}

// The old code deleted the OUTPUT jump and rebuilt in place, leaving the host
// unfiltered for ~15 execs on every re-arm.
func TestIPTablesApplyPlan_NeverLeavesOutputUnfiltered(t *testing.T) {
	model := newIPTablesModel()

	// Cold start: nothing installed, so a gap before the first jumps land is
	// expected and harmless.
	model.run(iptablesApplyPlan(iptChainName, ipt6ChainName, []string{"203.0.113.5"}, "wg-test", false), false)
	if !model.filtering("iptables") || !model.filtering("ip6tables") {
		t.Fatalf("not filtering after the first apply: jumps=%v", model.outputJumps)
	}

	// Re-arm for a different server — what Switch does while the old tunnel is
	// still carrying traffic. From here the guard is on for BOTH families.
	second := iptablesApplyPlan(
		iptablesStagingChain(model.v4Live()),
		iptables6StagingChain(model.v6Live()),
		[]string{"198.51.100.9"}, "wg-test", false,
	)
	if breach := model.run(second, true); breach != "" {
		t.Fatalf("kill switch went fail-open during re-arm at %q", breach)
	}

	// A third arm must swap back rather than accumulate chains.
	third := iptablesApplyPlan(
		iptablesStagingChain(model.v4Live()),
		iptables6StagingChain(model.v6Live()),
		[]string{"203.0.113.7"}, "wg-test", true,
	)
	if breach := model.run(third, true); breach != "" {
		t.Fatalf("kill switch went fail-open during third arm at %q", breach)
	}
	if got := len(model.outputJumps); got != 2 {
		t.Fatalf("expected exactly one IPv4 and one IPv6 jump after settling, got %d: %v", got, model.outputJumps)
	}
	if got := len(model.chains); got != 2 {
		t.Fatalf("expected exactly two chains to survive, got %d: %v", got, model.chains)
	}
}

// The permitted set must not grow across switches.
func TestIPTablesApplyPlan_ReArmReplacesEndpoints(t *testing.T) {
	model := newIPTablesModel()
	model.run(iptablesApplyPlan(iptChainName, ipt6ChainName, []string{"203.0.113.5"}, "wg-test", false), false)
	model.run(iptablesApplyPlan(
		iptablesStagingChain(model.v4Live()),
		iptables6StagingChain(model.v6Live()),
		[]string{"198.51.100.9"}, "wg-test", false,
	), false)

	rules := strings.Join(model.liveChain("PANGEAVPN_KS"), "\n")
	if !strings.Contains(rules, "198.51.100.9") {
		t.Fatalf("re-armed chain is missing the new endpoint:\n%s", rules)
	}
	if strings.Contains(rules, "203.0.113.5") {
		t.Fatalf("re-armed chain still permits the previous server's endpoint:\n%s", rules)
	}
}

// Turning Allow LAN off must actually close the LAN hole on the next arm.
func TestIPTablesApplyPlan_AllowLANCanBeTurnedOff(t *testing.T) {
	model := newIPTablesModel()
	model.run(iptablesApplyPlan(iptChainName, ipt6ChainName, []string{"203.0.113.5"}, "wg-test", true), false)
	if !strings.Contains(strings.Join(model.liveChain("PANGEAVPN_KS"), "\n"), "192.168.0.0/16") {
		t.Fatal("LAN permit missing when allowLAN is on")
	}

	model.run(iptablesApplyPlan(
		iptablesStagingChain(model.v4Live()),
		iptables6StagingChain(model.v6Live()),
		[]string{"203.0.113.5"}, "wg-test", false,
	), false)
	if strings.Contains(strings.Join(model.liveChain("PANGEAVPN_KS"), "\n"), "192.168.0.0/16") {
		t.Fatal("LAN permit survived allowLAN being turned off")
	}
}

func TestIPTablesStagingChain_AlwaysAvoidsTheLiveChain(t *testing.T) {
	if got := iptablesStagingChain(""); got != iptChainName {
		t.Fatalf("cold start staged into %q, want %q", got, iptChainName)
	}
	if iptablesStagingChain(iptChainName) == iptChainName {
		t.Fatal("staged into the live IPv4 chain")
	}
	if iptablesStagingChain(iptChainNameAlt) == iptChainNameAlt {
		t.Fatal("staged into the live IPv4 chain")
	}
	if iptables6StagingChain(ipt6ChainName) == ipt6ChainName {
		t.Fatal("staged into the live IPv6 chain")
	}
	if iptables6StagingChain(ipt6ChainNameAlt) == ipt6ChainNameAlt {
		t.Fatal("staged into the live IPv6 chain")
	}
}

// A partial teardown can leave the v6 jump pointing at an emptied chain, so the
// jump alone is not proof it is filtering.
func TestIPTablesApplyPlan_AlwaysRebuildsIPv6(t *testing.T) {
	plan := iptablesApplyPlan(iptChainNameAlt, ipt6ChainNameAlt, nil, "", false)
	var sawDrop, sawJump bool
	for _, cmd := range plan {
		if cmd.Binary != "ip6tables" {
			continue
		}
		args := strings.Join(stripWait(cmd.Args), " ")
		if args == "-A "+ipt6ChainNameAlt+" -j DROP" {
			sawDrop = true
		}
		if args == "-I OUTPUT 1 -j "+ipt6ChainNameAlt {
			sawJump = true
		}
	}
	if !sawDrop || !sawJump {
		t.Fatalf("re-arm did not rebuild the IPv6 chain (drop=%v jump=%v)", sawDrop, sawJump)
	}
}

// A v6 failure after the v4 swap would abort Enable with v4 already on the new
// endpoints, making Switch's "still on the old server" recovery wrong.
func TestIPTablesApplyPlan_IPv6PrecedesTheIPv4Swap(t *testing.T) {
	plan := iptablesApplyPlan(iptChainName, ipt6ChainName, []string{"203.0.113.5"}, "wg-test", false)
	lastRequiredV6, firstV4Endpoint := -1, -1
	for i, cmd := range plan {
		args := strings.Join(stripWait(cmd.Args), " ")
		if cmd.Binary == "ip6tables" && !cmd.BestEffort {
			lastRequiredV6 = i
		}
		if cmd.Binary == "iptables" && firstV4Endpoint == -1 && strings.Contains(args, "-d 203.0.113.5") {
			firstV4Endpoint = i
		}
	}
	if lastRequiredV6 == -1 || firstV4Endpoint == -1 {
		t.Fatalf("plan shape unexpected (v6=%d v4=%d)", lastRequiredV6, firstV4Endpoint)
	}
	if lastRequiredV6 > firstV4Endpoint {
		t.Fatal("an IPv6 step that can fail runs after the IPv4 endpoints are staged")
	}
}

func TestIPTablesApplyPlan_EveryCommandCarriesTheLockWait(t *testing.T) {
	plans := [][]iptablesCommand{
		iptablesApplyPlan(iptChainName, ipt6ChainName, []string{"203.0.113.5"}, "wg-test", true),
		iptablesRemovePlan(),
	}
	for _, plan := range plans {
		for _, cmd := range plan {
			if len(cmd.Args) < 2 || cmd.Args[0] != "-w" {
				t.Fatalf("%s %v does not wait for the xtables lock; a busy lock would be "+
					"misread as 'rule absent'", cmd.Binary, cmd.Args)
			}
		}
	}
}

func TestIPTablesRemovePlan_ClearsEveryChainName(t *testing.T) {
	var seen []string
	for _, cmd := range iptablesRemovePlan() {
		if !cmd.BestEffort {
			t.Fatalf("teardown command %v is not best-effort; a missing object would abort cleanup", cmd.Args)
		}
		seen = append(seen, cmd.Binary+" "+strings.Join(stripWait(cmd.Args), " "))
	}
	joined := strings.Join(seen, "\n")
	for _, chain := range []string{iptChainName, iptChainNameAlt, ipt6ChainName, ipt6ChainNameAlt} {
		if !strings.Contains(joined, "-X "+chain) {
			t.Fatalf("teardown never deletes chain %s:\n%s", chain, joined)
		}
	}
}

// ---------------------------------------------------------------------------
// Probe behaviour — the difference between "no such rule" and "couldn't tell"
// ---------------------------------------------------------------------------

// Wraps a real *exec.ExitError, matching how runIPTablesCommand wraps failures.
func exitCodeError(t *testing.T, code int) error {
	t.Helper()
	script := fmt.Sprintf("exit %d", code)
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", script)
	} else {
		cmd = exec.Command("sh", "-c", script)
	}
	err := cmd.Run()
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Skipf("cannot synthesise an ExitError on this platform: %v", err)
	}
	if exitErr.ExitCode() != code {
		t.Skipf("synthesised exit code %d, wanted %d", exitErr.ExitCode(), code)
	}
	return err
}

func TestProbeIPTablesRule_DistinguishesAbsentFromUnanswerable(t *testing.T) {
	original := runIPTablesCommand
	defer func() { runIPTablesCommand = original }()

	// Exit 0: the rule is there.
	runIPTablesCommand = func(context.Context, string, ...string) error { return nil }
	if present, determined := probeIPTablesRule(context.Background(), "iptables", nil); !present || !determined {
		t.Fatalf("exit 0 should mean present+determined, got present=%v determined=%v", present, determined)
	}

	// Exit 1: iptables' "no such rule".
	absent := exitCodeError(t, 1)
	runIPTablesCommand = func(context.Context, string, ...string) error { return absent }
	if present, determined := probeIPTablesRule(context.Background(), "iptables", nil); present || !determined {
		t.Fatalf("exit 1 should mean absent+determined, got present=%v determined=%v", present, determined)
	}

	// Exit 4: xtables lock held. Must NOT be read as "absent".
	locked := exitCodeError(t, 4)
	runIPTablesCommand = func(context.Context, string, ...string) error { return locked }
	if _, determined := probeIPTablesRule(context.Background(), "iptables", nil); determined {
		t.Fatal("xtables lock contention (exit 4) was reported as a determined answer; " +
			"reading it as 'rule absent' makes the rebuild stage into the live chain and tear it down")
	}

	// Non-exit failure (binary missing): also undeterminable.
	runIPTablesCommand = func(context.Context, string, ...string) error { return errors.New("exec: not found") }
	if _, determined := probeIPTablesRule(context.Background(), "iptables", nil); determined {
		t.Fatal("a failure to exec iptables was reported as a determined answer")
	}
}

// An unanswerable probe must refuse rather than guess.
func TestApplyIPTablesRules_RefusesWhenTheLiveChainIsUnknown(t *testing.T) {
	original := runIPTablesCommand
	defer func() { runIPTablesCommand = original }()

	locked := exitCodeError(t, 4)
	var mutating []string
	runIPTablesCommand = func(_ context.Context, binary string, args ...string) error {
		stripped := stripWait(args)
		if len(stripped) > 0 && stripped[0] == "-C" {
			return locked
		}
		mutating = append(mutating, binary+" "+strings.Join(stripped, " "))
		return nil
	}

	err := applyIPTablesRules(context.Background(), []string{"203.0.113.5"}, "wg-test", false)
	if err == nil {
		t.Fatal("apply proceeded despite being unable to determine the live chain")
	}
	if len(mutating) != 0 {
		t.Fatalf("apply mutated the firewall before establishing the live chain: %v", mutating)
	}
}

// Jump without a DROP is not live; treating it as live means never rebuilding.
func TestLiveIPTablesChain_IgnoresAHookedButEmptiedChain(t *testing.T) {
	original := runIPTablesCommand
	defer func() { runIPTablesCommand = original }()

	absent := exitCodeError(t, 1)
	runIPTablesCommand = func(_ context.Context, _ string, args ...string) error {
		stripped := stripWait(args)
		// Jump to the primary chain is present; its terminal DROP is not.
		if len(stripped) >= 4 && stripped[0] == "-C" && stripped[1] == "OUTPUT" && stripped[3] == iptChainName {
			return nil
		}
		return absent
	}

	live, ok := liveIPTablesChain(context.Background(), "iptables", iptChainName, iptChainNameAlt)
	if !ok {
		t.Fatal("probe should have been able to answer")
	}
	if live != "" {
		t.Fatalf("a hooked-but-emptied chain was reported live as %q", live)
	}
}

// iptables is the v4 binary; a v6 prefix in the LAN permits makes it reject
// the rule and the whole arm fails.
func TestIPTablesApplyPlan_LANPermitsAreIPv4Only(t *testing.T) {
	plan := iptablesApplyPlan(iptChainName, ipt6ChainName, []string{"198.51.100.20"}, "wg0", true)

	sawLAN := false
	for _, cmd := range plan {
		if cmd.Binary != "iptables" {
			continue
		}
		joined := strings.Join(cmd.Args, " ")
		if strings.Contains(joined, "192.168.0.0/16") {
			sawLAN = true
		}
		if strings.Contains(joined, "::") {
			t.Errorf("IPv6 address in an iptables command: %s", joined)
		}
	}
	if !sawLAN {
		t.Fatal("expected the IPv4 LAN permits to be present with allowLAN on")
	}
}
