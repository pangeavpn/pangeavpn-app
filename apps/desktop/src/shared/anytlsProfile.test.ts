import assert from "node:assert/strict";
import test from "node:test";
import { buildAnyTLSProfile } from "./anytlsProfile.ts";

const base = {
  remoteHost: "at.node.example.com",
  remotePort: 8443,
  password: "hunter2"
};

test("dials the node IP rather than the anytls domain", () => {
  const block = buildAnyTLSProfile(base, "192.0.2.10");
  assert.equal(block.remoteHost, "192.0.2.10");
  assert.equal(block.remotePort, 8443);
  assert.equal(block.password, "hunter2");
});

test("a per-transport remoteIp outranks the shared node address", () => {
  const block = buildAnyTLSProfile({ ...base, remoteIp: "198.51.100.4" }, "192.0.2.10");
  assert.equal(block.remoteHost, "198.51.100.4");
});

test("a blank remoteIp falls back to the node address", () => {
  const block = buildAnyTLSProfile({ ...base, remoteIp: "   " }, "192.0.2.10");
  assert.equal(block.remoteHost, "192.0.2.10");
});

test("localPort is always dynamic so the daemon picks the loopback port", () => {
  assert.equal(buildAnyTLSProfile(base, "192.0.2.10").localPort, 0);
});

test("carries serverName, insecure and pin when the hub named them", () => {
  const block = buildAnyTLSProfile(
    { ...base, serverName: "cover.example.com", insecure: true, pinSha256: "abc123==" },
    "192.0.2.10"
  );
  assert.equal(block.serverName, "cover.example.com");
  assert.equal(block.insecure, true);
  assert.equal(block.pinSha256, "abc123==");
});

test("omits target, insecure and pin when the hub named none", () => {
  const block = buildAnyTLSProfile(base, "192.0.2.10");
  assert.ok(!("targetHost" in block), "targetHost must stay absent for the daemon default");
  assert.ok(!("targetPort" in block), "targetPort must stay absent for the daemon default");
  assert.ok(!("insecure" in block), "insecure must stay absent unless set");
  assert.ok(!("pinSha256" in block), "pinSha256 must stay absent unless set");
});

test("passes a hub-configured relay target through", () => {
  const block = buildAnyTLSProfile(
    { ...base, targetHost: "10.10.1.1", targetPort: 51821 },
    "192.0.2.10"
  );
  assert.equal(block.targetHost, "10.10.1.1");
  assert.equal(block.targetPort, 51821);
});
