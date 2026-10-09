package api

import (
	"fmt"
	"testing"
	"time"
)

func testBaseline() (*reachBaseline, *time.Time) {
	clock := time.Date(2026, 9, 29, 20, 0, 0, 0, time.UTC)
	b := newReachBaseline()
	b.now = func() time.Time { return clock }
	return b, &clock
}

func TestReachBaseline_ProvenOnlyWhereRecorded(t *testing.T) {
	b, _ := testBaseline()
	b.record("wifi", "direct:203.0.113.7")
	if !b.proven("wifi", "direct:203.0.113.7") {
		t.Fatal("recorded route not proven")
	}
	if b.proven("wifi", "reality:198.51.100.9") {
		t.Error("an unrecorded route on the same network counted as proven")
	}
	if b.proven("ethernet", "direct:203.0.113.7") {
		t.Error("a route proven on one network counted on another")
	}
}

func TestReachBaseline_ProofExpiresAfterTheTTL(t *testing.T) {
	b, clock := testBaseline()
	b.record("wifi", "direct:203.0.113.7")
	*clock = clock.Add(reachBaselineTTL - time.Minute)
	if !b.proven("wifi", "direct:203.0.113.7") {
		t.Fatal("proof expired early")
	}
	*clock = clock.Add(2 * time.Minute)
	if b.proven("wifi", "direct:203.0.113.7") {
		t.Fatal("proof outlived its TTL")
	}
}

func TestReachBaseline_FreshWindowIsShorterThanTheTTL(t *testing.T) {
	b, clock := testBaseline()
	if b.fresh("wifi") {
		t.Fatal("an unseen network is fresh")
	}
	b.record("wifi", "direct:203.0.113.7")
	*clock = clock.Add(reachBaselineRefresh + time.Minute)
	if b.fresh("wifi") {
		t.Error("fresh past the refresh window, so a connect would never re-prove it")
	}
	if !b.proven("wifi", "direct:203.0.113.7") {
		t.Error("still inside the TTL, so it must stay proven")
	}
}

func TestReachBaseline_ForgetAndEmptyKeys(t *testing.T) {
	b, _ := testBaseline()
	b.record("wifi", "direct:203.0.113.7")
	b.forget("wifi")
	if b.proven("wifi", "direct:203.0.113.7") {
		t.Error("forget left the proof behind")
	}
	b.record("", "direct:203.0.113.7")
	b.record("wifi", "")
	if b.fresh("") || b.fresh("wifi") {
		t.Error("an empty network or route key was recorded")
	}
}

func TestReachBaseline_EvictsTheStalestNetworkAtTheCap(t *testing.T) {
	b, clock := testBaseline()
	for i := range reachBaselineMaxNetworks {
		b.record(fmt.Sprintf("net-%d", i), "direct:203.0.113.7")
		*clock = clock.Add(time.Second)
	}
	b.record("newcomer", "direct:203.0.113.7")
	if b.proven("net-0", "direct:203.0.113.7") {
		t.Error("the stalest network survived the cap")
	}
	if !b.proven("net-1", "direct:203.0.113.7") || !b.proven("newcomer", "direct:203.0.113.7") {
		t.Error("eviction took more than the stalest network")
	}
}

func TestReachBaseline_UnproveDropsOnlyThatRoute(t *testing.T) {
	b, _ := testBaseline()
	b.record("wifi", "direct:203.0.113.7")
	b.record("wifi", "reality:198.51.100.9")
	b.unprove("wifi", "direct:203.0.113.7")
	if b.proven("wifi", "direct:203.0.113.7") {
		t.Error("unproved route still counts")
	}
	if !b.proven("wifi", "reality:198.51.100.9") {
		t.Error("unprove took a sibling route with it")
	}
	b.unprove("wifi", "reality:198.51.100.9")
	b.unprove("ethernet", "direct:203.0.113.7")
	if b.fresh("wifi") {
		t.Error("a network with no proven routes left is still fresh")
	}
}
