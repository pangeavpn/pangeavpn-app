import assert from "node:assert/strict";
import test from "node:test";
import { shouldReleaseKillSwitch, type KillSwitchReleaseStatus } from "./killSwitchRelease.ts";

const status = (overrides: Partial<KillSwitchReleaseStatus> = {}): KillSwitchReleaseStatus => ({
  state: "DISCONNECTED",
  killSwitchActive: true,
  cloak: { running: false },
  wireguard: { running: false },
  ...overrides
});

test("a failed connect that left the switch armed is released", () => {
  assert.equal(shouldReleaseKillSwitch(status(), false), true);
});

test("lockdown keeps the block: that is what the user asked for", () => {
  assert.equal(shouldReleaseKillSwitch(status(), true), false);
});

test("nothing to release when the switch is not armed", () => {
  assert.equal(shouldReleaseKillSwitch(status({ killSwitchActive: false }), false), false);
  assert.equal(shouldReleaseKillSwitch(status({ killSwitchActive: undefined }), false), false);
});

test("the ERROR a failed connect settles on is released, not just DISCONNECTED", () => {
  assert.equal(shouldReleaseKillSwitch(status({ state: "ERROR" }), false), true);
});

test("a live or in-flight session keeps its lock", () => {
  assert.equal(shouldReleaseKillSwitch(status({ state: "CONNECTED" }), false), false);
  assert.equal(shouldReleaseKillSwitch(status({ state: "CONNECTING" }), false), false);
  assert.equal(shouldReleaseKillSwitch(status({ state: "DISCONNECTING" }), false), false);
});

test("a session held for the network to return keeps its lock", () => {
  assert.equal(shouldReleaseKillSwitch(status({ state: "ERROR", offline: true }), false), false);
});

test("a half-built session still running keeps its lock", () => {
  assert.equal(shouldReleaseKillSwitch(status({ cloak: { running: true } }), false), false);
  assert.equal(shouldReleaseKillSwitch(status({ wireguard: { running: true } }), false), false);
});
