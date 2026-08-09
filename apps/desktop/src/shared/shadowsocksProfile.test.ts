import assert from "node:assert/strict";
import test from "node:test";
import { buildShadowsocksProfile } from "./shadowsocksProfile.ts";

const base = {
  remoteHost: "ss.node.example.com",
  remotePort: 8488,
  method: "2022-blake3-aes-128-gcm",
  password: "MTIzNDU2Nzg5MGFiY2RlZg=="
};

test("dials the node IP rather than the shadowsocks domain", () => {
  const block = buildShadowsocksProfile(base, "192.0.2.10");
  assert.equal(block.remoteHost, "192.0.2.10");
  assert.equal(block.remotePort, 8488);
  assert.equal(block.method, "2022-blake3-aes-128-gcm");
  assert.equal(block.password, "MTIzNDU2Nzg5MGFiY2RlZg==");
});

test("a per-transport remoteIp outranks the shared node address", () => {
  const block = buildShadowsocksProfile({ ...base, remoteIp: "198.51.100.4" }, "192.0.2.10");
  assert.equal(block.remoteHost, "198.51.100.4");
});

test("a blank remoteIp falls back to the node address", () => {
  const block = buildShadowsocksProfile({ ...base, remoteIp: "   " }, "192.0.2.10");
  assert.equal(block.remoteHost, "192.0.2.10");
});

test("localPort is always dynamic so the daemon picks the loopback port", () => {
  assert.equal(buildShadowsocksProfile(base, "192.0.2.10").localPort, 0);
});

test("omits target and udpOverTcp when the hub named none", () => {
  const block = buildShadowsocksProfile(base, "192.0.2.10");
  assert.ok(!("targetHost" in block), "targetHost must stay absent for the daemon default");
  assert.ok(!("targetPort" in block), "targetPort must stay absent for the daemon default");
  assert.ok(!("udpOverTcp" in block), "udpOverTcp must stay absent when not requested");
});

test("passes a hub-configured relay target through", () => {
  const block = buildShadowsocksProfile(
    { ...base, targetHost: "10.10.1.1", targetPort: 51821 },
    "192.0.2.10"
  );
  assert.equal(block.targetHost, "10.10.1.1");
  assert.equal(block.targetPort, 51821);
});

test("udpOverTcp is emitted only when true", () => {
  assert.equal(buildShadowsocksProfile({ ...base, udpOverTcp: true }, "192.0.2.10").udpOverTcp, true);
  const off = buildShadowsocksProfile({ ...base, udpOverTcp: false }, "192.0.2.10");
  assert.ok(!("udpOverTcp" in off));
});
