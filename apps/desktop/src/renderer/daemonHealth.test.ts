import assert from "node:assert/strict";
import test from "node:test";
import { daemonHealthAfterFailure, daemonHealthAfterSuccess, initialDaemonHealth } from "./daemonHealth.ts";

test("daemon health requests recovery after three consecutive failures", () => {
  let health = initialDaemonHealth();

  health = daemonHealthAfterFailure(health);
  health = daemonHealthAfterFailure(health);
  assert.deepEqual(health, { consecutiveFailures: 2, recoveryRequired: false });

  health = daemonHealthAfterFailure(health);
  assert.deepEqual(health, { consecutiveFailures: 3, recoveryRequired: true });
});

test("daemon health success clears transient failures and recovery", () => {
  const recovered = daemonHealthAfterSuccess({ consecutiveFailures: 4, recoveryRequired: true });

  assert.deepEqual(recovered, initialDaemonHealth());
});

test("daemon health remains in recovery after further failures", () => {
  const health = daemonHealthAfterFailure({ consecutiveFailures: 3, recoveryRequired: true });

  assert.deepEqual(health, { consecutiveFailures: 4, recoveryRequired: true });
});
