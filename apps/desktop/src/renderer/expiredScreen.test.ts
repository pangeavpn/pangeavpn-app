import test from "node:test";
import assert from "node:assert/strict";
import { shouldShowExpiredScreen } from "./expiredScreen.ts";

test("takes over once the hub says the account is out of time", () => {
  assert.equal(shouldShowExpiredScreen(false, "DISCONNECTED"), true);
  assert.equal(shouldShowExpiredScreen(false, "ERROR"), true);
});

test("never takes over while a tunnel is up — that would hide the disconnect button", () => {
  // Time can run out mid-session; trapping the user in a screen with no way to
  // stop their own tunnel is worse than letting them finish and disconnect.
  assert.equal(shouldShowExpiredScreen(false, "CONNECTED"), false);
  assert.equal(shouldShowExpiredScreen(false, "CONNECTING"), false);
  assert.equal(shouldShowExpiredScreen(false, "DISCONNECTING"), false);
});

test("stays hidden while entitled, or while the hub has not been asked yet", () => {
  assert.equal(shouldShowExpiredScreen(true, "DISCONNECTED"), false);
  assert.equal(shouldShowExpiredScreen(null, "DISCONNECTED"), false);
});
