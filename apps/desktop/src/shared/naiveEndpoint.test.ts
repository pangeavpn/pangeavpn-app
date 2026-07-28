import test from "node:test";
import assert from "node:assert/strict";
import { resolveNaiveEndpoint } from "./naiveEndpoint.ts";

// TEST-NET / example domains only — a real node address must never land here.
const NODE_IP = "198.51.100.20";
const NAIVE_HOST = "naive-1.node.example";

test("dials the node IP when the hub names no per-transport IP", () => {
  const ep = resolveNaiveEndpoint({ remoteHost: NAIVE_HOST }, NODE_IP);
  assert.equal(ep.remoteHost, NODE_IP);
  assert.equal(ep.serverName, NAIVE_HOST);
});

test("prefers the hub's per-transport IP over the node IP", () => {
  const ep = resolveNaiveEndpoint(
    { remoteHost: NAIVE_HOST, remoteIp: "198.51.100.7" },
    NODE_IP
  );
  assert.equal(ep.remoteHost, "198.51.100.7");
  assert.equal(ep.serverName, NAIVE_HOST);
});

test("keeps an explicit hub serverName as the SNI", () => {
  const ep = resolveNaiveEndpoint(
    { remoteHost: NAIVE_HOST, serverName: "cover.example.com" },
    NODE_IP
  );
  assert.equal(ep.remoteHost, NODE_IP);
  assert.equal(ep.serverName, "cover.example.com");
});

// The MAP rule (and so the DNS-free dial) only exists when the two differ.
test("dial target and SNI differ so the engine installs its MAP rule", () => {
  const ep = resolveNaiveEndpoint({ remoteHost: NAIVE_HOST }, NODE_IP);
  assert.notEqual(ep.remoteHost, ep.serverName);
});

test("falls back to the hub's host when no IP is known anywhere", () => {
  const ep = resolveNaiveEndpoint({ remoteHost: NAIVE_HOST }, "");
  assert.equal(ep.remoteHost, NAIVE_HOST);
  assert.equal(ep.serverName, NAIVE_HOST);
});

test("tolerates a node address that is itself a hostname", () => {
  // Legacy/self-hosted nodes name cloak.remoteHost by domain. Dialing it is no
  // worse than today, and the SNI still names the naive host.
  const ep = resolveNaiveEndpoint({ remoteHost: NAIVE_HOST }, "node.example.org");
  assert.equal(ep.remoteHost, "node.example.org");
  assert.equal(ep.serverName, NAIVE_HOST);
});

test("ignores blank hub values", () => {
  const ep = resolveNaiveEndpoint(
    { remoteHost: NAIVE_HOST, remoteIp: "  ", serverName: "  " },
    NODE_IP
  );
  assert.equal(ep.remoteHost, NODE_IP);
  assert.equal(ep.serverName, NAIVE_HOST);
});

// An address in remoteHost is dialled as given, not swapped for the node.
test("dials an address given in remoteHost rather than substituting the node", () => {
  const ep = resolveNaiveEndpoint({ remoteHost: "198.51.100.9" }, NODE_IP);
  assert.equal(ep.remoteHost, "198.51.100.9");
  assert.equal(ep.serverName, "198.51.100.9");
  assert.equal(ep.remoteHost, ep.serverName, "no MAP rule when there is no name to map");
});

// remoteIp outranks everything, as it does for Reality and Hysteria2.
test("remoteIp outranks an address already sitting in remoteHost", () => {
  const ep = resolveNaiveEndpoint(
    { remoteHost: "198.51.100.9", remoteIp: "203.0.113.44" },
    NODE_IP
  );
  assert.equal(ep.remoteHost, "203.0.113.44");
});
