import assert from "node:assert/strict";
import test from "node:test";
import {
  cachedEntitlement,
  isSubscriptionInfo,
  restoreCachedSubscription
} from "./cachedSubscription.ts";

const SUB = {
  status: "active" as const,
  entitled: true,
  renews: true,
  expiresAt: "2027-01-01T00:00:00.000Z"
};

test("accepts a well-formed subscription", () => {
  assert.equal(isSubscriptionInfo(SUB), true);
  assert.equal(isSubscriptionInfo({ ...SUB, entitled: undefined }), true);
  assert.equal(isSubscriptionInfo({ ...SUB, expiresAt: null }), true);
});

test("rejects malformed subscriptions", () => {
  assert.equal(isSubscriptionInfo(null), false);
  assert.equal(isSubscriptionInfo([SUB]), false);
  assert.equal(isSubscriptionInfo({ ...SUB, status: "lapsed" }), false);
  assert.equal(isSubscriptionInfo({ ...SUB, renews: "yes" }), false);
  assert.equal(isSubscriptionInfo({ ...SUB, expiresAt: 12345 }), false);
  assert.equal(isSubscriptionInfo({ ...SUB, entitled: "true" }), false);
});

test("restores a stored blob", () => {
  const stored = { subscription: SUB, cachedAt: 1_700_000_000_000 };
  assert.deepEqual(restoreCachedSubscription(stored), stored);
});

test("rejects blobs that are unusable or from the future", () => {
  assert.equal(restoreCachedSubscription(undefined), null);
  assert.equal(restoreCachedSubscription({ subscription: SUB }), null);
  assert.equal(restoreCachedSubscription({ cachedAt: 1 }), null);
  assert.equal(restoreCachedSubscription({ subscription: { status: "x" }, cachedAt: 1 }), null);
  assert.equal(
    restoreCachedSubscription({ subscription: SUB, cachedAt: Date.now() + 5 * 86_400_000 }),
    null
  );
});

test("an unreachable hub does not revoke a live entitlement", () => {
  const now = Date.parse("2026-06-01T00:00:00.000Z");
  assert.equal(cachedEntitlement({ subscription: SUB, cachedAt: now }, now), true);
});

test("an expiry already past revokes it, whatever the stored status says", () => {
  const now = Date.parse("2026-06-01T00:00:00.000Z");
  const lapsed = { ...SUB, expiresAt: "2026-05-01T00:00:00.000Z" };
  assert.equal(cachedEntitlement({ subscription: lapsed, cachedAt: now }, now), false);
});

test("a hub that already said no stays a no", () => {
  const now = Date.now();
  const denied = { ...SUB, entitled: false, expiresAt: null };
  assert.equal(cachedEntitlement({ subscription: denied, cachedAt: now }, now), false);
});

test("no plan is not an entitlement", () => {
  const now = Date.now();
  const none = { status: "none" as const, renews: false, expiresAt: null };
  assert.equal(cachedEntitlement({ subscription: none, cachedAt: now }, now), false);
});

test("an old hub that omits entitled is trusted on status alone", () => {
  const now = Date.parse("2026-06-01T00:00:00.000Z");
  const legacy = { status: "active" as const, renews: true, expiresAt: null };
  assert.equal(cachedEntitlement({ subscription: legacy, cachedAt: now }, now), true);
});

test("omitted entitled does not trust canceled, unpaid or past_due", () => {
  const now = Date.now();
  for (const status of ["canceled", "unpaid", "past_due"] as const) {
    const sub = { status, renews: false, expiresAt: null };
    assert.equal(cachedEntitlement({ subscription: sub, cachedAt: now }, now), false, status);
  }
});

test("a renewing subscription survives a missed renewal within the grace window", () => {
  const expiresAt = "2026-06-01T00:00:00.000Z";
  const cachedAt = Date.parse(expiresAt);
  const renewing = { ...SUB, expiresAt };
  const justAfter = Date.parse(expiresAt) + 24 * 60 * 60 * 1000;
  const wayAfter = Date.parse(expiresAt) + 10 * 24 * 60 * 60 * 1000;
  assert.equal(cachedEntitlement({ subscription: renewing, cachedAt }, justAfter), true);
  assert.equal(cachedEntitlement({ subscription: renewing, cachedAt }, wayAfter), false);
});

test("a non-renewing subscription is revoked the moment it expires", () => {
  const expiresAt = "2026-06-01T00:00:00.000Z";
  const cachedAt = Date.parse(expiresAt) - 1000;
  const nonRenewing = { ...SUB, renews: false, expiresAt };
  const justAfter = Date.parse(expiresAt) + 1000;
  assert.equal(cachedEntitlement({ subscription: nonRenewing, cachedAt }, justAfter), false);
});

test("a cache older than the offline grace window stops granting entitlement", () => {
  const cachedAt = Date.parse("2026-01-01T00:00:00.000Z");
  const wayLater = cachedAt + 30 * 24 * 60 * 60 * 1000;
  assert.equal(cachedEntitlement({ subscription: SUB, cachedAt }, wayLater), false);
});
