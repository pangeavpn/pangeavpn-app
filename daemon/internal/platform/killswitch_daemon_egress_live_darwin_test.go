//go:build darwin

package platform

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
	"testing"
)

const liveTarget = "1.1.1.1"

// asCred runs name as root with gid as its group, or as plain root when gid is 0.
func asCred(gid int, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	if gid > 0 {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: &syscall.Credential{Uid: 0, Gid: uint32(gid)}}
	}
	return cmd.Run()
}

func ncReachesAs(gid int) bool {
	return asCred(gid, "/usr/bin/nc", "-z", "-G", "3", liveTarget, "443") == nil
}

func nobodyReaches() bool {
	return exec.Command("sudo", "-u", "nobody", "/usr/bin/nc", "-z", "-G", "3", liveTarget, "443").Run() == nil
}

func digReachesAs(gid int) bool {
	return asCred(gid, "/usr/bin/dig", "+time=2", "+tries=1", "@"+liveTarget, "example.com") == nil
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

// Real pf must pass only the egress broker's group to a daemon-only address: plain root
// (every other root process) and other users stay blocked, and port 53 stays blocked for all.
func TestLivePFDaemonEgressPassesOnlyTheBrokersGroup(t *testing.T) {
	if os.Getenv("PANGEA_LIVE_LOCK") != "1" {
		t.Skip("set PANGEA_LIVE_LOCK=1 on a disposable machine")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	gid, err := SplitEgressGroupID()
	if err != nil {
		t.Skipf("no usable %s group: %v", SplitEgressGroupName, err)
	}
	if !ncReachesAs(0) || !ncReachesAs(gid) || !nobodyReaches() || !digReachesAs(gid) {
		t.Skipf("%s is not reachable from this machine without pf", liveTarget)
	}

	rules := append(pfDaemonEgressRules([]string{liveTarget}, gid), "block out quick inet proto { tcp udp } to "+liveTarget)
	pfctl(t, "anchor \"pangea-live\"\n", "-f", "-")
	t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-f", "/etc/pf.conf").Run() })
	pfctl(t, strings.Join(rules, "\n")+"\n", "-a", "pangea-live", "-f", "-")
	t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-a", "pangea-live", "-F", "all").Run() })
	if token := regexp.MustCompile(`Token : (\d+)`).FindStringSubmatch(pfctl(t, "", "-E")); token != nil {
		t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-X", token[1]).Run() })
	}
	t.Logf("anchor:\n%s", pfctl(t, "", "-a", "pangea-live", "-s", "rules"))

	if !ncReachesAs(gid) {
		t.Error("the broker's group was refused through its daemon-only pass")
	}
	if ncReachesAs(0) {
		t.Error("a plain root socket (any root process) reached the shared address")
	}
	if nobodyReaches() {
		t.Error("another user's socket reached the shared address")
	}
	if digReachesAs(gid) {
		t.Error("the broker's group sent DNS to the shared address off-tunnel")
	}
}
