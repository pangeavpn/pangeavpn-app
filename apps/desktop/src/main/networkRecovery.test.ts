import assert from "node:assert/strict";
import test from "node:test";
import { NETWORK_RECOVER_COOLDOWN_MS, shouldRecoverFromNetworkChange } from "./networkRecovery.ts";

const base = {
  autoConnectEnabled: true,
  userDisconnected: false,
  lastConnectedProfileId: "p1",
  connectionAttemptRunning: false,
  recoverInProgress: false,
  lastRecoverAtMs: 0,
  nowMs: 10_000_000
};

test("reconnects the last session after a network change", () => {
  assert.equal(shouldRecoverFromNetworkChange(base), true);
});

test("stays down after the user pressed Disconnect, even with auto-connect on", () => {
  assert.equal(shouldRecoverFromNetworkChange({ ...base, userDisconnected: true }), false);
});

test("needs auto-connect and a session to bring back", () => {
  assert.equal(shouldRecoverFromNetworkChange({ ...base, autoConnectEnabled: false }), false);
  assert.equal(shouldRecoverFromNetworkChange({ ...base, lastConnectedProfileId: null }), false);
});

test("does not cut across a connect already running or the cooldown", () => {
  assert.equal(shouldRecoverFromNetworkChange({ ...base, connectionAttemptRunning: true }), false);
  assert.equal(shouldRecoverFromNetworkChange({ ...base, recoverInProgress: true }), false);
  assert.equal(shouldRecoverFromNetworkChange({ ...base, lastRecoverAtMs: base.nowMs - 1_000 }), false);
  assert.equal(
    shouldRecoverFromNetworkChange({ ...base, lastRecoverAtMs: base.nowMs - NETWORK_RECOVER_COOLDOWN_MS }),
    true
  );
});
