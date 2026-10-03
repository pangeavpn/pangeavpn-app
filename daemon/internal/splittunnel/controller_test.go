package splittunnel

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/egress"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/splittunnel/procmatch"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/wg"
)

func TestEnginesStartLazilyOnFirstPump(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
	h.setRules(appGame)
	time.Sleep(20 * time.Millisecond)
	if h.clsMade.Load() != 0 || h.egMade.Load() != 0 {
		t.Fatal("classifier/egress created before any device pumped")
	}
	if h.c.Status().AppsActive {
		t.Fatal("AppsActive before the pump started")
	}
	c, _ := h.mustDial(41200, dst(remoteB, 9200))
	roundTrip(t, c, "first flow starts the engine")
	c.Close()
	waitFor(t, 2*time.Second, "engines created", func() bool { return h.clsMade.Load() == 1 && h.egMade.Load() == 1 })
	if st := h.c.Status(); !st.AppsActive || st.UnavailableReason != "" {
		t.Fatalf("status after start = %+v", st)
	}

	other := h.c.WrapTUN(newFakeTUN(kindLinux), testTunnelInfo())
	defer other.Close()
	if h.clsMade.Load() != 1 || h.egMade.Load() != 1 {
		t.Fatal("a second device re-created the shared engines")
	}
}

func TestEngineInitFailuresKeepAppsInTunnel(t *testing.T) {
	for _, tc := range []struct {
		name   string
		o      harnessOpts
		reason string
	}{
		{"classifier", harnessOpts{clsErr: errors.New("self-test failed")}, ReasonClassifierFailed},
		{"egress", harnessOpts{egErr: errors.New("broker missing")}, ReasonEgressFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.o.peer = true
			h := newHarness(t, tc.o)
			h.eg.setTarget(h.server(echoConn))
			h.cls.set(41210, true, appGame)
			h.cls.set(41211, true, appGame)
			h.setRules(appGame)
			c, _ := h.mustDial(41210, dst(remoteA, 9210))
			roundTrip(t, c, "engine failed")
			c.Close()
			waitFor(t, 2*time.Second, "failure reported", func() bool { return h.c.Status().UnavailableReason == tc.reason })
			c, _ = h.mustDial(41211, dst(remoteA, 9211))
			roundTrip(t, c, "still tunnelled")
			c.Close()
			if h.eg.dials.Load() != 0 || h.c.Status().AppsActive {
				t.Fatal("bypass happened without a classifier and egress")
			}
			if !h.tunnelHasPort(41211) {
				t.Fatal("flow did not reach the tunnel")
			}
		})
	}
}

// pumpExtraDevice wraps a second tunnel and lets its first Read start the pump, as a reconnect does.
func pumpExtraDevice(t *testing.T, h *harness) {
	t.Helper()
	inner := newFakeTUN(kindLinux)
	d := h.c.WrapTUN(inner, testTunnelInfo())
	t.Cleanup(func() { _ = d.Close() })
	inner.fromOS(tunnelUDP(40500, []byte("kick")))
	bufs, sizes := readBufs(d.BatchSize(), 65535+wgOffset)
	if _, err := d.Read(bufs, sizes, wgOffset); err != nil {
		t.Fatalf("first Read on the new device: %v", err)
	}
	waitFor(t, 2*time.Second, "new device pumping", func() bool { return d.(*Device).pumping.Load() })
}

func TestFailedEnginesRetryWithoutRuleEdits(t *testing.T) {
	for _, tc := range []struct {
		name  string
		retry func(t *testing.T, h *harness)
	}{
		{"networkChanged", func(_ *testing.T, h *harness) { h.c.NetworkChanged() }},
		{"sameRules", func(_ *testing.T, h *harness) { h.setRules(appGame) }},
		{"newDevice", pumpExtraDevice},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var fail atomic.Bool
			fail.Store(true)
			h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
			h.c.opts.NewEgress = func() (egress.Dialer, error) {
				h.egMade.Add(1)
				if fail.Load() {
					return nil, errors.New("broker spawn failed")
				}
				return h.eg, nil
			}
			h.eg.setTarget(h.server(echoConn))
			h.cls.set(41230, true, appGame)
			h.cls.set(41231, true, appGame)
			h.setRules(appGame)
			c, _ := h.mustDial(41230, dst(remoteA, 9230))
			roundTrip(t, c, "egress failed")
			c.Close()
			waitFor(t, 2*time.Second, "failure reported", func() bool { return h.c.Status().UnavailableReason == ReasonEgressFailed })

			fail.Store(false)
			tc.retry(t, h)
			time.Sleep(50 * time.Millisecond)
			if n := h.egMade.Load(); n != 1 {
				t.Fatalf("egress created %d times before engineRetry passed, want 1", n)
			}
			h.c.mu.Lock()
			h.c.engFailedAt = time.Now().Add(-engineRetry - time.Second)
			h.c.mu.Unlock()
			tc.retry(t, h)
			waitFor(t, 2*time.Second, "engines retried", func() bool {
				st := h.c.Status()
				return st.AppsActive && st.UnavailableReason == ""
			})
			if n := h.egMade.Load(); n != 2 {
				t.Fatalf("egress created %d times, want 2", n)
			}
			c, _ = h.mustDial(41231, dst(remoteA, 9231))
			roundTrip(t, c, "bypassed after the retry")
			c.Close()
			if h.eg.dials.Load() != 1 || h.tunnelHasPort(41231) {
				t.Fatal("flow did not bypass after the engines recovered")
			}
		})
	}
}

