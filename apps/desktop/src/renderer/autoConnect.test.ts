import assert from "node:assert/strict";
import test from "node:test";
import {
  attemptInitialAutoConnect,
  getUserIntent,
  initAutoConnect,
  notifyConnectRequested,
  notifyStatusTick,
  notifyUserConnected,
  notifyUserDisconnected,
  type AutoConnectDeps
} from "./autoConnect.ts";

const server = (id: string): ServerInfo => ({
  id,
  name: id,
  region: id,
  country: "",
  cloak: { remoteHost: `${id}.example`, uid: "uid", publicKey: "pk" }
});

function makeDeps(overrides: Partial<AutoConnectDeps> = {}): AutoConnectDeps & {
  inFlightCalls: boolean[];
  connected: (string | undefined)[];
} {
  const inFlightCalls: boolean[] = [];
  const connected: (string | undefined)[] = [];
  const deps: AutoConnectDeps & { inFlightCalls: boolean[]; connected: (string | undefined)[] } = {
    getEnabled: () => true,
    getAuthenticated: () => true,
    getDaemonState: () => "DISCONNECTED",
    getUserIntent: () => "connected",
    getConnectionInFlight: () => false,
    setConnectionInFlight: (v) => inFlightCalls.push(v),
    getLastServerId: () => "srv-1",
    getFallbackServerId: () => null,
    getVisibleServers: () => [server("srv-1")],
    provisionAndSwitch: async () => ({ ok: true, serverId: "srv-1" }),
    onConnected: (serverId) => connected.push(serverId),
    inFlightCalls,
    connected,
    ...overrides
  };
  return deps;
}

test("attemptInitialAutoConnect reports in-flight around the attempt and passes the resolved serverId", async () => {
  const deps = makeDeps();
  initAutoConnect(deps);
  await attemptInitialAutoConnect();
  assert.deepEqual(deps.inFlightCalls, [true, false]);
  assert.deepEqual(deps.connected, ["srv-1"]);
});

test("a user disconnect mid-attempt is not overridden by a late success", async () => {
  let resolveProvision: (value: { ok: true; serverId: string }) => void = () => {};
  const provisionPromise = new Promise<{ ok: true; serverId: string }>((resolve) => {
    resolveProvision = resolve;
  });
  const deps = makeDeps({ provisionAndSwitch: () => provisionPromise });
  initAutoConnect(deps);

  const attempt = attemptInitialAutoConnect();
  notifyUserDisconnected();
  resolveProvision({ ok: true, serverId: "srv-1" });
  await attempt;

  assert.deepEqual(deps.connected, []);
});

test("resolveServerId falls back when the stored id is no longer visible", async () => {
  const calls: string[] = [];
  const deps = makeDeps({
    getVisibleServers: () => [server("srv-2")],
    getFallbackServerId: () => "srv-2",
    provisionAndSwitch: async (serverId) => {
      calls.push(serverId);
      return { ok: true, serverId };
    }
  });
  initAutoConnect(deps);
  await attemptInitialAutoConnect();
  assert.deepEqual(calls, ["srv-2"]);
});

test("a cancelled attempt does not count as a backoff failure", async () => {
  const deps = makeDeps({
    provisionAndSwitch: async () => ({ ok: false, error: "cancelled" })
  });
  initAutoConnect(deps);
  await attemptInitialAutoConnect();

  // Immediately eligible again — no backoff was applied for a cancellation.
  notifyStatusTick();
  assert.equal(deps.inFlightCalls.filter((v) => v).length, 2);
});

test("a success still imposes a short cooldown before the next attempt", async () => {
  const deps = makeDeps();
  initAutoConnect(deps);
  await attemptInitialAutoConnect();

  notifyStatusTick();
  // Still within the post-success cooldown, so no second attempt fires.
  assert.equal(deps.inFlightCalls.filter((v) => v).length, 1);
});

test("notifyUserConnected clears backoff state set up by a prior failure", async () => {
  const deps = makeDeps({ provisionAndSwitch: async () => ({ ok: false, error: "network" }) });
  initAutoConnect(deps);
  await attemptInitialAutoConnect();
  notifyUserConnected();
  notifyStatusTick();
  assert.equal(deps.inFlightCalls.filter((v) => v).length, 2);
});

test("no attempt while the daemon is recovering a dropped session itself", async () => {
  let attempts = 0;
  const deps = makeDeps({
    getDaemonState: () => "ERROR",
    getDaemonReconnecting: () => true,
    provisionAndSwitch: async () => {
      attempts += 1;
      return { ok: true, serverId: "srv-1" };
    }
  });
  initAutoConnect(deps);
  await attemptInitialAutoConnect();
  notifyStatusTick();
  assert.equal(attempts, 0);
});

test("a Connect after Disconnect restores connected intent before the attempt", () => {
  notifyUserDisconnected();
  assert.equal(getUserIntent(), "disconnected");
  notifyConnectRequested();
  assert.equal(getUserIntent(), "connected");
  // Stop mid-attempt still wins, so a late success is discarded.
  notifyUserDisconnected();
  assert.equal(getUserIntent(), "disconnected");
});
