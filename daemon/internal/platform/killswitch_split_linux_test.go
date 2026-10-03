//go:build linux

package platform

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

type linuxRender struct {
	backend string
	rules   ksRules
}

type linuxSplitHarness struct {
	t         *testing.T
	ks        *linuxKillSwitch
	renders   []linuxRender
	nft       bool
	failNFT   int
	failIPT   int
	removeErr error
}

func newLinuxSplitHarness(t *testing.T, nft bool) *linuxSplitHarness {
	t.Helper()
	isolateStateDir(t)
	h := &linuxSplitHarness{t: t, ks: &linuxKillSwitch{}, nft: nft}

	prevAvail, prevNFT, prevNFTRemove, prevIPT, prevIPTRemove := nftAvailable, nftApply, nftRemove, iptApply, iptRemove
	t.Cleanup(func() {
		nftAvailable, nftApply, nftRemove, iptApply, iptRemove = prevAvail, prevNFT, prevNFTRemove, prevIPT, prevIPTRemove
	})
	record := func(backend string, r ksRules, fail *int) error {
		r.EndpointIPs = slices.Clone(r.EndpointIPs)
		r.Split = r.Split.clone()
		h.renders = append(h.renders, linuxRender{backend: backend, rules: r})
		if *fail > 0 {
			*fail--
			return errors.New(backend + ": rejected")
		}
		return nil
	}
	nftAvailable = func(context.Context) bool { return h.nft }
	nftApply = func(_ context.Context, r ksRules) error { return record("nft", r, &h.failNFT) }
	iptApply = func(_ context.Context, r ksRules) error { return record("iptables", r, &h.failIPT) }
	nftRemove = func(context.Context) error { return h.removeErr }
	iptRemove = func(context.Context) error { return nil }
	return h
}

func (h *linuxSplitHarness) last() linuxRender {
	h.t.Helper()
	if len(h.renders) == 0 {
		h.t.Fatal("nothing rendered")
	}
	return h.renders[len(h.renders)-1]
}

func (h *linuxSplitHarness) assertRendersSince(from int, want splitPermits) {
	h.t.Helper()
	if from >= len(h.renders) {
		h.t.Fatalf("no render since #%d", from)
	}
	for i, r := range h.renders[from:] {
		if !r.rules.Split.equal(want) {
			h.t.Fatalf("render #%d (%s) carries split %+v, want %+v", from+i, r.backend, r.rules.Split, want)
		}
	}
}

// DL-2: every path that rewrites the ruleset (Update, DropTunnelPermit, a merged
// re-arm, the iptables fallback) must carry the split permits.
func TestLinuxSplitPermits_EveryRenderCarriesTheSplitSet(t *testing.T) {
	for _, backend := range []string{"nft", "iptables"} {
		t.Run(backend, func(t *testing.T) {
			h := newLinuxSplitHarness(t, backend == "nft")
			ctx := t.Context()
			if err := h.ks.SetSplitCIDRs(ctx, []string{"198.51.100.0/24"}); err != nil {
				t.Fatal(err)
			}
			if err := h.ks.SetSplitEgress(ctx, true); err != nil {
				t.Fatal(err)
			}
			if len(h.renders) != 0 {
				t.Fatalf("an idle switch rendered %d times", len(h.renders))
			}
			want := splitPermits{Egress: true, CIDRs: []string{"198.51.100.0/24"}}

			if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
				t.Fatalf("Enable: %v", err)
			}
			if h.last().backend != backend {
				t.Fatalf("rendered with %s, want %s", h.last().backend, backend)
			}
			if err := h.ks.Update(ctx, TunnelRef{Name: "wg0"}); err != nil {
				t.Fatalf("Update: %v", err)
			}
			if h.last().rules.Tunnel != "wg0" {
				t.Fatalf("Update rendered tunnel %q", h.last().rules.Tunnel)
			}
			if err := h.ks.DropTunnelPermit(ctx); err != nil {
				t.Fatalf("DropTunnelPermit: %v", err)
			}
			if h.last().rules.Tunnel != "" {
				t.Fatalf("DropTunnelPermit rendered tunnel %q", h.last().rules.Tunnel)
			}
			if err := h.ks.Enable(ctx, []string{"203.0.113.5", "203.0.113.6"}, true, false); err != nil {
				t.Fatalf("Enable (merged): %v", err)
			}
			if r := h.last(); !slices.Equal(r.rules.EndpointIPs, []string{"203.0.113.5", "203.0.113.6"}) || !r.rules.AllowLAN {
				t.Fatalf("merged Enable rendered %+v", r.rules)
			}
			if backend == "nft" {
				// A rejected nft script on a re-arm falls back to iptables, split and all.
				h.failNFT = 1
				if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, true, false); err != nil {
					t.Fatalf("Enable (iptables fallback): %v", err)
				}
				if h.last().backend != "iptables" {
					t.Fatalf("fallback rendered with %s", h.last().backend)
				}
			}
			h.failNFT, h.failIPT = 1, 1
			if err := h.ks.Enable(ctx, []string{"203.0.113.9"}, true, false); err == nil {
				t.Fatal("Enable succeeded with every backend failing")
			}
			if !h.ks.split.equal(want) {
				t.Fatalf("a failed re-arm changed the desired split to %+v", h.ks.split)
			}
			if err := h.ks.Enable(ctx, []string{"203.0.113.9"}, true, false); err != nil {
				t.Fatalf("Enable after the failure: %v", err)
			}
			h.assertRendersSince(0, want)
		})
	}
}