func TestPermitOutlivesTeardownDuringDeviceChurn(t *testing.T) {
	for _, closeOther := range []bool{false, true} {
		name := "wrap"
		if closeOther {
			name = "close"
		}
		t.Run(name, func(t *testing.T) {
			rec := &permitRecorder{}
			h := newHarness(t, harnessOpts{kind: kindWindows, permit: rec.set})
			h.eg.setTarget(h.server(echoConn))
			var other tun.Device
			if closeOther {
				other = h.c.WrapTUN(newFakeTUN(kindWindows), testTunnelInfo())
			}
			h.cls.set(40112, true, appGame)
			h.setRules(appGame)
			waitFor(t, 2*time.Second, "permit granted", func() bool { return h.c.permitted.Load() })
			c, _ := h.mustDial(40112, dst(remoteA, 7112))
			roundTrip(t, c, "bypassed")

			// Holding the flow mutex parks SetRules(nil) inside its teardown, as a busy pump can.
			e := h.engine()
			e.mu.Lock()
			unlock := sync.OnceFunc(e.mu.Unlock)
			defer unlock()
			done := make(chan struct{})
			go func() {
				h.c.SetRules(nil, nil)
				close(done)
			}()
			waitFor(t, 2*time.Second, "teardown parked", func() bool {
				h.c.mu.Lock()
				defer h.c.mu.Unlock()
				return h.c.rules.empty()
			})
			if closeOther {
				_ = other.Close()
			} else {
				other = h.c.WrapTUN(newFakeTUN(kindWindows), testTunnelInfo())
			}
			time.Sleep(100 * time.Millisecond)
			early, gate := rec.seen(), h.c.permitted.Load()
			unlock()
			<-done
			expectReset(t, c)
			if !closeOther {
				_ = other.Close()
			}
			if len(early) != 1 || !early[0] {
				t.Fatalf("permit calls while the teardown was still running = %v", early)
			}
			if gate {
				t.Fatal("bypass gate open while the rules were being emptied")
			}
			waitFor(t, 2*time.Second, "permit withdrawn after the teardown", func() bool {
				got := rec.seen()
				return len(got) == 2 && !got[1]
			})
		})
	}
}

type permitRecorder struct {
	mu    sync.Mutex
	calls []bool
	err   error
}

func (p *permitRecorder) set(_ context.Context, on bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, on)
	return p.err
}

func (p *permitRecorder) seen() []bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]bool(nil), p.calls...)
}

func TestEgressPermitFollowsRulesAndDevices(t *testing.T) {
	rec := &permitRecorder{}
	c := NewController(ControllerOptions{SetEgressPermit: rec.set})
	c.compile = fakeCompile
	c.SetRules([]string{appGame}, nil)
	time.Sleep(20 * time.Millisecond)
	if len(rec.seen()) != 0 {
		t.Fatal("permit requested without a tunnel device")
	}
	dev := c.WrapTUN(newFakeTUN(kindWindows), testTunnelInfo())
	waitFor(t, 2*time.Second, "permit on", func() bool { return c.permitted.Load() })
	if got := rec.seen(); len(got) != 1 || !got[0] {
		t.Fatalf("permit calls = %v", got)
	}
	_ = dev.Close()
	waitFor(t, 2*time.Second, "permit off", func() bool { return len(rec.seen()) == 2 })
	if c.permitted.Load() || rec.seen()[1] {
		t.Fatal("permit not withdrawn when the last device closed")
	}

	dev = c.WrapTUN(newFakeTUN(kindWindows), testTunnelInfo())
	waitFor(t, 2*time.Second, "permit back on", func() bool { return len(rec.seen()) == 3 })
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	if got := rec.seen(); len(got) != 4 || got[3] {
		t.Fatalf("Controller.Close did not withdraw the permit: %v", got)
	}
	_ = dev.Close()
	checkNoEngineGoroutines(t)
}

