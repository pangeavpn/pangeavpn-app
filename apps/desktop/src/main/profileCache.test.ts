import assert from "node:assert/strict";
import test from "node:test";
import {
  MAX_CACHED_PROFILES,
  PROFILE_TTL_MS,
  commitProfileSet,
  dropExpired,
  forgetProfile,
  isReusable,
  parseProfileRecords,
  profileFingerprint,
  recordProvision,
  retainOnly,
  type ProfileRecord
} from "./profileCache.ts";

const FP = "fingerprint";
const record = (overrides: Partial<ProfileRecord> = {}): ProfileRecord => ({
  serverId: "us-east-1",
  provisionedAt: 1_000_000,
  fingerprint: FP,
  ...overrides
});

test("a peer inside its TTL with matching inputs is reused", () => {
  assert.equal(isReusable(record(), FP, 1_000_000 + PROFILE_TTL_MS - 1), true);
});

test("a peer past its TTL is not reused", () => {
  assert.equal(isReusable(record(), FP, 1_000_000 + PROFILE_TTL_MS), false);
});

test("changed provisioning inputs invalidate a peer that is still fresh", () => {
  assert.equal(isReusable(record(), "other", 1_000_001), false);
});

test("a record stamped in the future is treated as a clock change, not a fresh peer", () => {
  assert.equal(isReusable(record(), FP, 999_999), false);
});

test("a missing record is never reusable", () => {
  assert.equal(isReusable(undefined, FP, 1_000_001), false);
});

test("the fingerprint tracks MTU, custom DNS and the server entry", () => {
  const base = { wireguardMtu: 1420, customDns: ["1.1.1.1"], server: { id: "a" } };

  assert.equal(profileFingerprint(base), profileFingerprint({ ...base }));
  assert.notEqual(profileFingerprint(base), profileFingerprint({ ...base, wireguardMtu: 1280 }));
  assert.notEqual(profileFingerprint(base), profileFingerprint({ ...base, customDns: ["9.9.9.9"] }));
  assert.notEqual(profileFingerprint(base), profileFingerprint({ ...base, server: { id: "b" } }));
});

test("parseProfileRecords drops malformed and unmanaged entries", () => {
  const parsed = parseProfileRecords({
    "auto-us-east-1": record(),
    "auto-bad": { serverId: "x" },
    "manual-1": record(),
    "auto-worse": "nope"
  });

  assert.deepEqual(Object.keys(parsed), ["auto-us-east-1"]);
});

test("parseProfileRecords tolerates a hand-edited non-object", () => {
  assert.deepEqual(parseProfileRecords([1, 2]), {});
  assert.deepEqual(parseProfileRecords(null), {});
});

test("recordProvision keeps only the newest peers once the cap is reached", () => {
  let records = {};
  for (let i = 0; i <= MAX_CACHED_PROFILES; i += 1) {
    records = recordProvision(records, `auto-r${i}`, record({ provisionedAt: i }));
  }

  assert.equal(Object.keys(records).length, MAX_CACHED_PROFILES);
  assert.equal("auto-r0" in records, false);
});

test("forgetProfile leaves the other peers alone", () => {
  const records = { "auto-a": record(), "auto-b": record() };

  assert.deepEqual(Object.keys(forgetProfile(records, "auto-a")), ["auto-b"]);
});

test("dropExpired removes only the peers past their TTL", () => {
  const records = {
    "auto-fresh": record({ provisionedAt: 1_000_000 }),
    "auto-stale": record({ provisionedAt: 1_000_000 - PROFILE_TTL_MS })
  };

  assert.deepEqual(Object.keys(dropExpired(records, 1_000_001)), ["auto-fresh"]);
});

test("retainOnly forgets records the daemon no longer holds", () => {
  const records = { "auto-live": record(), "auto-gone": record() };

  assert.deepEqual(Object.keys(retainOnly(records, ["auto-live", "manual"])), ["auto-live"]);
});

test("commitProfileSet keeps unmanaged profiles and reusable peers, installing the winner last", () => {
  const winner = { id: "auto-new" };

  assert.deepEqual(
    commitProfileSet(
      [{ id: "manual" }, { id: "auto-keep" }, { id: "auto-spent" }, { id: "auto-new" }],
      winner,
      ["auto-keep"]
    ),
    [{ id: "manual" }, { id: "auto-keep" }, winner]
  );
});
