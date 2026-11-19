//go:build linux

package platform

import (
	"context"
	"net"
	"os/exec"
	"syscall"
	"testing"
)

const liveEdge = "104.21.24.200"

// sendUDP reports whether the output hook let one datagram to dst out, the socket marked or not.
func sendUDP(t *testing.T, dst string, mark int) error {
	t.Helper()
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		if mark == 0 {
			return nil
		}
		var serr error
		if err := c.Control(func(fd uintptr) { serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, mark) }); err != nil {
			return err
		}
		return serr
	}}
	conn, err := lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	addr, err := net.ResolveUDPAddr("udp4", dst)
	if err != nil {
		t.Fatal(err)
	}
	_, err = conn.WriteTo([]byte("x"), addr)
	return err
}

// The lock must let the daemon's marked sockets reach a shared edge and nothing else:
// an unmarked app socket to the same address, any other address, and port 53 all drop.
func TestLiveDaemonEgressPassesOnlyMarkedSocketsToTheEdge(t *testing.T) {
	requireScratchNetns(t)
	// A dummy link carrying the default route, so every send reaches the output hook.
	for _, args := range [][]string{
		{"link", "add", "pgdummy0", "type", "dummy"},
		{"addr", "add", "10.99.0.1/24", "dev", "pgdummy0"},
		{"link", "set", "pgdummy0", "up"},
		{"route", "add", "default", "dev", "pgdummy0"},
	} {
		if out, err := exec.Command("ip", args...).CombinedOutput(); err != nil {
			t.Skipf("ip %v: %v (%s)", args, err, out)
		}
	}
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", "pgdummy0").Run() })

	r := ksRules{EndpointIPs: []string{"203.0.113.5"}, Split: splitPermits{DaemonIPs: []string{liveEdge}}}
	backends := map[string]struct {
		apply, remove func(context.Context) error
		present       func() bool
	}{
		"nft": {
			apply:   func(ctx context.Context) error { return applyNFTRules(ctx, r) },
			remove:  removeNFTRules,
			present: func() bool { return hasNFT(context.Background()) },
		},
		"iptables": {
			apply:  func(ctx context.Context) error { return applyIPTablesRules(ctx, r) },
			remove: removeIPTablesRules,
			present: func() bool {
				_, err := exec.LookPath("iptables")
				return err == nil
			},
		},
	}
	ran := 0
	for name, b := range backends {
		if !b.present() {
			continue
		}
		ran++
		ctx := context.Background()
		if err := b.apply(ctx); err != nil {
			t.Fatalf("%s: lock rejected: %v", name, err)
		}
		mark := 0x1ca6c
		if err := sendUDP(t, liveEdge+":443", mark); err != nil {
			t.Errorf("%s: the daemon's marked socket could not reach the edge: %v", name, err)
		}
		if err := sendUDP(t, liveEdge+":443", 0); err == nil {
			t.Errorf("%s: an unmarked socket reached the shared edge off-tunnel", name)
		}
		if err := sendUDP(t, "104.21.24.201:443", mark); err == nil {
			t.Errorf("%s: the mark opened an address that is not a daemon-only edge", name)
		}
		if err := sendUDP(t, liveEdge+":53", mark); err == nil {
			t.Errorf("%s: the marked socket sent DNS to the edge off-tunnel", name)
		}
		if err := sendUDP(t, "203.0.113.5:443", 0); err != nil {
			t.Errorf("%s: a dedicated endpoint lost its all-app permit: %v", name, err)
		}
		if err := b.remove(ctx); err != nil {
			t.Fatalf("%s: remove: %v", name, err)
		}
	}
	if ran == 0 {
		t.Skip("neither nft nor iptables is installed")
	}
}
