//go:build darwin

package procmatch

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func hostDarwinEnv() *ruleEnv {
	return newRuleEnv("darwin", hostSettings())
}

func ownDarwinImage(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	p, err := pidPath(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(ownImagesDarwin(), p) {
		t.Fatalf("proc_pidpath %q is none of %q", p, ownImagesDarwin())
	}
	return exe
}

func TestDarwinLiveProcessSources(t *testing.T) {
	ppid, start, err := kinfo(os.Getpid())
	if err != nil || ppid != os.Getppid() || start <= 0 {
		t.Fatalf("kinfo = %d %d %v", ppid, start, err)
	}
	if _, _, err := kinfo(1 << 30); err == nil {
		t.Fatal("kinfo of a missing pid succeeded")
	}
	ownDarwinImage(t)
	s := openOwnSockets(t)
	c, err := newDarwinClassifier(hostDarwinEnv(), 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	snap, _ := c.snapshot([]FlowID{s.tcp, s.udp})
	for _, f := range []FlowID{s.tcp, s.udp} {
		if o, ok := snap.owner(f); !ok || !o.has(os.Getpid()) {
			t.Fatalf("proto %d owner = %+v %v", f.Proto, o, ok)
		}
	}
}

func TestDarwinLiveSelfClassification(t *testing.T) {
	c, err := NewClassifier(Options{SelfPID: os.Getpid(), Logf: t.Logf})
	if err != nil {
		t.Fatalf("NewClassifier: %v", err)
	}
	defer c.Close()
	s := openOwnSockets(t)
	_, start, err := kinfo(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	rules := compileIn(t, hostDarwinEnv(), ownDarwinImage(t))
	res := c.Classify(rules, []FlowID{s.tcp, s.udp})
	for i, r := range res {
		if r.Verdict != VerdictTunnel || r.Owner.PID != os.Getpid() || r.Owner.Start != start || r.Owner.Chain != nil {
			t.Fatalf("flow %d: %+v; the classifier's own process must resolve and never bypass", i, r)
		}
	}
	checks := []SocketCheck{
		{Proto: protoTCP, App: s.tcp.App, Owner: res[0].Owner},
		{Proto: protoUDP, App: s.udp.App, Owner: res[1].Owner},
		{Proto: protoUDP, App: s.udp.App, Owner: Owner{PID: os.Getpid(), Start: start + 1}},
		{Proto: protoUDP, App: netip.MustParseAddrPort("127.0.0.1:1"), Owner: res[1].Owner},
	}
	if got := c.Validate(checks); !slices.Equal(got, []bool{true, true, false, false}) {
		t.Fatalf("Validate = %v", got)
	}
}

func TestDarwinLiveChildAncestry(t *testing.T) {
	h := startHelperTree(t, "")
	env := hostDarwinEnv()
	c, err := newDarwinClassifier(env, 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	game, launcher := h.gamePath, h.launcherPath
	if real, err := filepath.EvalSymlinks(game); err == nil {
		game = real
	}
	if real, err := filepath.EvalSymlinks(launcher); err == nil {
		launcher = real
	}
	for _, rules := range []*Rules{compileIn(t, env, h.launcherPath), compileIn(t, env, filepath.Dir(h.gamePath)+"/")} {
		for i, r := range c.Classify(rules, h.flows()) {
			if r.Verdict != VerdictBypass || r.Owner.PID != h.game.PID || len(r.Owner.Chain) < 2 || r.Owner.Chain[0] != game || r.Owner.Chain[1] != launcher {
				t.Fatalf("flow %d: %+v", i, r)
			}
		}
	}
	if r := c.Classify(compileIn(t, env, "/nowhere/other"), h.flows())[0]; r.Verdict != VerdictTunnel || r.Owner.PID != h.game.PID {
		t.Fatalf("other rule: %+v", r)
	}
}

func TestDarwinSelfTestRejectsWrongSelf(t *testing.T) {
	c, err := NewClassifier(Options{SelfPID: os.Getppid()})
	if err == nil {
		c.Close()
		t.Fatal("self-test passed for another process")
	}
	if !strings.Contains(err.Error(), "self-test") {
		t.Fatalf("error = %v", err)
	}
}

func TestDarwinStaleClassifyKeepsRefreshOff(t *testing.T) {
	c, err := newDarwinClassifier(hostDarwinEnv(), 0, nil, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	c.start()
	defer c.Close()
	c.ObserveRules(nil)
	// An engine still holding the previous rules classifies after they were emptied.
	flow := FlowID{Proto: protoTCP, App: netip.MustParseAddrPort("127.0.0.1:1"), Remote: netip.MustParseAddrPort("127.0.0.1:2")}
	c.Classify(compileIn(t, hostDarwinEnv(), "/nowhere/other"), []FlowID{flow})
	before := c.Stats().Refreshes
	time.Sleep(1300 * time.Millisecond)
	if n := c.Stats().Refreshes - before; n != 0 {
		t.Fatalf("refreshed %d times after the rules were emptied", n)
	}
}
