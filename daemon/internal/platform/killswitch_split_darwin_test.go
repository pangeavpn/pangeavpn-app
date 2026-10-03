//go:build darwin

package platform

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

type pfRender struct {
	rules ksRules
	gid   int
}

type darwinSplitHarness struct {
	t           *testing.T
	ks          *darwinKillSwitch
	renders     []pfRender
	killed      []string
	killTunnels []string
	failNext    int
	gid         int
	gidErr      error
}

func newDarwinSplitHarness(t *testing.T) *darwinSplitHarness {
	t.Helper()
	isolateStateDir(t)
	t.Setenv("PANGEA_APP_SUPPORT_DIR", t.TempDir())
	h := &darwinSplitHarness{t: t, ks: &darwinKillSwitch{}, gid: 437}

	prevApply, prevEnable, prevIsEnabled, prevVerify := pfApply, pfEnable, pfIsEnabled, pfVerifyLive
	prevFlush, prevKill, prevDisable, prevRemove, prevGID := pfFlushStates, pfKillStates, pfDisable, pfRemoveAnchor, splitEgressGID
	t.Cleanup(func() {
		pfApply, pfEnable, pfIsEnabled, pfVerifyLive = prevApply, prevEnable, prevIsEnabled, prevVerify
		pfFlushStates, pfKillStates, pfDisable, pfRemoveAnchor, splitEgressGID = prevFlush, prevKill, prevDisable, prevRemove, prevGID
	})
	pfApply = func(_ context.Context, r ksRules, gid int) error {
		r.EndpointIPs = slices.Clone(r.EndpointIPs)
		r.Split = r.Split.clone()
		h.renders = append(h.renders, pfRender{rules: r, gid: gid})
		if h.failNext > 0 {
			h.failNext--
			return errors.New("pfctl: syntax error")
		}
		return nil
	}
	pfEnable = func(context.Context) (string, error) { return "4242", nil }
	pfIsEnabled = func(context.Context) bool { return true }
	pfVerifyLive = func(context.Context) error { return nil }
	pfFlushStates = func(context.Context) {}
	pfKillStates = func(_ context.Context, cidrs []string, tunnel string) {
		h.killed = append(h.killed, cidrs...)
		h.killTunnels = append(h.killTunnels, tunnel)
	}
	pfDisable = func(context.Context, string) error { return nil }
	pfRemoveAnchor = func(context.Context) error { return nil }
	splitEgressGID = func() (int, error) { return h.gid, h.gidErr }
	return h
}

func (h *darwinSplitHarness) last() pfRender {
	h.t.Helper()
	if len(h.renders) == 0 {
		h.t.Fatal("nothing rendered")
	}
	return h.renders[len(h.renders)-1]
}

func (h *darwinSplitHarness) assertRendersSince(from int, want splitPermits) {
	h.t.Helper()
	if from >= len(h.renders) {
		h.t.Fatalf("no render since #%d", from)
	}
	for i, r := range h.renders[from:] {
		if !r.rules.Split.equal(want) {
			h.t.Fatalf("render #%d carries split %+v, want %+v", from+i, r.rules.Split, want)
		}
		wantGID := 0
		if want.Egress {
			wantGID = 437
		}
		if r.gid != wantGID {
			h.t.Fatalf("render #%d has group %d, want %d", from+i, r.gid, wantGID)
		}
	}
}

