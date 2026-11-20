import { test } from "node:test";
import assert from "node:assert/strict";
import { buildCDNProfile, buildCDNWSProfile, type CDNServerInfo } from "./cdnProfile.ts";

const node: CDNServerInfo = {
  remoteHost: "edge.example",
  remoteIp: "198.51.100.9",
  uuid: "vless-uuid",
  path: "/v",
  vmess: { uuid: "vmess-uuid", path: "/m", remotePort: 2083 }
};

test("cdn stays VLESS when the node also serves VMess", () => {
  assert.deepEqual(buildCDNProfile(node), {
    localPort: 0,
    remoteHost: "198.51.100.9",
    remotePort: 443,
    uuid: "vless-uuid",
    serverName: "edge.example",
    path: "/v"
  });
});

test("cdnws dials the same edge with the VMess route", () => {
  assert.deepEqual(buildCDNWSProfile(node), {
    localPort: 0,
    remoteHost: "198.51.100.9",
    remotePort: 2083,
    uuid: "vmess-uuid",
    serverName: "edge.example",
    path: "/m"
  });
});

test("cdnws is absent without a VMess uuid", () => {
  assert.equal(buildCDNWSProfile({ ...node, vmess: undefined }), undefined);
  assert.equal(buildCDNWSProfile({ ...node, vmess: { uuid: "", path: "/m" } }), undefined);
});
