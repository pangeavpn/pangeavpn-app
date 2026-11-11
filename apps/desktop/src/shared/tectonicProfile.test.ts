import assert from "node:assert/strict";
import test from "node:test";
import { buildTectonicProfile, tectonicCdnEdgeIp, tectonicNodeHost } from "./tectonicProfile.ts";

const base = {
  node: "203.0.113.7:8443",
  key_id: "a2V5LWlk",
  device_key: "ZGV2aWNlLWtleQ=="
};

test("dials the hub's literal node with its port", () => {
  const block = buildTectonicProfile(base, "192.0.2.10");
  assert.equal(block?.node, "203.0.113.7:8443");
  assert.equal(block?.keyId, "a2V5LWlk");
  assert.equal(block?.deviceKey, "ZGV2aWNlLWtleQ==");
});

test("a literal node without a port defaults to 443", () => {
  assert.equal(buildTectonicProfile({ ...base, node: "203.0.113.7" }, "192.0.2.10")?.node, "203.0.113.7:443");
});

test("a hostname node falls back to the node IP, keeping the hub's port", () => {
  const block = buildTectonicProfile({ ...base, node: "tc.node.example.com:8443" }, "192.0.2.10");
  assert.equal(block?.node, "192.0.2.10:8443");
});

test("a bare hostname node falls back to the node IP on 443", () => {
  const block = buildTectonicProfile({ ...base, node: "tc.node.example.com" }, "192.0.2.10");
  assert.equal(block?.node, "192.0.2.10:443");
});

test("a blank node falls back to the node IP on 443", () => {
  assert.equal(buildTectonicProfile({ ...base, node: "  " }, "192.0.2.10")?.node, "192.0.2.10:443");
});

test("an out-of-range port defaults to 443", () => {
  assert.equal(buildTectonicProfile({ ...base, node: "203.0.113.7:99999" }, "192.0.2.10")?.node, "203.0.113.7:443");
});

test("an IPv6 node is bracketed", () => {
  assert.equal(buildTectonicProfile({ ...base, node: "[2001:db8::7]:8443" }, "192.0.2.10")?.node, "[2001:db8::7]:8443");
  assert.equal(buildTectonicProfile({ ...base, node: "2001:db8::7" }, "192.0.2.10")?.node, "[2001:db8::7]:443");
});

test("never sends a hostname when neither address is a literal", () => {
  assert.equal(buildTectonicProfile({ ...base, node: "tc.node.example.com" }, "node.example.com"), undefined);
});

test("omits the block without a credential", () => {
  assert.equal(buildTectonicProfile({ ...base, key_id: "" }, "192.0.2.10"), undefined);
  assert.equal(buildTectonicProfile({ ...base, device_key: "  " }, "192.0.2.10"), undefined);
});

test("localPort is always dynamic and the SNI mode is left to the daemon", () => {
  const block = buildTectonicProfile(base, "192.0.2.10");
  assert.equal(block?.localPort, 0);
  assert.ok(block && !("sniMode" in block) && !("sni" in block), "sniMode/sni must stay unset");
});

test("maps the hub's snake_case fields and passes shaping through untouched", () => {
  const shaping = [{ name: "web", sizes: [1, 2, 3] }];
  const block = buildTectonicProfile(
    {
      ...base,
      cover_domain: "cover.example.com",
      pins: ["pin=="],
      borrowed_sni: ["www.example.org"],
      cdn: { host: "cdn.example.net", path: "/ws", edge: "198.51.100.9:443" },
      shaping
    },
    "192.0.2.10"
  );
  assert.equal(block?.coverDomain, "cover.example.com");
  assert.deepEqual(block?.pins, ["pin=="]);
  assert.deepEqual(block?.borrowedSni, ["www.example.org"]);
  assert.deepEqual(block?.cdn, { host: "cdn.example.net", path: "/ws", edge: "198.51.100.9:443" });
  assert.equal(block?.shaping, shaping);
});

test("omits optional fields the hub left out", () => {
  const block = buildTectonicProfile(base, "192.0.2.10");
  for (const key of ["coverDomain", "pins", "borrowedSni", "cdn", "shaping"]) {
    assert.ok(block && !(key in block), `${key} must stay absent`);
  }
});

test("exposes the node and CDN edge hosts for the AllowedIPs exclusion", () => {
  const block = buildTectonicProfile(
    { ...base, cdn: { host: "cdn.example.net", path: "/ws", edge: "198.51.100.9:443" } },
    "192.0.2.10"
  );
  assert.equal(tectonicNodeHost(block), "203.0.113.7");
  assert.equal(tectonicCdnEdgeIp(block), "198.51.100.9");
  assert.equal(tectonicCdnEdgeIp(buildTectonicProfile(base, "192.0.2.10")), undefined);
});
