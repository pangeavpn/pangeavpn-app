//go:build darwin

package platform

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

const liveTarget = "1.1.1.1"

func runAs(user string, name string, args ...string) error {
	if user == "" {
		return exec.Command(name, args...).Run()
	}
	return exec.Command("sudo", append([]string{"-u", user, name}, args...)...).Run()
}

func ncReaches(user string) bool {
	return runAs(user, "/usr/bin/nc", "-z", "-G", "3", liveTarget, "443") == nil
}

func digReaches() bool {
	return exec.Command("/usr/bin/dig", "+time=2", "+tries=1", "@"+liveTarget, "example.com").Run() == nil
}

func pfctl(t *testing.T, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command("/sbin/pfctl", args...)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pfctl %v: %v (%s)", args, err, out)
	}
	return string(out)
}

// Real pf must pass root's sockets (the daemon) to a daemon-only address, keep other
// users' sockets blocked from it, and keep port 53 to it blocked even for root.
func TestLivePFDaemonEgressPassesRootOnly(t *testing.T) {
	if os.Getenv("PANGEA_LIVE_LOCK") != "1" {
		t.Skip("set PANGEA_LIVE_LOCK=1 on a disposable machine")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if !ncReaches("") || !ncReaches("nobody") || !digReaches() {
		t.Skipf("%s is not reachable from this machine without pf", liveTarget)
	}

	rules := append(pfDaemonEgressRules([]string{liveTarget}), "block out quick inet proto { tcp udp } to "+liveTarget)
	pfctl(t, "anchor \"pangea-live\"\n", "-f", "-")
	t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-f", "/etc/pf.conf").Run() })
	pfctl(t, strings.Join(rules, "\n")+"\n", "-a", "pangea-live", "-f", "-")
	t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-a", "pangea-live", "-F", "all").Run() })
	token := regexp.MustCompile(`Token : (\d+)`).FindStringSubmatch(pfctl(t, "", "-E"))
	if token != nil {
		t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-X", token[1]).Run() })
	}
	t.Logf("anchor:\n%s", pfctl(t, "", "-a", "pangea-live", "-s", "rules"))

	if !ncReaches("") {
		t.Error("root (the daemon) was refused through its daemon-only pass")
	}
	if ncReaches("nobody") {
		t.Error("another user's socket reached the shared address through the daemon-only pass")
	}
	if digReaches() {
		t.Error("root sent DNS to the shared address off-tunnel")
	}
}
