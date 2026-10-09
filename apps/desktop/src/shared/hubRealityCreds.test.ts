import assert from "node:assert/strict";
import test from "node:test";
import { mergeAdvertised, restoreCached } from "./hubCredList.ts";
import { isIPv4Literal } from "./ipLiteral.ts";
import {
  DEFAULT_HUB_REALITY,
  REALITY_CREDS,
  isHubRealityCreds,
  sameHubReality,
  seedRealityCreds,
  type HubRealityCreds
} from "./hubRealityCreds.ts";

const node = (host: string, overrides: Partial<HubRealityCreds> = {}): HubRealityCreds => ({
  remoteHost: host,
  remotePort: 443,
  uuid: "2c1f6f1e-5d0a-4a57-9b1e-3f5f0b8a9c11",
  publicKey: "dYchF-wg15LxDwTaiRObrl-RYukvcihh-wC6vnJI2To",
  shortId: "8d3c2b1a0f9e8d7c",
  serverName: "swdist.apple.com",
  ...overrides
});

test("isHubRealityCreds accepts a complete block", () => {
  assert.equal(isHubRealityCreds(node("192.0.2.10")), true);
});

test("isHubRealityCreds rejects incomplete or malformed blocks", () => {
  for (const bad of [
    null,
    undefined,
    {},
    "nope",
    // A name needs a DNS lookup first, and the kill switch permits only IPs.
    node("reality.example.com"),
    node("010.0.2.10"),
    node("192.0.2.10", { remotePort: 0 }),
    node("192.0.2.10", { remotePort: 65536 }),
    node("192.0.2.10", { remotePort: 443.5 }),
    { ...node("192.0.2.10"), remotePort: "443" },
    node("192.0.2.10", { uuid: "not-a-uuid" }),
    node("192.0.2.10", { uuid: "2c1f6f1e5d0a4a579b1e3f5f0b8a9c11" }),
    node("192.0.2.10", { publicKey: "" }),
    node("192.0.2.10", { publicKey: "dYchF-wg15LxDwTaiRObrl-RYukvcihh-wC6vnJI2T" }),
    node("192.0.2.10", { publicKey: "dYchF+wg15LxDwTaiRObrl/RYukvcihh+wC6vnJI2To" }),
    node("192.0.2.10", { shortId: "" }),
    node("192.0.2.10", { shortId: "abc" }),
    node("192.0.2.10", { shortId: "zz" }),
    node("192.0.2.10", { shortId: "aa".repeat(9) }),
    node("192.0.2.10", { serverName: "" }),
    node("192.0.2.10", { serverName: "localhost" }),
    node("192.0.2.10", { serverName: "192.0.2.10" })
  ]) {
    assert.equal(isHubRealityCreds(bad), false, `should reject ${JSON.stringify(bad)}`);
  }
});

test("restoring normalizes case and padding so dedup sees one node", () => {
  const padded = node(" 192.0.2.10 ", {
    uuid: " 2C1F6F1E-5D0A-4A57-9B1E-3F5F0B8A9C11",
    shortId: "8D3C2B1A0F9E8D7C ",
    serverName: " SWDIST.apple.com"
  });
  const restored = restoreCached(REALITY_CREDS, [padded, node("192.0.2.10")]);
  assert.equal(restored.length, 1);
  assert.deepEqual(restored[0], node("192.0.2.10"));
});

test("the public key keeps its case: it is base64, not hex", () => {
  const restored = restoreCached(REALITY_CREDS, [node("192.0.2.10")]);
  assert.equal(restored[0].publicKey, "dYchF-wg15LxDwTaiRObrl-RYukvcihh-wC6vnJI2To");
});

test("sameHubReality compares every field", () => {
  const base = node("192.0.2.10");
  assert.equal(sameHubReality(base, { ...base }), true);
  for (const [field, value] of Object.entries({
    remoteHost: "192.0.2.11",
    remotePort: 8444,
    uuid: "3c1f6f1e-5d0a-4a57-9b1e-3f5f0b8a9c11",
    publicKey: "eYchF-wg15LxDwTaiRObrl-RYukvcihh-wC6vnJI2To",
    shortId: "9d3c2b1a0f9e8d7c",
    serverName: "www.bing.com"
  })) {
    assert.equal(sameHubReality(base, { ...base, [field]: value }), false, field);
  }
});

test("mergeAdvertised picks up every usable control-plane block the hub named", () => {
  const merged = mergeAdvertised(REALITY_CREDS, [], [undefined, node("192.0.2.10"), { junk: true }, node("192.0.2.11")]);
  assert.deepEqual(merged?.map((c) => c.remoteHost), ["192.0.2.10", "192.0.2.11"]);
});

test("DEFAULT_HUB_REALITY ships usable, distinct nodes that dial IP literals", () => {
  assert.ok(DEFAULT_HUB_REALITY.length > 0);
  for (const creds of DEFAULT_HUB_REALITY) {
    assert.equal(isHubRealityCreds(creds), true, creds.remoteHost);
    assert.equal(isIPv4Literal(creds.remoteHost), true, creds.remoteHost);
  }
  const hosts = DEFAULT_HUB_REALITY.map((c) => `${c.remoteHost}:${c.remotePort}`);
  assert.equal(new Set(hosts).size, hosts.length);
});

test("seedRealityCreds falls back to the shipped nodes when nothing usable is stored", () => {
  for (const stored of [null, undefined, [], {}, [{ junk: true }]]) {
    assert.deepEqual(seedRealityCreds(stored), [...DEFAULT_HUB_REALITY]);
  }
});

test("seedRealityCreds prefers what the hub last advertised", () => {
  const stored = [node("192.0.2.10"), node("192.0.2.11")];
  assert.deepEqual(seedRealityCreds(stored), stored);
});

test("seedRealityCreds hands out copies, so a caller cannot alter the shipped nodes", () => {
  const before = DEFAULT_HUB_REALITY.map((c) => ({ ...c }));
  const seeded = seedRealityCreds(null);
  seeded[0].uuid = "mutated";
  seeded.reverse();
  assert.deepEqual(DEFAULT_HUB_REALITY.map((c) => ({ ...c })), before);
});
