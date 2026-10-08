import assert from "node:assert/strict";
import test from "node:test";
import worker from "./worker.js";

const HUB = "https://api.pangeavpn.org";

// Stands in for the network: records what the worker forwarded and answers
// like the hub rejecting an empty envelope.
function stubUpstream(t) {
  const calls = [];
  t.mock.method(globalThis, "fetch", async (url, init) => {
    calls.push({ url, init });
    return new Response('{"error":"Missing encrypted envelope fields"}', { status: 400 });
  });
  return calls;
}

const post = (url, body = '{"eph":"x"}') =>
  new Request(url, { method: "POST", body, headers: { "Content-Type": "application/json" } });

for (const route of ["/v1/secure", "/v2/secure"]) {
  test(`relays ${route} to the same route on the hub`, async (t) => {
    const calls = stubUpstream(t);
    const res = await worker.fetch(post(`https://relay.example${route}`));
    assert.equal(res.status, 400, "the hub's status passes through");
    assert.equal(calls.length, 1);
    assert.equal(calls[0].url, `${HUB}${route}`);
    assert.equal(calls[0].init.method, "POST");
  });
}

test("refuses every other path without touching the hub", async (t) => {
  const calls = stubUpstream(t);
  for (const path of ["/", "/v3/secure", "/v2/secure/", "/api/client/regions", "/v2/secure/../admin"]) {
    const res = await worker.fetch(post(`https://relay.example${path}`));
    assert.equal(res.status, 404, path);
  }
  assert.equal(calls.length, 0);
});

test("refuses anything but POST", async (t) => {
  const calls = stubUpstream(t);
  const res = await worker.fetch(new Request("https://relay.example/v2/secure"));
  assert.equal(res.status, 405);
  assert.equal(calls.length, 0);
});

// The upstream host is fixed: a crafted Host or URL must never redirect it.
test("always forwards to the hub, whatever host the request names", async (t) => {
  const calls = stubUpstream(t);
  await worker.fetch(post("https://evil.example/v2/secure"));
  assert.equal(new URL(calls[0].url).origin, HUB);
});

test("refuses an empty or oversized body", async (t) => {
  const calls = stubUpstream(t);
  assert.equal((await worker.fetch(post("https://relay.example/v2/secure", ""))).status, 400);
  const big = "x".repeat(64 * 1024 + 1);
  assert.equal((await worker.fetch(post("https://relay.example/v2/secure", big))).status, 400);
  assert.equal(calls.length, 0);
});

test("refuses a declared oversized body without reading it", async (t) => {
  const calls = stubUpstream(t);
  const body = new ReadableStream({
    pull() {
      throw new Error("the body was read");
    }
  });
  const req = new Request("https://relay.example/v2/secure", {
    method: "POST",
    body,
    duplex: "half",
    headers: { "Content-Type": "application/json", "Content-Length": String(10 * 1024 * 1024) }
  });
  assert.equal((await worker.fetch(req)).status, 400);
  assert.equal(calls.length, 0);
});
