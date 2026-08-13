import test from "node:test";
import assert from "node:assert/strict";
import { classifyHubFailure, isSubscriptionLapseBody } from "./entitlement.ts";

// ── Which 403 bodies mean "out of time" rather than "bad identity" ──

test("recognises the marker a lapsed prepaid account produces", () => {
  assert.equal(isSubscriptionLapseBody('{"error":"Subscription expired","code":"SUBSCRIPTION_EXPIRED"}'), true);
});

test("recognises NO_ACTIVE_SUBSCRIPTION, which is what a lapsed Stripe plan actually returns", () => {
  // Stripe flips status to canceled/past_due, so no row matches the hub's
  // active/trialing query and it never reaches the SUBSCRIPTION_EXPIRED branch.
  assert.equal(isSubscriptionLapseBody('{"error":"No active subscription","code":"NO_ACTIVE_SUBSCRIPTION"}'), true);
});

test("a revoked device is not a lapse — that identity really is invalid", () => {
  assert.equal(isSubscriptionLapseBody('{"error":"Device not registered","code":"DEVICE_NOT_REGISTERED"}'), false);
  assert.equal(isSubscriptionLapseBody('{"error":"Invalid VPN token","code":"INVALID_TOKEN"}'), false);
  assert.equal(isSubscriptionLapseBody(""), false);
});

// ── Sign out, or show the expired screen? ──

const authFailure = { status: 403, body: '{"code":"INVALID_TOKEN"}' };

test("a lapse marker never signs the user out", () => {
  assert.equal(
    classifyHubFailure({ status: 403, body: '{"code":"NO_ACTIVE_SUBSCRIPTION"}' }, null),
    "expired"
  );
});

test("a bare 403 from an unentitled account is a lapse, whatever the body said", () => {
  // Backstop: a hub code we do not know about yet must not cost the user their
  // session and a device slot.
  assert.equal(classifyHubFailure(authFailure, { entitled: false }), "expired");
});

test("a bare 403 from an entitled account is a real auth failure", () => {
  assert.equal(classifyHubFailure(authFailure, { entitled: true }), "signOut");
});

test("signs out when entitlement is unknown and nothing suggests a lapse", () => {
  // Hub unreachable for the re-check. Preserves today's behaviour rather than
  // stranding someone on the expired screen with a genuinely dead session.
  assert.equal(classifyHubFailure(authFailure, null), "signOut");
});

test("an older hub omitting entitled is treated as entitled, so a payer is never locked out", () => {
  assert.equal(classifyHubFailure(authFailure, {}), "signOut");
});

test("non-auth statuses are left alone for the normal error path", () => {
  assert.equal(classifyHubFailure({ status: 500, body: "boom" }, { entitled: false }), "passthrough");
  assert.equal(classifyHubFailure({ status: 0, body: "" }, null), "passthrough");
});

test("a 401 is classified like a 403", () => {
  assert.equal(classifyHubFailure({ status: 401, body: "" }, { entitled: false }), "expired");
  assert.equal(classifyHubFailure({ status: 401, body: "" }, { entitled: true }), "signOut");
});