// DL-2: Update, DropTunnelPermit, a merged re-arm and a rollback all rewrite the
// whole anchor, so each must carry the split permits or silently drop them.
func TestDarwinSplitPermits_EveryRenderCarriesTheSplitSet(t *testing.T) {
	h := newDarwinSplitHarness(t)
	ctx := t.Context()
	if err := h.ks.SetSplitCIDRs(ctx, []string{"198.51.100.0/24"}); err != nil {
		t.Fatal(err)
	}
	if len(h.renders) != 0 {
		t.Fatalf("an idle switch rendered %d times", len(h.renders))
	}
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	h.assertRendersSince(0, splitPermits{CIDRs: []string{"198.51.100.0/24"}})

	if err := h.ks.SetSplitEgress(ctx, true); err != nil {
		t.Fatalf("SetSplitEgress: %v", err)
	}
	want := splitPermits{Egress: true, CIDRs: []string{"198.51.100.0/24"}}
	from := len(h.renders) - 1
	if err := h.ks.Update(ctx, TunnelRef{Name: "utun7"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if h.last().rules.Tunnel != "utun7" {
		t.Fatalf("Update rendered tunnel %q", h.last().rules.Tunnel)
	}
	if err := h.ks.DropTunnelPermit(ctx); err != nil {
		t.Fatalf("DropTunnelPermit: %v", err)
	}
	if h.last().rules.Tunnel != "" {
		t.Fatalf("DropTunnelPermit rendered tunnel %q", h.last().rules.Tunnel)
	}
	if err := h.ks.Enable(ctx, []string{"203.0.113.5", "203.0.113.6"}, false, false); err != nil {
		t.Fatalf("Enable (merged): %v", err)
	}
	if got := h.last().rules.EndpointIPs; !slices.Equal(got, []string{"203.0.113.5", "203.0.113.6"}) {
		t.Fatalf("merged Enable rendered endpoints %v", got)
	}

	h.failNext = 1
	before := len(h.renders)
	if err := h.ks.Enable(ctx, []string{"203.0.113.9"}, false, false); err == nil {
		t.Fatal("Enable succeeded with pfctl failing")
	}
	if len(h.renders) != before+2 {
		t.Fatalf("failed Enable rendered %d times, want the attempt and its rollback", len(h.renders)-before)
	}
	if got := h.last().rules.EndpointIPs; !slices.Equal(got, []string{"203.0.113.5", "203.0.113.6"}) {
		t.Fatalf("rollback rendered endpoints %v, want the previous set", got)
	}
	h.assertRendersSince(from, want)
}

func TestDarwinSplitPermits_UnchangedReArmIsSkippedOnlyWhenTheAnchorMatches(t *testing.T) {
	h := newDarwinSplitHarness(t)
	ctx := t.Context()
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.SetSplitCIDRs(ctx, []string{"198.51.100.0/24"}); err != nil {
		t.Fatal(err)
	}
	before := len(h.renders)
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if len(h.renders) != before {
		t.Fatal("an unchanged re-arm re-rendered a matching anchor")
	}

	// KS-12: a failed narrowing must be retried by the next arm.
	h.failNext = 1
	if err := h.ks.SetSplitCIDRs(ctx, nil); err == nil {
		t.Fatal("SetSplitCIDRs(nil) succeeded with pfctl failing")
	}
	if !h.ks.split.empty() {
		t.Fatalf("a failed removal kept %+v desired", h.ks.split)
	}
	h.killed = nil
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if !h.last().rules.Split.empty() {
		t.Fatalf("re-arm still renders %+v", h.last().rules.Split)
	}
	if !slices.Equal(h.killed, []string{"198.51.100.0/24"}) {
		t.Fatalf("killed states for %v, want the range the retry removed", h.killed)
	}
}

// KS-9: pf keeps established states across a reload, so a removed range's
// flows must be ended explicitly.
func TestDarwinSplitPermits_RemovedRangesLoseTheirStates(t *testing.T) {
	h := newDarwinSplitHarness(t)
	ctx := t.Context()
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.SetSplitCIDRs(ctx, []string{"198.51.100.0/24", "10.20.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	if len(h.killed) != 0 {
		t.Fatalf("adding ranges killed states for %v", h.killed)
	}
	if err := h.ks.SetSplitCIDRs(ctx, []string{"10.20.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.Update(ctx, TunnelRef{Name: "utun7"}); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.SetSplitCIDRs(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(h.killed, []string{"198.51.100.0/24", "10.20.0.0/16"}) {
		t.Fatalf("killed states for %v, want each removed range once", h.killed)
	}
}

// KS-R2-2: only what the new anchor permits in no form loses its states, and the
// tunnel's own flows are spared.
func TestDarwinSplitPermits_KillsOnlyWhatIsNoLongerPermitted(t *testing.T) {
	h := newDarwinSplitHarness(t)
	ctx := t.Context()
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.Update(ctx, TunnelRef{Name: "utun7"}); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][]string{{"10.1.0.0/16"}, {"10.0.0.0/8"}} {
		if err := h.ks.SetSplitCIDRs(ctx, step); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.killed) != 0 {
		t.Fatalf("widening 10.1.0.0/16 to 10.0.0.0/8 killed %v", h.killed)
	}
	if err := h.ks.SetSplitCIDRs(ctx, []string{"10.1.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	if len(h.killed) != 8 || slices.Contains(h.killed, "10.1.0.0/16") || !slices.Equal(h.killTunnels, []string{"utun7"}) {
		t.Fatalf("narrowing to 10.1.0.0/16 killed %v (tunnels %v), want the 8 leftover pieces sparing utun7", h.killed, h.killTunnels)
	}

	h.killed = nil
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, true, false); err != nil {
		t.Fatal(err)
	}
	for _, step := range [][]string{{"192.168.1.0/24"}, nil} {
		if err := h.ks.SetSplitCIDRs(ctx, step); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.killed) != 0 {
		t.Fatalf("killed %v, though Allow LAN still permits every removed range", h.killed)
	}
}

func TestDarwinSplitPermits_EgressNeedsTheGroup(t *testing.T) {
	h := newDarwinSplitHarness(t)
	ctx := t.Context()
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	h.gidErr = errors.New("group _pangeasplit not found")
	before := len(h.renders)
	if err := h.ks.SetSplitEgress(ctx, true); err == nil {
		t.Fatal("SetSplitEgress(true) succeeded without the group")
	}
	if len(h.renders) != before || h.ks.split.Egress {
		t.Fatalf("a refused egress permit rendered %d times, desired %+v", len(h.renders)-before, h.ks.split)
	}

	h.gidErr = nil
	if err := h.ks.SetSplitEgress(ctx, true); err != nil {
		t.Fatal(err)
	}
	if r := h.last(); !r.rules.Split.Egress || r.gid != 437 {
		t.Fatalf("egress render = %+v", r)
	}
	h.gidErr = errors.New("group _pangeasplit has members")
	if err := h.ks.SetSplitEgress(ctx, true); err == nil {
		t.Fatal("re-asserting egress succeeded with the group no longer safe")
	}
	if r := h.last(); r.rules.Split.Egress || r.gid != 0 {
		t.Fatalf("an unsafe group kept its pass: %+v", r)
	}
}

func TestDarwinSplitPermits_ClearForgetsAndNothingIsPersisted(t *testing.T) {
	h := newDarwinSplitHarness(t)
	ctx := t.Context()
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, true, false); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.SetSplitCIDRs(ctx, []string{"198.51.100.0/24"}); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.SetSplitEgress(ctx, true); err != nil {
		t.Fatal(err)
	}
	path, err := killSwitchStatePath()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "198.51.100.0") || strings.Contains(strings.ToLower(string(data)), "split") {
		t.Fatalf("split permits reached the state file a fresh process re-arms from:\n%s", data)
	}

	if err := h.ks.Clear(ctx); err != nil {
		t.Fatal(err)
	}
	if !h.ks.split.empty() {
		t.Fatalf("Clear kept %+v desired", h.ks.split)
	}
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if r := h.last(); !r.rules.Split.empty() || r.gid != 0 {
		t.Fatalf("Enable after Clear rendered %+v", r)
	}

	fresh := &darwinKillSwitch{}
	if !fresh.split.empty() {
		t.Fatal("a fresh switch starts with split permits")
	}
}
