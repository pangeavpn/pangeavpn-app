import { test } from "node:test";
import assert from "node:assert/strict";
import { DARWIN_SOFTWARE_PATTERN, collectDarwinHostSnapshot, collectWindowsHostSnapshot } from "./hostSnapshot.ts";

const matcher = new RegExp(DARWIN_SOFTWARE_PATTERN, "i");

test("the macOS interceptor pattern catches known TLS-interception / VPN / security software", () => {
  for (const proc of [
    "/Applications/Zscaler/Zscaler.app/Contents/MacOS/Zscaler",
    "/Applications/Netskope Client.app/Contents/MacOS/stAgentUI",
    "/Applications/GlobalProtect.app/Contents/MacOS/GlobalProtect",
    "com.cisco.anyconnect.gui",
    "WireGuardNetworkExtension",
    "openvpn",
    "com.pangea.pangeavpn.daemon"
  ]) {
    assert.ok(matcher.test(proc), `expected pattern to match ${proc}`);
  }
});

test("the macOS interceptor pattern does not match ordinary apps", () => {
  for (const proc of ["/System/Applications/Safari.app/Contents/MacOS/Safari", "/usr/sbin/cfprefsd", "Finder", "node"]) {
    assert.ok(!matcher.test(proc), `expected pattern not to match ${proc}`);
  }
});

test("host snapshot collectors are exposed per platform", () => {
  assert.equal(typeof collectDarwinHostSnapshot, "function");
  assert.equal(typeof collectWindowsHostSnapshot, "function");
});
