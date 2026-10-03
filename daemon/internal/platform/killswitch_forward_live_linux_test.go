//go:build linux

package platform

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockedBuffer collects a background command's output while the test reads it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) count(s string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Count(b.buf.String(), s)
}

func mustRun(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %s: %v (%s)", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func pingOnce(ns, dst string) bool {
	return exec.Command("ip", "netns", "exec", ns, "ping", "-n", "-c", "1", "-W", "1", dst).Run() == nil
}

// forwardTopology makes the current (scratch) netns a NAT host for a guest at
// 192.168.122.2, a routed guest at 10.0.3.2 and an uplink holding 8.8.8.8 and 198.51.100.7.
func forwardTopology(t *testing.T, useNFT bool) (guest, routed string) {
	t.Helper()
	id := fmt.Sprintf("%d%t", os.Getpid(), useNFT)
	guest, routed, inet := "ksfg"+id, "ksfr"+id, "ksfi"+id
	for _, ns := range []string{guest, routed, inet} {
		mustRun(t, "ip", "netns", "add", ns)
		t.Cleanup(func() { _ = exec.Command("ip", "netns", "del", ns).Run() })
		mustRun(t, "ip", "-n", ns, "link", "set", "lo", "up")
	}
	link := func(host, ns, hostAddr, nsAddr string) {
		mustRun(t, "ip", "link", "add", host, "type", "veth", "peer", "name", "eth0", "netns", ns)
		t.Cleanup(func() { _ = exec.Command("ip", "link", "del", host).Run() })
		mustRun(t, "ip", "addr", "add", hostAddr, "dev", host)
		mustRun(t, "ip", "link", "set", host, "up")
		mustRun(t, "ip", "-n", ns, "addr", "add", nsAddr, "dev", "eth0")
		mustRun(t, "ip", "-n", ns, "link", "set", "eth0", "up")
	}
	link("ksfg0", guest, "192.168.122.1/24", "192.168.122.2/24")
	link("ksfr0", routed, "10.0.3.1/24", "10.0.3.2/24")
	link("ksfe0", inet, "198.18.0.1/24", "198.18.0.2/24")
	mustRun(t, "ip", "-n", guest, "route", "add", "default", "via", "192.168.122.1")
	mustRun(t, "ip", "-n", routed, "route", "add", "default", "via", "10.0.3.1")
	mustRun(t, "ip", "-n", inet, "addr", "add", "8.8.8.8/32", "dev", "lo")
	mustRun(t, "ip", "-n", inet, "addr", "add", "198.51.100.7/32", "dev", "lo")
	mustRun(t, "ip", "route", "replace", "default", "via", "198.18.0.2")
	const forwarding = "/proc/sys/net/ipv4/ip_forward"
	if prev, err := os.ReadFile(forwarding); err == nil {
		t.Cleanup(func() { _ = os.WriteFile(forwarding, prev, 0o644) })
	}
	if err := os.WriteFile(forwarding, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Masquerade like a Docker or libvirt host, which also has conntrack tracking
	// guest flows before the lock lands.
	if useNFT {
		mustRun(t, "nft", "add table ip ksf_nat; add chain ip ksf_nat post { type nat hook postrouting priority 100; }; add rule ip ksf_nat post oifname ksfe0 masquerade")
		t.Cleanup(func() { _ = exec.Command("nft", "delete", "table", "ip", "ksf_nat").Run() })
	} else {
		mustRun(t, "iptables", "-w", "5", "-t", "nat", "-A", "POSTROUTING", "-o", "ksfe0", "-j", "MASQUERADE")
		t.Cleanup(func() {
			_ = exec.Command("iptables", "-w", "5", "-t", "nat", "-D", "POSTROUTING", "-o", "ksfe0", "-j", "MASQUERADE").Run()
		})
	}
	return guest, routed
}

// KS-R2-1: a forwarded guest whose own subnet lies in an excluded range must not
// leave off-tunnel while the lock holds no tunnel, on a new flow or one opened before.
func TestLiveForwardLockKeepsGuestsInAnExcludedRangeLocked(t *testing.T) {
	requireScratchNetns(t)
	for _, tool := range []string{"ip", "ping"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("no %s", tool)
		}
	}
	for _, backend := range []string{"nft", "iptables"} {
		t.Run(backend, func(t *testing.T) {
			useNFT := backend == "nft"
			if useNFT && !hasNFT(context.Background()) {
				t.Skip("no nft")
			}
			if _, err := exec.LookPath("iptables"); !useNFT && err != nil {
				t.Skip("no iptables")
			}
			isolateStateDir(t)
			prevAvail := nftAvailable
			t.Cleanup(func() { nftAvailable = prevAvail })
			nftAvailable = func(context.Context) bool { return useNFT }

			guest, routed := forwardTopology(t, useNFT)
			var replies lockedBuffer
			ping := exec.Command("ip", "netns", "exec", guest, "ping", "-n", "-i", "0.2", "-c", "60", "8.8.8.8")
			ping.Stdout = &replies
			if err := ping.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = ping.Process.Kill(); _ = ping.Wait() })
			for deadline := time.Now().Add(5 * time.Second); replies.count("bytes from") < 3; time.Sleep(100 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("the guest never reached 8.8.8.8 before the lock; the topology is broken")
				}
			}

			ks := &linuxKillSwitch{}
			ctx := context.Background()
			t.Cleanup(func() { _ = ks.Clear(context.Background()) })
			if err := ks.Enable(ctx, []string{"203.0.113.10"}, false, false); err != nil {
				t.Fatalf("Enable: %v", err)
			}
			if ks.useNFT != useNFT {
				t.Fatalf("armed with nft=%v, want %v", ks.useNFT, useNFT)
			}
			if err := ks.SetSplitCIDRs(ctx, []string{"192.168.0.0/16", "198.51.100.0/24"}); err != nil {
				t.Fatalf("SetSplitCIDRs: %v", err)
			}

			time.Sleep(500 * time.Millisecond)
			before := replies.count("bytes from")
			time.Sleep(3 * time.Second)
			if leaked := replies.count("bytes from") - before; leaked > 0 {
				t.Errorf("a flow the guest opened before the lock got %d replies from 8.8.8.8 off-tunnel", leaked)
			}
			if pingOnce(guest, "8.8.8.8") {
				t.Error("a guest inside an excluded range opened a new flow to 8.8.8.8 off-tunnel")
			}
			if !pingOnce(routed, "198.51.100.7") {
				t.Error("a routed guest lost the excluded range: its replies were dropped")
			}
			if !pingOnce(guest, "198.51.100.7") {
				t.Error("a guest inside an excluded range lost the other excluded range")
			}
		})
	}
}
