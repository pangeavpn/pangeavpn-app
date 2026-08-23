//go:build darwin

package platform

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

func TestBuildPFRules_IPv4AndIPv6AllowRules(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20", "2001:db8::20"}, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}

	if !strings.Contains(rules, "pass out quick inet proto { tcp udp } to 198.51.100.20") {
		t.Fatalf("missing IPv4 endpoint allow rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick inet6 proto { tcp udp } to 2001:db8::20") {
		t.Fatalf("missing IPv6 endpoint allow rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick inet proto udp from any port 68 to any port 67") {
		t.Fatalf("missing DHCP allow rule:\n%s", rules)
	}
	if !strings.Contains(rules, "pass out quick on utun9 all") {
		t.Fatalf("missing tunnel allow rule:\n%s", rules)
	}
}

func TestBuildPFRules_RejectsInvalidTunnelName(t *testing.T) {
	if _, err := buildPFRules(nil, "utun9; block", false); err == nil {
		t.Fatal("expected error for invalid tunnel interface name")
	}
}

func TestBuildPFRules_LANPrefixesUseMatchingAddressFamily(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", true)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}

	if !strings.Contains(rules, "pass out quick inet to 192.168.0.0/16") {
		t.Fatalf("missing IPv4 LAN allow rule:\n%s", rules)
	}
	for _, cidr := range []string{"fe80::/10", "ff02::/16", "fc00::/7"} {
		if !strings.Contains(rules, "pass out quick inet6 to "+cidr) {
			t.Fatalf("IPv6 LAN prefix %s not emitted as inet6:\n%s", cidr, rules)
		}
		if strings.Contains(rules, "pass out quick inet to "+cidr) {
			t.Fatalf("IPv6 LAN prefix %s emitted as inet, pfctl will reject it:\n%s", cidr, rules)
		}
	}
}

func TestBuildPFRules_TunnelRuleCoversBothFamilies(t *testing.T) {
	rules, err := buildPFRules(nil, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if !strings.Contains(rules, "pass out quick on utun9 all") {
		t.Fatalf("tunnel rule should not be pinned to one address family:\n%s", rules)
	}
}

// The shipped wedge: Enable held the one mutex Active() needs across DNS
// resolution and pfctl, so a hung resolve froze every /status response.
func TestDarwinKillSwitch_ActiveDoesNotBlockBehindEnable(t *testing.T) {
	t.Setenv("PANGEA_APP_SUPPORT_DIR", t.TempDir())
	prevLookup := lookupResolverIP
	defer func() { lookupResolverIP = prevLookup }()

	resolveStarted := make(chan struct{})
	release := make(chan struct{})
	lookupResolverIP = func(ctx context.Context, _, _ string) ([]net.IP, error) {
		close(resolveStarted)
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, context.Canceled
	}

	ks := &darwinKillSwitch{}
	enableCtx, cancelEnable := context.WithCancel(context.Background())
	enableDone := make(chan struct{})
	go func() {
		defer close(enableDone)
		_ = ks.Enable(enableCtx, []string{"node.example.com"}, false, false)
	}()
	<-resolveStarted

	activeDone := make(chan bool, 1)
	go func() { activeDone <- ks.Active() }()
	select {
	case active := <-activeDone:
		if active {
			t.Error("kill switch reported active before Enable completed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Active() blocked behind an in-flight Enable")
	}

	cancelEnable()
	close(release)
	<-enableDone
}

// The shipped outage: stateful lo0 filtering made macOS pf drop loopback TCP
// the moment the lock armed, so the app could no longer reach its own daemon.
func TestBuildPFRules_LoopbackIsStatelessBothDirections(t *testing.T) {
	rules, err := buildPFRules([]string{"198.51.100.20"}, "utun9", false)
	if err != nil {
		t.Fatalf("buildPFRules: %v", err)
	}
	if !strings.Contains(rules, "pass out quick on lo0 all no state") {
		t.Fatalf("outbound lo0 rule must be stateless:\n%s", rules)
	}
	if !strings.Contains(rules, "pass in quick on lo0 all no state") {
		t.Fatalf("inbound lo0 rule must exist and be stateless:\n%s", rules)
	}
}
