import assert from "node:assert/strict";
import test from "node:test";
import {
  planAfterServerExhausted,
  SERVER_ROTATION_COOLDOWN_MS,
  shouldRotateServers
} from "./serverRotation.ts";

const base = {
  transportsExhausted: true,
  connectionAttemptRunning: false,
  rotationInFlight: false,
  lastRotationAtMs: 0,
  nowMs: 10_000_000
};

test("rotates when the daemon says this server has no transport left", () => {
  assert.equal(shouldRotateServers(base), true);
});

test("leaves a healthy session alone", () => {
  assert.equal(shouldRotateServers({ ...base, transportsExhausted: false }), false);
});

test("does not cut across a connect attempt already running", () => {
  assert.equal(shouldRotateServers({ ...base, connectionAttemptRunning: true }), false);
  assert.equal(shouldRotateServers({ ...base, rotationInFlight: true }), false);
});

test("waits out the cooldown so a blocked network does not spin", () => {
  assert.equal(shouldRotateServers({ ...base, lastRotationAtMs: base.nowMs - 1_000 }), false);
  assert.equal(
    shouldRotateServers({ ...base, lastRotationAtMs: base.nowMs - SERVER_ROTATION_COOLDOWN_MS - 1 }),
    true
  );
});

test("retries the exhausted server first, then the rest", () => {
  assert.deepEqual(
    planAfterServerExhausted(["us-1", "de-1", "sg-1"], "us-1"),
    ["us-1", "de-1", "sg-1"]
  );
  assert.deepEqual(
    planAfterServerExhausted(["de-1", "us-1", "sg-1"], "us-1"),
    ["us-1", "de-1", "sg-1"]
  );
});

test("keeps the exhausted server exactly once", () => {
  assert.deepEqual(
    planAfterServerExhausted(["us-1", "us-1", "de-1"], "us-1"),
    ["us-1", "de-1"]
  );
});

test("leaves the order alone when the failed server is unknown or null", () => {
  assert.deepEqual(planAfterServerExhausted(["us-1", "de-1"], null), ["us-1", "de-1"]);
  assert.deepEqual(planAfterServerExhausted(["us-1", "de-1"], "gone-1"), ["us-1", "de-1"]);
});
