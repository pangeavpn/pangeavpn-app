import assert from "node:assert/strict";
import test from "node:test";
import {
  firstWorkingCreds,
  isHubShadowsocksCreds,
  mergeAdvertisedCreds,
  promoteCreds,
  restoreCachedCreds,
  sameHubShadowsocks,
  type HubShadowsocksCreds
} from "./hubShadowsocksCreds.ts";

const node = (host: string, password = "MTIzNDU2Nzg5MGFiY2RlZg=="): HubShadowsocksCreds => ({
  remoteHost: host,
  remotePort: 8489,
  method: "2022-blake3-aes-128-gcm",
  password
});

test("isHubShadowsocksCreds accepts a complete block", () => {
  assert.equal(isHubShadowsocksCreds(node("192.0.2.10")), true);
});

test("isHubShadowsocksCreds rejects incomplete or malformed blocks", () => {
  for (const bad of [
    null,
    undefined,
    {},
    "nope",
    { ...node("192.0.2.10"), remoteHost: "" },
    { ...node("192.0.2.10"), remotePort: 0 },
    { ...node("192.0.2.10"), remotePort: 70000 },
    { ...node("192.0.2.10"), remotePort: 8489.5 },
    { ...node("192.0.2.10"), remotePort: "8489" },
    { ...node("192.0.2.10"), method: "  " },
    { ...node("192.0.2.10"), password: "" }
  ]) {
    assert.equal(isHubShadowsocksCreds(bad), false, `should reject ${JSON.stringify(bad)}`);
  }
});

// The whole point: one node's rotated key must not strand the client.
test("mergeAdvertisedCreds keeps every node the hub named, not just the first", () => {
  const merged = mergeAdvertisedCreds(
    [],
    [node("192.0.2.10"), node("192.0.2.11"), node("192.0.2.12")]
  );
  assert.equal(merged?.length, 3);
  assert.deepEqual(
    merged?.map((c) => c.remoteHost),
    ["192.0.2.10", "192.0.2.11", "192.0.2.12"]
  );
});

test("mergeAdvertisedCreds skips servers with no control-plane block", () => {
  const merged = mergeAdvertisedCreds([], [undefined, node("192.0.2.10"), null, { junk: true }]);
  assert.equal(merged?.length, 1);
  assert.equal(merged?.[0].remoteHost, "192.0.2.10");
});

test("mergeAdvertisedCreds returns null when the hub advertises nothing usable", () => {
  assert.equal(mergeAdvertisedCreds([node("192.0.2.10")], []), null);
  assert.equal(mergeAdvertisedCreds([node("192.0.2.10")], [null, { junk: true }]), null);
});

test("mergeAdvertisedCreds deduplicates identical blocks", () => {
  const merged = mergeAdvertisedCreds([], [node("192.0.2.10"), node("192.0.2.10")]);
  assert.equal(merged?.length, 1);
});

test("mergeAdvertisedCreds returns null when nothing changed, so no disk write", () => {
  const current = [node("192.0.2.10"), node("192.0.2.11")];
  assert.equal(mergeAdvertisedCreds(current, [node("192.0.2.10"), node("192.0.2.11")]), null);
});

test("mergeAdvertisedCreds reports a rotated password as a change", () => {
  const current = [node("192.0.2.10", "b2xkLXBhc3N3b3JkLTAwMDA=")];
  const merged = mergeAdvertisedCreds(current, [node("192.0.2.10")]);
  assert.equal(merged?.length, 1);
  assert.equal(merged?.[0].password, "MTIzNDU2Nzg5MGFiY2RlZg==");
});

test("mergeAdvertisedCreds keeps the last working node in front across a refresh", () => {
  const current = [node("192.0.2.12"), node("192.0.2.10")];
  const merged = mergeAdvertisedCreds(current, [
    node("192.0.2.10"),
    node("192.0.2.11"),
    node("192.0.2.12")
  ]);
  assert.equal(merged?.[0].remoteHost, "192.0.2.12", "the promoted node must stay first");
  assert.equal(merged?.length, 3);
});

test("mergeAdvertisedCreds copies rather than aliasing the hub payload", () => {
  const advertised = node("192.0.2.10");
  const merged = mergeAdvertisedCreds([], [advertised]);
  advertised.password = "mutated";
  assert.equal(merged?.[0].password, "MTIzNDU2Nzg5MGFiY2RlZg==");
});

