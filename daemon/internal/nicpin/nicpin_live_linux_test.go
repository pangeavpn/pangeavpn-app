//go:build linux && !android

package nicpin

import (
	"context"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/platform"
)

const liveEdge = "104.21.24.200"

func ipCmd(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
		t.Fatalf("ip %v: %v (%s)", args, err, out)
	}
}

// listenIn opens a TCP listener on addr inside the named network namespace.
func listenIn(t *testing.T, ns, addr string) net.Listener {
	t.Helper()
	type result struct {
		ln  net.Listener
		err error
	}
	ch := make(chan result, 1)
	go func() {
		// The thread is never unlocked, so it dies with this goroutine instead of returning to the pool.
		runtime.LockOSThread()
		f, err := os.Open("/var/run/netns/" + ns)
		if err != nil {
			ch <- result{err: err}
			return
		}
		defer f.Close()
		if err := unix.Setns(int(f.Fd()), unix.CLONE_NEWNET); err != nil {
			ch <- result{err: err}
			return
		}
		ln, err := net.Listen("tcp4", addr)
		ch <- result{ln, err}
	}()
	r := <-ch
	if r.err != nil {
		t.Fatalf("listen in %s: %v", ns, r.err)
	}
	t.Cleanup(func() { r.ln.Close() })
	go func() {
		for {
			c, err := r.ln.Accept()
			if err != nil {
				return
			}
			c.Close()
		}
	}()
	return r.ln
}

func dialMarked(mark uint32) error {
	d := net.Dialer{Timeout: 2 * time.Second}
	if mark != 0 {
		d.Control = (&Pin{Mark: mark}).Control()
	}
	c, err := d.Dial("tcp4", liveEdge+":443")
	if err == nil {
		c.Close()
	}
	return err
}

// A host routed like the daemon's tunnel (unmarked traffic to a dead "wg" table) with strict
// rp_filter: a marked socket's replies are dropped until the pin carries the mark onto them.
func TestLiveStrictReversePathAdmitsOnlyPinnedReplies(t *testing.T) {
	if os.Getenv("PANGEA_LIVE_PIN") != "1" {
		t.Skip("set PANGEA_LIVE_PIN=1 in a scratch network namespace")
	}
	self, _ := os.Readlink("/proc/self/ns/net")
	initNS, _ := os.Readlink("/proc/1/ns/net")
	if os.Geteuid() != 0 || self == initNS {
		t.Skip("needs root inside a scratch network namespace")
	}

	ipCmd(t, "netns", "add", "pgremote")
	t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", "pgremote").Run() })
	ipCmd(t, "link", "set", "lo", "up")
	ipCmd(t, "link", "add", "pgv0", "type", "veth", "peer", "name", "pgv1", "netns", "pgremote")
	ipCmd(t, "addr", "add", "10.88.0.1/24", "dev", "pgv0")
	ipCmd(t, "link", "set", "pgv0", "up")
	ipCmd(t, "-n", "pgremote", "link", "set", "lo", "up")
	ipCmd(t, "-n", "pgremote", "addr", "add", "10.88.0.2/24", "dev", "pgv1")
	ipCmd(t, "-n", "pgremote", "addr", "add", liveEdge+"/32", "dev", "pgv1")
	ipCmd(t, "-n", "pgremote", "link", "set", "pgv1", "up")
	ipCmd(t, "-n", "pgremote", "route", "add", "default", "via", "10.88.0.1")
	ipCmd(t, "route", "add", "default", "via", "10.88.0.2", "dev", "pgv0")

	// The daemon's policy routing: unmarked traffic to a tunnel table whose device goes nowhere.
	ipCmd(t, "link", "add", "pgwg0", "type", "dummy")
	ipCmd(t, "link", "set", "pgwg0", "up")
	ipCmd(t, "route", "add", "default", "dev", "pgwg0", "table", "51820")
	ipCmd(t, "rule", "add", "not", "fwmark", "0xca6c/0xca6c", "table", "51820", "priority", "100")
	ipCmd(t, "rule", "add", "table", "main", "suppress_prefixlength", "0", "priority", "99")
	for _, conf := range []string{"all", "pgv0"} {
		if err := os.WriteFile("/proc/sys/net/ipv4/conf/"+conf+"/rp_filter", []byte("1"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if strict, err := platform.StrictReversePathFilter(); err != nil || !strict {
		t.Fatalf("the namespace is not strict (%v, %v); the test would prove nothing", strict, err)
	}
	listenIn(t, "pgremote", liveEdge+":443")

	if err := dialMarked(0); err == nil {
		t.Fatal("an unmarked socket reached the edge; it should be in the dead tunnel table")
	}
	if err := dialMarked(pinMark); err == nil {
		t.Fatal("a marked socket got its reply under strict rp_filter without the fix; the test would prove nothing")
	}

	pin, err := Acquire(context.Background(), liveEdge)
	if err != nil {
		t.Fatal(err)
	}
	if err := dialMarked(pinMark); err != nil {
		t.Errorf("the pinned socket's replies were still dropped: %v", err)
	}
	if err := dialMarked(0); err == nil {
		t.Error("the fix let an unmarked socket reach the edge")
	}
	pin.Release()
	if out, err := exec.Command("nft", "list", "table", "inet", rpTable).CombinedOutput(); err == nil {
		t.Errorf("the reply-mark table outlived the last release:\n%s", out)
	}
	if out, _ := exec.Command("iptables", "-t", "mangle", "-S").CombinedOutput(); strings.Contains(string(out), rpTable) {
		t.Errorf("the reply-mark iptables rules outlived the last release:\n%s", out)
	}
}