func TestControllerCloseResetsBypassAndReleasesEngines(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true})
	h.eg.setTarget(h.server(echoConn))
	h.cls.set(41220, true, appGame)
	h.cls.set(41221, true, appGame)
	h.setRules(appGame)
	c, _ := h.mustDial(41220, dst(remoteA, 9220))
	roundTrip(t, c, "bypassed")
	if err := h.c.Close(); err != nil {
		t.Fatal(err)
	}
	expectReset(t, c)
	if !h.cls.closed.Load() || !h.eg.closed.Load() {
		t.Fatal("Controller.Close did not release the classifier and egress")
	}
	c, _ = h.mustDial(41221, dst(remoteA, 9221))
	roundTrip(t, c, "device keeps working tunnel-only")
	c.Close()
	if !h.tunnelHasPort(41221) {
		t.Fatal("flow after Controller.Close did not use the tunnel")
	}
	if errs := h.c.SetRules([]string{appGame}, nil); len(errs) != 0 {
		t.Fatalf("SetRules after Close = %v", errs)
	}
}

func TestWrapTUNWithoutIPv4StaysUnwrapped(t *testing.T) {
	c := NewController(ControllerOptions{})
	defer c.Close()
	inner := newFakeTUN(kindLinux)
	got := c.WrapTUN(inner, wg.TunnelInfo{Addresses: []netip.Addr{netip.MustParseAddr("fd00::2")}})
	if any(got) != any(inner) {
		t.Fatal("a tunnel without IPv4 must not be wrapped")
	}
}

func TestSetRulesReportsErrorsByIndexOnly(t *testing.T) {
	logs := &logCapture{}
	c := NewController(ControllerOptions{Logf: logs.logf})
	defer c.Close()
	c.compile = fakeCompile
	errs := c.SetRules([]string{appGame, "relative/secret-app", appChat, "/pangea/daemon"}, []string{"/pangea/daemon"})
	want := []procmatch.RuleError{{Index: 1, Code: procmatch.CodeNotAbsolute}, {Index: 3, Code: procmatch.CodeOwnImage}}
	if fmt.Sprint(errs) != fmt.Sprint(want) {
		t.Fatalf("errors = %v, want %v", errs, want)
	}
	for _, l := range logs.all() {
		if strings.Contains(l, "secret-app") || strings.Contains(l, "/apps/") || strings.Contains(l, "/pangea") {
			t.Fatalf("log leaks a rule path: %q", l)
		}
	}
}

func TestCompileRuleSetWithProcmatch(t *testing.T) {
	var app, exe string
	switch runtime.GOOS {
	case "windows":
		app, exe = `C:\Games\Fake Game\game.exe`, `c:\games\fake game\game.exe`
	case "darwin":
		app, exe = "/Applications/Fake Game.app", "/Applications/Fake Game.app/Contents/MacOS/game"
	default:
		app, exe = "/opt/fakegame/game", "/opt/fakegame/game"
	}
	rs, errs := compileRuleSet([]string{app}, nil)
	if len(errs) != 0 || rs.empty() {
		t.Fatalf("compile %q: errs=%v empty=%v", app, errs, rs.empty())
	}
	if !rs.allows([]string{"/somewhere/else", exe}) || rs.allows([]string{"/somewhere/else"}) || rs.allows(nil) {
		t.Fatal("compiled rules match the wrong chains")
	}
	again, _ := compileRuleSet([]string{app, app}, nil)
	if !rs.sameAs(again) {
		t.Fatal("identical rules compare unequal")
	}
	empty, _ := compileRuleSet(nil, nil)
	if !empty.empty() || rs.sameAs(empty) || !empty.sameAs(nil) {
		t.Fatal("empty rule set comparisons are wrong")
	}
	if _, errs := compileRuleSet([]string{app}, []string{app}); len(errs) != 1 || errs[0].Code != procmatch.CodeOwnImage {
		t.Fatalf("protected image accepted as a rule: %v", errs)
	}
}

