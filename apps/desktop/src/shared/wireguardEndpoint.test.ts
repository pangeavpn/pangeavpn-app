import test from "node:test";
import assert from "node:assert/strict";
import { nodeWireGuardEndpointForRegistration, parseNodeWireGuardEndpoint } from "./wireguardEndpoint.ts";

// TEST-NET addresses only — a real node address must never land here.
test("splits the hub's host:port", () => {
  const parsed = parseNodeWireGuardEndpoint("198.51.100.20:51820");
  assert.deepEqual(parsed, { endpoint: "198.51.100.20:51820", host: "198.51.100.20" });
});

test("tolerates surrounding whitespace", () => {
  const parsed = parseNodeWireGuardEndpoint("  198.51.100.20:51820  ");
  assert.equal(parsed?.endpoint, "198.51.100.20:51820");
});

// A domain would have to be resolved to be excluded from AllowedIPs, and that
// lookup is exactly what the direct method cannot do.
test("refuses a hostname endpoint", () => {
  assert.equal(parseNodeWireGuardEndpoint("node-1.example.org:51820"), null);
});

test("refuses an address with no port", () => {
  assert.equal(parseNodeWireGuardEndpoint("198.51.100.20"), null);
});

test("refuses a port that is not a usable number", () => {
  assert.equal(parseNodeWireGuardEndpoint("198.51.100.20:0"), null);
  assert.equal(parseNodeWireGuardEndpoint("198.51.100.20:70000"), null);
  assert.equal(parseNodeWireGuardEndpoint("198.51.100.20:wg"), null);
  assert.equal(parseNodeWireGuardEndpoint("198.51.100.20:518.20"), null);
});

test("refuses a malformed address", () => {
  assert.equal(parseNodeWireGuardEndpoint("198.51.100.300:51820"), null);
  assert.equal(parseNodeWireGuardEndpoint("198.51.100:51820"), null);
  assert.equal(parseNodeWireGuardEndpoint(":51820"), null);
});

// An older hub, or one mid-deploy, may send nothing at all; the direct method
// then simply stays unavailable for the profile.
test("refuses anything that is not a string", () => {
  assert.equal(parseNodeWireGuardEndpoint(undefined), null);
  assert.equal(parseNodeWireGuardEndpoint(null), null);
  assert.equal(parseNodeWireGuardEndpoint(51820), null);
  assert.equal(parseNodeWireGuardEndpoint(""), null);
});

// Issue #28: under multihop reg.serverEndpoint is the exit, which the client
// never dials. It must not reach AllowedIPs, the kill switch, or direct mode.
test("a single-hop registration keeps the node endpoint", () => {
  assert.deepEqual(nodeWireGuardEndpointForRegistration("198.51.100.20:51820", false), {
    endpoint: "198.51.100.20:51820",
    host: "198.51.100.20"
  });
});

test("a hop drops the endpoint so it can't leak or enable direct mode", () => {
  assert.equal(nodeWireGuardEndpointForRegistration("198.51.100.20:51820", true), null);
});
