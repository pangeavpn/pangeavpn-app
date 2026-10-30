package mobile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func boolPtr(b bool) *bool { return &b }

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatal(err)
	}
	return parsed
}

func activeSub() hubSubscription {
	return hubSubscription{Status: "active", Entitled: boolPtr(true), Renews: true, ExpiresAt: strPtr("2027-01-01T00:00:00.000Z")}
}

func cachedSubAt(sub hubSubscription, at time.Time) cachedSubscription {
	return cachedSubscription{Subscription: sub, CachedAt: at.UnixMilli()}
}

func TestLiveEntitlementPrefersTheHub(t *testing.T) {
	sub := activeSub()
	sub.Entitled = boolPtr(false)
	if liveEntitlement(sub) {
		t.Fatal("entitled:false with status active was treated as entitled")
	}
	sub.Status = "canceled"
	sub.Entitled = boolPtr(true)
	if !liveEntitlement(sub) {
		t.Fatal("entitled:true was overridden by the status")
	}
}

func TestLiveEntitlementFallsBackToStatus(t *testing.T) {
	for status, want := range map[string]bool{
		"active": true, "trialing": true, "past_due": false, "canceled": false, "unpaid": false, "none": false,
	} {
		if got := liveEntitlement(hubSubscription{Status: status}); got != want {
			t.Errorf("status %s = %v, want %v", status, got, want)
		}
	}
}

func TestCachedEntitlementKeepsALiveEntitlementOffline(t *testing.T) {
	now := mustTime(t, "2026-06-01T00:00:00Z")
	if !cachedEntitlement(cachedSubAt(activeSub(), now), now) {
		t.Fatal("an unreachable hub revoked a live entitlement")
	}
	legacy := hubSubscription{Status: "active", Renews: true}
	if !cachedEntitlement(cachedSubAt(legacy, now), now) {
		t.Fatal("an old hub that omits entitled is trusted on status alone")
	}
}

func TestCachedEntitlementNeverGrantsWhatTheHubRefused(t *testing.T) {
	now := mustTime(t, "2026-06-01T00:00:00Z")
	denied := activeSub()
	denied.Entitled, denied.ExpiresAt = boolPtr(false), nil
	lapsed := activeSub()
	lapsed.ExpiresAt = strPtr("2026-05-01T00:00:00.000Z")
	cases := map[string]hubSubscription{
		"hub said no":        denied,
		"expiry long past":   lapsed,
		"no plan":            {Status: "none"},
		"canceled, no field": {Status: "canceled"},
		"unpaid, no field":   {Status: "unpaid"},
		"past_due, no field": {Status: "past_due"},
	}
	for name, sub := range cases {
		if cachedEntitlement(cachedSubAt(sub, now), now) {
			t.Errorf("%s: cached entitlement granted", name)
		}
	}
}

func TestCachedEntitlementExpiresAfterFourteenDaysOffline(t *testing.T) {
	at := mustTime(t, "2026-01-01T00:00:00Z")
	c := cachedSubAt(activeSub(), at)
	if !cachedEntitlement(c, at.Add(14*24*time.Hour)) {
		t.Fatal("revoked at exactly fourteen days")
	}
	if cachedEntitlement(c, at.Add(14*24*time.Hour+time.Millisecond)) {
		t.Fatal("still entitled past fourteen days offline")
	}
	if cachedEntitlement(c, at.Add(30*24*time.Hour)) {
		t.Fatal("still entitled thirty days offline")
	}
}

func TestCachedEntitlementGivesARenewingPlanThreeDays(t *testing.T) {
	expiry := mustTime(t, "2026-06-01T00:00:00Z")
	sub := activeSub()
	sub.ExpiresAt = strPtr("2026-06-01T00:00:00.000Z")
	c := cachedSubAt(sub, expiry)
	if !cachedEntitlement(c, expiry.Add(24*time.Hour)) {
		t.Fatal("a renewing plan lost entitlement a day after a missed renewal")
	}
	if !cachedEntitlement(c, expiry.Add(3*24*time.Hour)) {
		t.Fatal("a renewing plan lost entitlement at the grace deadline")
	}
	if cachedEntitlement(c, expiry.Add(10*24*time.Hour)) {
		t.Fatal("a renewing plan kept entitlement ten days past expiry")
	}
}

func TestCachedEntitlementCutsANonRenewingPlanAtExpiry(t *testing.T) {
	expiry := mustTime(t, "2026-06-01T00:00:00Z")
	sub := activeSub()
	sub.Renews = false
	sub.ExpiresAt = strPtr("2026-06-01T00:00:00.000Z")
	c := cachedSubAt(sub, expiry.Add(-time.Second))
	if cachedEntitlement(c, expiry.Add(time.Second)) {
		t.Fatal("a non-renewing plan kept entitlement past expiry")
	}
	if !cachedEntitlement(c, expiry.Add(-time.Millisecond)) {
		t.Fatal("a non-renewing plan lost entitlement before expiry")
	}
}