test("promoteCreds moves the node that worked to the front", () => {
  const list = [node("192.0.2.10"), node("192.0.2.11"), node("192.0.2.12")];
  const promoted = promoteCreds(list, 2);
  assert.deepEqual(
    promoted?.map((c) => c.remoteHost),
    ["192.0.2.12", "192.0.2.10", "192.0.2.11"]
  );
  assert.equal(list[0].remoteHost, "192.0.2.10", "the input list must not be mutated");
});

test("promoteCreds returns null when there is nothing to move", () => {
  const list = [node("192.0.2.10"), node("192.0.2.11")];
  assert.equal(promoteCreds(list, 0), null);
  assert.equal(promoteCreds(list, -1), null);
  assert.equal(promoteCreds(list, 5), null);
});

// An install from before the list existed still has a bare object on disk.
test("restoreCachedCreds migrates a single stored object into a list", () => {
  const restored = restoreCachedCreds(node("192.0.2.10"));
  assert.equal(restored.length, 1);
  assert.equal(restored[0].remoteHost, "192.0.2.10");
});

test("restoreCachedCreds accepts a stored list and drops junk entries", () => {
  const restored = restoreCachedCreds([
    node("192.0.2.10"),
    { remoteHost: "192.0.2.11" },
    null,
    node("192.0.2.12")
  ]);
  assert.deepEqual(
    restored.map((c) => c.remoteHost),
    ["192.0.2.10", "192.0.2.12"]
  );
});

test("restoreCachedCreds returns an empty list for absent or unusable settings", () => {
  for (const stored of [null, undefined, [], {}, "string", [{ junk: true }]]) {
    assert.deepEqual(restoreCachedCreds(stored), []);
  }
});

test("sameHubShadowsocks compares every field", () => {
  const base = node("192.0.2.10");
  assert.equal(sameHubShadowsocks(base, { ...base }), true);
  assert.equal(sameHubShadowsocks(base, { ...base, remoteHost: "192.0.2.11" }), false);
  assert.equal(sameHubShadowsocks(base, { ...base, remotePort: 8488 }), false);
  assert.equal(sameHubShadowsocks(base, { ...base, method: "aes-128-gcm" }), false);
  assert.equal(sameHubShadowsocks(base, { ...base, password: "other" }), false);
});

// The core of the fix: a stale first node must not end the search.
test("firstWorkingCreds falls through a dead node to a working one", async () => {
  const list = [node("192.0.2.10"), node("192.0.2.11"), node("192.0.2.12")];
  const tried: string[] = [];
  const won = await firstWorkingCreds(list, async (creds) => {
    tried.push(creds.remoteHost);
    return creds.remoteHost === "192.0.2.12" ? 41234 : null;
  });
  assert.deepEqual(tried, ["192.0.2.10", "192.0.2.11", "192.0.2.12"]);
  assert.equal(won?.value, 41234);
  assert.equal(won?.index, 2);
});

test("firstWorkingCreds stops at the first node that answers", async () => {
  const tried: string[] = [];
  const won = await firstWorkingCreds([node("a"), node("b")], async (creds) => {
    tried.push(creds.remoteHost);
    return 1;
  });
  assert.deepEqual(tried, ["a"]);
  assert.equal(won?.index, 0);
});

test("firstWorkingCreds keeps going when a node throws", async () => {
  const errors: number[] = [];
  const won = await firstWorkingCreds(
    [node("a"), node("b")],
    async (creds) => {
      if (creds.remoteHost === "a") throw new Error("connection refused");
      return 7;
    },
    (_err, index) => errors.push(index)
  );
  assert.deepEqual(errors, [0]);
  assert.equal(won?.value, 7);
  assert.equal(won?.index, 1);
});

test("firstWorkingCreds returns null when every node fails", async () => {
  assert.equal(await firstWorkingCreds([node("a"), node("b")], async () => null), null);
  assert.equal(await firstWorkingCreds([], async () => 1), null);
});

test("firstWorkingCreds treats a zero port as a real answer, not a falsy miss", async () => {
  const won = await firstWorkingCreds([node("a")], async () => 0);
  assert.equal(won?.value, 0);
});
