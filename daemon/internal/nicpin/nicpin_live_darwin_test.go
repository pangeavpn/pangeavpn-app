//go:build darwin && !ios

package nicpin

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

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

// With an unscoped reject route standing in for the tunnel's routes, a socket bound to the
// NIC must reach the target through the pin's scoped host route, and an unbound one must not.
func TestLiveScopedRouteServesOnlyBoundSockets(t *testing.T) {
	if os.Getenv("PANGEA_LIVE_PIN") != "1" {
		t.Skip("set PANGEA_LIVE_PIN=1 on a disposable machine")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if err := dialVia(nil); err != nil {
		t.Skipf("%s is not reachable from this machine: %v", liveTarget, err)
	}
	if out, err := exec.Command("/sbin/route", "-n", "add", "-host", liveTarget, "-reject", "127.0.0.1").CombinedOutput(); err != nil {
		t.Fatalf("add reject route: %v (%s)", err, out)
	}
	t.Cleanup(func() { _ = exec.Command("/sbin/route", "-n", "delete", "-host", liveTarget).Run() })
	if err := dialVia(nil); err == nil {
		t.Fatal("the reject route did not reject; the rest of the test would prove nothing")
	}

	pin, err := Acquire(context.Background(), liveTarget)
	if err != nil {
		t.Fatal(err)
	}
	if !scopedHostRoute(t, liveTarget, pin.Interface) {
		t.Fatalf("no scoped host route to %s on %s after Acquire", liveTarget, pin.Interface)
	}
	if err := dialVia(pin); err != nil {
		t.Errorf("a socket bound to %s could not use the pin's scoped route: %v", pin.Interface, err)
	}
	if err := dialVia(nil); err == nil {
		t.Error("an unbound socket followed the pin's scoped route around the reject route")
	}

	// A second holder keeps the route through the first one's release.
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