func TestCachedSubscriptionRoundTrips(t *testing.T) {
	now := mustTime(t, "2026-06-01T00:00:00Z")
	raw := encodeCachedSubscription(activeSub(), "owner-1", now)
	got, ok := restoreCachedSubscription(raw, "owner-1", now)
	if !ok {
		t.Fatalf("restore of %s failed", raw)
	}
	if got.CachedAt != now.UnixMilli() || got.Subscription.Status != "active" || !got.Subscription.Renews ||
		got.Subscription.Entitled == nil || !*got.Subscription.Entitled || *got.Subscription.ExpiresAt != "2027-01-01T00:00:00.000Z" {
		t.Fatalf("restored %+v", got)
	}

	legacy := hubSubscription{Status: "none", Renews: false}
	got, ok = restoreCachedSubscription(encodeCachedSubscription(legacy, "owner-1", now), "owner-1", now)
	if !ok || got.Subscription.Entitled != nil || got.Subscription.ExpiresAt != nil {
		t.Fatalf("an absent entitled or null expiry did not survive: %+v, %v", got, ok)
	}
}

func TestRestoreCachedSubscriptionRejectsFutureAndMalformed(t *testing.T) {
	now := mustTime(t, "2026-06-01T00:00:00Z")
	ms := now.UnixMilli()
	blob := func(sub string, at int64) string {
		return `{"owner":"o","cachedAt":` + jsonInt(at) + `,"subscription":` + sub + `}`
	}
	good := `{"status":"active","entitled":true,"renews":true,"expiresAt":null}`
	if _, ok := restoreCachedSubscription(blob(good, ms), "o", now); !ok {
		t.Fatal("a well-formed blob was rejected")
	}
	cases := map[string]string{
		"empty":                 "",
		"not json":              "{nope",
		"array":                 "[]",
		"unknown status":        blob(`{"status":"lapsed","renews":true,"expiresAt":null}`, ms),
		"non-bool renews":       blob(`{"status":"active","renews":"yes","expiresAt":null}`, ms),
		"missing renews":        blob(`{"status":"active","expiresAt":null}`, ms),
		"numeric expiry":        blob(`{"status":"active","renews":true,"expiresAt":12345}`, ms),
		"missing expiry":        blob(`{"status":"active","renews":true}`, ms),
		"string entitled":       blob(`{"status":"active","renews":true,"expiresAt":null,"entitled":"true"}`, ms),
		"no subscription":       `{"owner":"o","cachedAt":` + jsonInt(ms) + `}`,
		"no cachedAt":           `{"owner":"o","subscription":` + good + `}`,
		"string cachedAt":       `{"owner":"o","cachedAt":"1","subscription":` + good + `}`,
		"more than a day ahead": blob(good, now.Add(5*24*time.Hour).UnixMilli()),
		"another session":       strings.Replace(blob(good, ms), `"owner":"o"`, `"owner":"x"`, 1),
	}
	for name, raw := range cases {
		if _, ok := restoreCachedSubscription(raw, "o", now); ok {
			t.Errorf("%s: restored %s", name, raw)
		}
	}
	if _, ok := restoreCachedSubscription(blob(good, now.Add(23*time.Hour).UnixMilli()), "o", now); !ok {
		t.Fatal("a cache less than a day ahead was rejected; desktop allows the skew")
	}
}

func jsonInt(v int64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSubscriptionJSONCarriesEntitledAndCached(t *testing.T) {
	now := mustTime(t, "2026-06-01T00:00:00Z")
	live, _ := json.Marshal(liveSubscriptionInfo(activeSub()))
	if string(live) != `{"status":"active","renews":true,"expiresAt":"2027-01-01T00:00:00.000Z","entitled":true,"cached":false}` {
		t.Fatalf("live = %s", live)
	}
	stale := activeSub()
	stale.Entitled = nil
	cached, _ := json.Marshal(cachedSubscriptionInfo(cachedSubAt(stale, now), now))
	if string(cached) != `{"status":"active","renews":true,"expiresAt":"2027-01-01T00:00:00.000Z","entitled":true,"cached":true}` {
		t.Fatalf("cached = %s", cached)
	}
}

func TestCacheOwnerBindsAccountAndDevice(t *testing.T) {
	base := cacheOwner("lk", "pub")
	if base == "" || base != cacheOwner("lk", "pub") {
		t.Fatal("cacheOwner is not stable")
	}
	for _, other := range []string{cacheOwner("lk2", "pub"), cacheOwner("lk", "pub2"), cacheOwner("lkp", "ub")} {
		if other == base {
			t.Fatal("two sessions share a cache owner")
		}
	}
	if strings.Contains(base, "lk") {
		t.Fatal("the owner tag carries the license in clear")
	}
}
