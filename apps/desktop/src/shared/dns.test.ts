import test from "node:test";
import assert from "node:assert/strict";
import { normalizeCustomDns, resolveWireGuardDns } from "./dns.ts";

test("accepts comma or whitespace separated IPv4 DNS servers", () => {
  assert.deepEqual(normalizeCustomDns("1.1.1.1, 1.0.0.1"), ["1.1.1.1", "1.0.0.1"]);
  assert.deepEqual(normalizeCustomDns("8.8.8.8\n8.8.4.4"), ["8.8.8.8", "8.8.4.4"]);
});

test("accepts the persisted array format", () => {
  assert.deepEqual(normalizeCustomDns(["9.9.9.9", "149.112.112.112"]), [
    "9.9.9.9",
    "149.112.112.112"
  ]);
});

test("an empty value restores the VPN default", () => {
  assert.deepEqual(normalizeCustomDns(""), []);
  assert.deepEqual(normalizeCustomDns("   "), []);
  assert.deepEqual(normalizeCustomDns([]), []);
});

test("canonicalizes octets and removes duplicates", () => {
  assert.deepEqual(normalizeCustomDns("001.001.001.001, 1.1.1.1"), ["1.1.1.1"]);
});

test("rejects invalid addresses and unsupported IPv6", () => {
  assert.equal(normalizeCustomDns("1.1.1"), null);
  assert.equal(normalizeCustomDns("1.1.1.256"), null);
  assert.equal(normalizeCustomDns("dns.example.com"), null);
  assert.equal(normalizeCustomDns("2606:4700:4700::1111"), null);
  assert.equal(normalizeCustomDns("1.1.1.1, nope"), null);
});

test("rejects untrusted non-string settings values", () => {
  assert.equal(normalizeCustomDns(null), null);
  assert.equal(normalizeCustomDns(undefined), null);
  assert.equal(normalizeCustomDns(true), null);
  assert.equal(normalizeCustomDns(["1.1.1.1", 8]), null);
});

test("custom DNS replaces the server DNS in both WireGuard representations", () => {
  assert.deepEqual(resolveWireGuardDns("10.0.0.1", ["1.1.1.1", "1.0.0.1"]), {
    servers: ["1.1.1.1", "1.0.0.1"],
    configValue: "1.1.1.1, 1.0.0.1"
  });
});

test("blank custom DNS falls back to the VPN server DNS", () => {
  assert.deepEqual(resolveWireGuardDns("10.0.0.1, 10.0.0.2", null), {
    servers: ["10.0.0.1", "10.0.0.2"],
    configValue: "10.0.0.1, 10.0.0.2"
  });
});

// Finding 8: an empty array means "use the server default", not "no DNS".
test("an empty custom DNS array falls back to the VPN server DNS", () => {
  assert.deepEqual(resolveWireGuardDns("10.0.0.1, 10.0.0.2", []), {
    servers: ["10.0.0.1", "10.0.0.2"],
    configValue: "10.0.0.1, 10.0.0.2"
  });
});
