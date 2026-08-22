import assert from "node:assert/strict";
import test from "node:test";
import {
  DEFAULT_FRONTED_ENDPOINTS,
  isFrontedEndpoint,
  mergeFrontedEndpoints,
  normalizeFrontedEndpoint,
  promoteFrontedEndpoint,
  restoreFrontedEndpoints,
  seedFrontedEndpoints
} from "./frontedEndpoints.ts";

test("a plain hostname is accepted and lowercased", () => {
  assert.equal(normalizeFrontedEndpoint("Relay-A.Example.WORKERS.dev"), "relay-a.example.workers.dev");
  assert.equal(normalizeFrontedEndpoint("  relay.example.com  "), "relay.example.com");
});

test("anything carrying a scheme, port or path is rejected", () => {
  assert.equal(normalizeFrontedEndpoint("https://relay.example.com"), null);
  assert.equal(normalizeFrontedEndpoint("relay.example.com:8443"), null);
  assert.equal(normalizeFrontedEndpoint("relay.example.com/v1/secure"), null);
  assert.equal(normalizeFrontedEndpoint("relay.example.com?x=1"), null);
});

test("a bare label is rejected so a hostile LAN search domain cannot claim it", () => {
  assert.equal(normalizeFrontedEndpoint("relay"), null);
  assert.equal(normalizeFrontedEndpoint("localhost"), null);
});

test("malformed labels are rejected", () => {
  assert.equal(normalizeFrontedEndpoint("-relay.example.com"), null);
  assert.equal(normalizeFrontedEndpoint("relay-.example.com"), null);
  assert.equal(normalizeFrontedEndpoint("relay..example.com"), null);
  assert.equal(normalizeFrontedEndpoint("relay.exa_mple.com"), null);
  assert.equal(normalizeFrontedEndpoint(`${"a".repeat(64)}.example.com`), null);
  assert.equal(normalizeFrontedEndpoint(""), null);
  assert.equal(normalizeFrontedEndpoint("   "), null);
});

// Finding 5: a dotted-quad IP splits into four valid labels and must not pass.
test("an IPv4 literal is rejected even though every octet is a valid label", () => {
  assert.equal(normalizeFrontedEndpoint("192.168.1.7"), null);
  assert.equal(normalizeFrontedEndpoint("8.8.8.8"), null);
});

test("a hostname over the DNS length limit is rejected", () => {
  const tooLong = `${Array.from({ length: 26 }, () => "a".repeat(9)).join(".")}.com`;
  assert.ok(tooLong.length > 253);
  assert.equal(normalizeFrontedEndpoint(tooLong), null);
});

test("non-strings are rejected", () => {
  assert.equal(isFrontedEndpoint(undefined), false);
  assert.equal(isFrontedEndpoint(null), false);
  assert.equal(isFrontedEndpoint(42), false);
  assert.equal(isFrontedEndpoint({ host: "relay.example.com" }), false);
  assert.equal(isFrontedEndpoint("relay.example.com"), true);
});

test("restore validates, deduplicates and preserves order", () => {
  assert.deepEqual(
    restoreFrontedEndpoints(["b.example.com", "junk", "A.EXAMPLE.COM", "a.example.com", 7]),
    ["b.example.com", "a.example.com"]
  );
});

test("restore accepts a single stored string and an absent value", () => {
  assert.deepEqual(restoreFrontedEndpoints("relay.example.com"), ["relay.example.com"]);
  assert.deepEqual(restoreFrontedEndpoints(undefined), []);
  assert.deepEqual(restoreFrontedEndpoints(null), []);
  assert.deepEqual(restoreFrontedEndpoints([]), []);
});

test("merge reports null when the advertisement matches the cache", () => {
  const current = ["a.example.com", "b.example.com"];
  assert.equal(mergeFrontedEndpoints(current, ["a.example.com", "b.example.com"]), null);
});

test("merge keeps the relay that last worked in front", () => {
  const current = ["b.example.com", "a.example.com"];
  assert.deepEqual(mergeFrontedEndpoints(current, ["a.example.com", "b.example.com", "c.example.com"]), [
    "b.example.com",
    "a.example.com",
    "c.example.com"
  ]);
});

test("merge takes the hub's list when the leader is gone", () => {
  const current = ["gone.example.com"];
  assert.deepEqual(mergeFrontedEndpoints(current, ["a.example.com", "b.example.com"]), [
    "a.example.com",
    "b.example.com"
  ]);
});

test("an empty or junk advertisement never discards the cache", () => {
  const current = ["a.example.com"];
  assert.equal(mergeFrontedEndpoints(current, []), null);
  assert.equal(mergeFrontedEndpoints(current, undefined), null);
  assert.equal(mergeFrontedEndpoints(current, ["nonsense", 5]), null);
});

test("merge populates an empty cache", () => {
  assert.deepEqual(mergeFrontedEndpoints([], ["a.example.com"]), ["a.example.com"]);
});

test("promote moves the winner to the front and reports no-ops as null", () => {
  const list = ["a.example.com", "b.example.com", "c.example.com"];
  assert.deepEqual(promoteFrontedEndpoint(list, 2), [
    "c.example.com",
    "a.example.com",
    "b.example.com"
  ]);
  assert.equal(promoteFrontedEndpoint(list, 0), null);
  assert.equal(promoteFrontedEndpoint(list, -1), null);
  assert.equal(promoteFrontedEndpoint(list, 3), null);
  assert.deepEqual(list, ["a.example.com", "b.example.com", "c.example.com"], "input untouched");
});

test("an install with nothing stored still has the shipped relays", () => {
  assert.deepEqual(seedFrontedEndpoints(null), [...DEFAULT_FRONTED_ENDPOINTS]);
  assert.deepEqual(seedFrontedEndpoints([]), [...DEFAULT_FRONTED_ENDPOINTS]);
  assert.deepEqual(seedFrontedEndpoints(["not a host"]), [...DEFAULT_FRONTED_ENDPOINTS]);
});

test("every shipped relay is a valid hostname", () => {
  for (const host of DEFAULT_FRONTED_ENDPOINTS) {
    assert.equal(normalizeFrontedEndpoint(host), host);
  }
});

test("what the hub named wins over the shipped relays", () => {
  assert.deepEqual(seedFrontedEndpoints(["relay-b.example.com"]), ["relay-b.example.com"]);
});