func TestLinuxSplitPermits_SettersRenderAnArmedLockAndNarrowOnFailure(t *testing.T) {
	h := newLinuxSplitHarness(t, true)
	ctx := t.Context()
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.Update(ctx, TunnelRef{Name: "wg0"}); err != nil {
		t.Fatal(err)
	}
	if err := h.ks.SetSplitCIDRs(ctx, []string{"198.51.100.0/24", "10.20.0.0/16"}); err != nil {
		t.Fatal(err)
	}
	r := h.last()
	if r.rules.Tunnel != "wg0" || !slices.Equal(r.rules.EndpointIPs, []string{"203.0.113.5"}) {
		t.Fatalf("a split change dropped the lock's other inputs: %+v", r.rules)
	}
	if !slices.Equal(r.rules.Split.CIDRs, []string{"10.20.0.0/16", "198.51.100.0/24"}) {
		t.Fatalf("rendered ranges %v", r.rules.Split.CIDRs)
	}

	h.failNFT = 1
	if err := h.ks.SetSplitCIDRs(ctx, []string{"10.20.0.0/16", "203.0.113.0/24"}); err == nil {
		t.Fatal("SetSplitCIDRs succeeded with nft failing")
	}
	if !h.ks.split.equal(splitPermits{CIDRs: []string{"10.20.0.0/16"}}) {
		t.Fatalf("after a failed change desired = %+v, want only what was both live and asked for", h.ks.split)
	}
	if err := h.ks.SetSplitEgress(ctx, true); err != nil {
		t.Fatal(err)
	}
	if got := h.last().rules.Split; !got.equal(splitPermits{Egress: true, CIDRs: []string{"10.20.0.0/16"}}) {
		t.Fatalf("rendered %+v", got)
	}
	if err := h.ks.SetSplitCIDRs(ctx, []string{"bogus"}); err == nil {
		t.Fatal("an invalid range was accepted")
	}
}

func TestLinuxSplitPermits_ClearForgetsAndNothingIsPersisted(t *testing.T) {
	h := newLinuxSplitHarness(t, true)
	ctx := t.Context()
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
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

	h.removeErr = errors.New("nft: busy")
	if err := h.ks.Clear(ctx); err == nil {
		t.Fatal("Clear succeeded with the removal failing")
	}
	if !h.ks.split.empty() {
		t.Fatalf("a failed Clear kept %+v desired", h.ks.split)
	}
	if err := h.ks.Enable(ctx, []string{"203.0.113.5"}, false, false); err != nil {
		t.Fatal(err)
	}
	if got := h.last().rules.Split; !got.empty() {
		t.Fatalf("re-arm after Clear rendered %+v", got)
	}
	h.removeErr = nil
	if err := h.ks.Clear(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestStrictReversePathFilterReadsTheHost(t *testing.T) {
	strict, err := StrictReversePathFilter()
	if err != nil {
		t.Fatalf("StrictReversePathFilter: %v", err)
	}
	t.Logf("strict reverse-path filtering: %v", strict)
}
