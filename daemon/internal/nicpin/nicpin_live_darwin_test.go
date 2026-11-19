//go:build darwin && !ios

package nicpin

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
)

// TestMain serves the egress broker when this binary is spawned as one, as the daemon does.
func TestMain(m *testing.M) {
	if slices.Contains(os.Args[1:], "--split-egress-broker") {
		os.Exit(egress.RunBroker())
	}
	os.Exit(m.Run())
}

func requireLiveRoot(t *testing.T) {
	t.Helper()
	if os.Getenv("PANGEA_LIVE_PIN") != "1" {
		t.Skip("set PANGEA_LIVE_PIN=1 on a disposable machine")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if err := dialVia(nil); err != nil {
		t.Skipf("%s is not reachable from this machine: %v", liveTarget, err)
	}
}

func brokerDial(pin *Pin) error {
	conn, err := pin.Dial(context.Background(), "tcp4", liveTarget+":443")
	if err == nil {
		conn.Close()
	}
	return err
}

// scopedHostRoute reports whether the table holds an interface-scoped host route to ip on iface.
func scopedHostRoute(t *testing.T, ip, iface string) bool {
	t.Helper()
	out, err := exec.Command("/usr/sbin/netstat", "-rn", "-f", "inet").CombinedOutput()
	if err != nil {
		t.Fatalf("netstat: %v (%s)", err, out)
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) >= 4 && f[0] == ip && strings.Contains(f[2], "I") && f[3] == iface {
			return true
		}
	}
	return false
}

func rejectRoute(t *testing.T) {
	t.Helper()
	if out, err := exec.Command("/sbin/route", "-n", "add", "-host", liveTarget, "-reject", "127.0.0.1").CombinedOutput(); err != nil {
		t.Fatalf("add reject route: %v (%s)", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("/sbin/route", "-n", "delete", "-host", liveTarget).Run() })
	if err := dialVia(nil); err == nil {
		t.Fatal("the reject route did not reject; the rest of the test would prove nothing")
	}
}

// With an unscoped reject route standing in for the tunnel's routes, the broker's bound
// socket must reach the target through the pin's scoped host route, and nothing unbound may.
func TestLiveScopedRouteServesOnlyBoundSockets(t *testing.T) {
	requireLiveRoot(t)
	rejectRoute(t)

	pin, err := Acquire(context.Background(), liveTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !scopedHostRoute(t, liveTarget, pin.Interface) {
		t.Fatalf("no scoped host route to %s on %s after Acquire", liveTarget, pin.Interface)
	}
	if err := brokerDial(pin); err != nil {
		t.Errorf("the broker's socket could not use the pin's scoped route: %v", err)
	}
	if err := dialVia(nil); err == nil {
		t.Error("an unbound socket followed the pin's scoped route around the reject route")
	}
	if _, err := pin.Dial(context.Background(), "tcp4", "1.0.0.1:443"); err == nil {
		t.Error("the pinned dial accepted an address it was not pinned to")
	}

	second, err := Acquire(context.Background(), liveTarget)
	if err != nil {
		t.Fatal(err)
	}
	pin.Release()
	if !scopedHostRoute(t, liveTarget, second.Interface) {
		t.Error("one holder's release pulled the route out from under another")
	}
	second.Release()
	if scopedHostRoute(t, liveTarget, second.Interface) {
		t.Error("the scoped host route outlived its last Release")
	}
}

// End to end: under a pf lock passing only the broker's group, the pinned dial gets through
// and a plain root socket (any other root process) does not.
func TestLivePinnedDialIsTheOnlyWayThroughPF(t *testing.T) {
	requireLiveRoot(t)
	gid, err := platform.SplitEgressGroupID()
	if err != nil {
		t.Skipf("no usable %s group: %v", platform.SplitEgressGroupName, err)
	}
	pin, err := Acquire(context.Background(), liveTarget)
	if err != nil {
		t.Fatal(err)
	}
	defer pin.Release()

	rules := fmt.Sprintf("pass out quick inet proto tcp to %s group %d\nblock out quick inet proto { tcp udp } to %s\n", liveTarget, gid, liveTarget)
	run := func(stdin string, args ...string) string {
		cmd := exec.Command("/sbin/pfctl", args...)
		cmd.Stdin = strings.NewReader(stdin)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("pfctl %v: %v (%s)", args, err, out)
		}
		return string(out)
	}
	run("anchor \"pangea-live\"\n", "-f", "-")
	t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-f", "/etc/pf.conf").Run() })
	run(rules, "-a", "pangea-live", "-f", "-")
	t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-a", "pangea-live", "-F", "all").Run() })
	if token := regexp.MustCompile(`Token : (\d+)`).FindStringSubmatch(run("", "-E")); token != nil {
		t.Cleanup(func() { _ = exec.Command("/sbin/pfctl", "-X", token[1]).Run() })
	}

	if err := brokerDial(pin); err != nil {
		t.Errorf("the daemon's pinned dial was blocked: %v", err)
	}
	if err := dialVia(nil); err == nil {
		t.Error("a plain root socket got through the daemon-only pass")
	}
	if err := dialVia(pin); err == nil {
		t.Error("a root socket bound to the NIC, but not the broker's, got through")
	}
}
