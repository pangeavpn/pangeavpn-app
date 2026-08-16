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