func TestLogsCarryNoFlowData(t *testing.T) {
	h := newHarness(t, harnessOpts{kind: kindWindows, peer: true, lim: func(l *limits) { l.pendingTimeout = 200 * time.Millisecond }})
	h.eg.setTarget(h.server(echoConn))
	h.setRules(appGame, appChat)

	ports := []uint16{}
	for i := uint16(0); i < 30; i++ {
		p := 42000 + i
		ports = append(ports, p)
		switch i % 3 {
		case 0:
			h.cls.set(p, true, appGame)
		case 1:
			h.cls.set(p, false, "/apps/other/other")
		}
		c, _ := h.mustDial(p, dst(remoteA, 9300+i))
		roundTrip(t, c, "x")
		c.Close()
	}
	h.cls.fail.Store(true)
	for i := uint16(30); i < 40; i++ {
		c, _ := h.mustDial(42000+i, dst(remoteB, 9300+i))
		c.Close()
	}
	h.cls.fail.Store(false)
	release := h.cls.block()
	for i := uint16(40); i < 45; i++ {
		h.cls.set(42000+i, true, appChat)
		c, _ := h.mustDial(42000+i, dst(remoteB, 9300+i))
		c.Close()
	}
	release()
	h.eg.setTarget("")
	for i := uint16(45); i < 50; i++ {
		h.cls.set(42000+i, true, appChat)
		_, _, _ = h.dial(42000+i, dst(remoteA, 9300+i), 3*time.Second)
	}
	h.setRules(appChat)
	h.close()

	lines := h.logs.all()
	if len(lines) > 15 {
		t.Fatalf("%d log lines for 50 flows; logging must not scale with traffic:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	forbidden := []string{tunAddr.String(), remoteA.String(), remoteB.String(), "127.0.0.1", "/apps/", "game", "chat"}
	for p := uint16(9300); p < 9350; p++ {
		forbidden = append(forbidden, strconv.Itoa(int(p)))
	}
	for _, p := range ports {
		forbidden = append(forbidden, strconv.Itoa(int(p)))
	}
	for _, l := range lines {
		for _, f := range forbidden {
			if strings.Contains(l, f) {
				t.Errorf("log line %q leaks %q", l, f)
			}
		}
	}
	t.Logf("captured log:\n%s", strings.Join(lines, "\n"))
}

func TestFailedPermitIsWithdrawnWhenNoLongerWanted(t *testing.T) {
	rec := &permitRecorder{err: errors.New("filter add failed")}
	c := NewController(ControllerOptions{SetEgressPermit: rec.set})
	c.compile = fakeCompile
	dev := c.WrapTUN(newFakeTUN(kindWindows), testTunnelInfo())
	t.Cleanup(func() {
		_ = dev.Close()
		_ = c.Close()
	})
	c.SetRules([]string{appGame}, nil)
	// The worker may see both the device and the rules kick after the rules are on: one or two grants.
	waitFor(t, 2*time.Second, "failed grant", func() bool { return len(rec.seen()) >= 1 && c.Status().UnavailableReason == ReasonPermitFailed })
	rec.mu.Lock()
	rec.err = nil
	grants := len(rec.calls)
	rec.mu.Unlock()
	c.SetRules(nil, nil)
	waitFor(t, 2*time.Second, "withdrawal", func() bool { return len(rec.seen()) == grants+1 })
	got := rec.seen()
	for _, on := range got[:grants] {
		if !on {
			t.Fatalf("permit calls = %v", got)
		}
	}
	if got[grants] || c.permitted.Load() {
		t.Fatalf("permit calls = %v", got)
	}
	if st := c.Status(); st.UnavailableReason != "" {
		t.Fatalf("reason after withdrawal = %q", st.UnavailableReason)
	}
	_ = dev.Close()
	_ = c.Close()
	if n := len(rec.seen()); n != grants+1 {
		t.Fatalf("Close re-sent a withdrawal (%d calls)", n)
	}
	checkNoEngineGoroutines(t)
}

func TestClassifierObservesTheLatestRulesAfterRacingEdits(t *testing.T) {
	for round := 0; round < 20; round++ {
		release := make(chan struct{})
		h := newHarness(t, harnessOpts{kind: kindLinux})
		h.c.compile = func(apps, never []string) (*ruleSet, []procmatch.RuleError) {
			rs, errs := fakeCompile(apps, never)
			rs.rules = &procmatch.Rules{}
			return rs, errs
		}
		orig := h.c.opts.NewClassifier
		h.c.opts.NewClassifier = func() (procmatch.Classifier, error) {
			<-release
			return orig()
		}
		h.setRules(appGame)
		h.tun.fromOS(tunnelUDP(40600, []byte("kick")))
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				if i == 4 {
					close(release)
				}
				if i%2 == 0 {
					h.c.SetRules(nil, nil)
				} else {
					h.c.SetRules([]string{appGame, fmt.Sprintf("/apps/%d", i)}, nil)
				}
			}(i)
		}
		wg.Wait()
		waitFor(t, 2*time.Second, "engines started", func() bool { return h.cls.observed.Load() != nil || h.c.Status().UnavailableReason != "" })
		h.c.mu.Lock()
		cur := h.c.rules
		h.c.mu.Unlock()
		if got := h.cls.observed.Load(); cur != nil && got != cur.rules {
			t.Fatalf("round %d: classifier observed stale rules", round)
		}
		h.close()
	}
}
