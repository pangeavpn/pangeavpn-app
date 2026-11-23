import assert from "node:assert/strict";
import http from "node:http";
import test from "node:test";
import { startLoopbackCallback, type LoopbackCallback } from "./loopbackCallback.ts";

const STATE = "s".repeat(43);

function get(
  port: number,
  pathAndQuery: string,
  options: { host?: string; method?: string } = {}
): Promise<{ status: number; headers: http.IncomingHttpHeaders; body: string }> {
  return new Promise((resolve, reject) => {
    const req = http.request(
      {
        host: "127.0.0.1",
        port,
        path: pathAndQuery,
        method: options.method ?? "GET",
        headers: { Host: options.host ?? `127.0.0.1:${port}` },
        agent: false
      },
      (res) => {
        let body = "";
        res.setEncoding("utf8");
        res.on("data", (chunk) => (body += chunk));
        res.on("end", () => resolve({ status: res.statusCode ?? 0, headers: res.headers, body }));
      }
    );
    req.on("error", reject);
    req.end();
  });
}

async function withLoopback(fn: (cb: LoopbackCallback) => Promise<void>, timeoutMs = 5000): Promise<void> {
  const cb = await startLoopbackCallback({ state: STATE, timeoutMs });
  try {
    await fn(cb);
  } finally {
    cb.close();
  }
}

test("listens on an OS-assigned 127.0.0.1 port", async () => {
  await withLoopback(async (cb) => {
    assert.ok(cb.port > 0);
  });
});

test("a good callback resolves the normalised code, answers with hardened headers, and closes", async () => {
  await withLoopback(async (cb) => {
    const res = await get(cb.port, `/callback?code=7k3mq-9xa2p&state=${STATE}`);
    assert.equal(res.status, 200);
    assert.equal(await cb.code, "7K3MQ9XA2P");
    assert.equal(res.headers["cache-control"], "no-store");
    assert.equal(res.headers["referrer-policy"], "no-referrer");
    assert.equal(res.headers["content-security-policy"], "default-src 'none'; style-src 'unsafe-inline'");
    assert.match(res.body, /finish signing in/);
    await assert.rejects(get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}`));
  });
});

test("wrong paths and methods get 404 and the wait goes on", async () => {
  await withLoopback(async (cb) => {
    assert.equal((await get(cb.port, "/")).status, 404);
    assert.equal((await get(cb.port, `/other?code=7K3MQ9XA2P&state=${STATE}`)).status, 404);
    assert.equal((await get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}`, { method: "POST" })).status, 404);
    const res = await get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}`);
    assert.equal(res.status, 200);
    assert.equal(await cb.code, "7K3MQ9XA2P");
  });
});

test("a foreign Host header (DNS rebinding) is refused", async () => {
  await withLoopback(async (cb) => {
    const res = await get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}`, { host: `evil.example:${cb.port}` });
    assert.equal(res.status, 404);
  });
});

test("missing or wrong state, or a bad code, gets 400 and the wait goes on", async () => {
  await withLoopback(async (cb) => {
    assert.equal((await get(cb.port, "/callback?code=7K3MQ9XA2P")).status, 400);
    assert.equal((await get(cb.port, `/callback?code=7K3MQ9XA2P&state=${"x".repeat(43)}`)).status, 400);
    assert.equal((await get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}x`)).status, 400);
    assert.equal((await get(cb.port, `/callback?code=short&state=${STATE}`)).status, 400);
    const bad = await get(cb.port, `/callback?state=${STATE}`);
    assert.equal(bad.status, 400);
    assert.equal(bad.headers["content-security-policy"], "default-src 'none'; style-src 'unsafe-inline'");
    const res = await get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}`);
    assert.equal(res.status, 200);
    assert.equal(await cb.code, "7K3MQ9XA2P");
  });
});

test("the wait times out and the port is released", async () => {
  const cb = await startLoopbackCallback({ state: STATE, timeoutMs: 50 });
  await assert.rejects(cb.code, { name: "LoopbackClosedError", reason: "timeout" });
  await assert.rejects(get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}`));
});

test("close() cancels the wait", async () => {
  const cb = await startLoopbackCallback({ state: STATE, timeoutMs: 5000 });
  cb.close();
  await assert.rejects(cb.code, { name: "LoopbackClosedError", reason: "closed" });
});

test("page text is HTML-escaped", async () => {
  const cb = await startLoopbackCallback({
    state: STATE,
    timeoutMs: 5000,
    text: { done: "<b>ok</b>", invalid: "bad & <wrong>" }
  });
  try {
    const bad = await get(cb.port, "/callback");
    assert.match(bad.body, /bad &#38; &#60;wrong&#62;/);
    const res = await get(cb.port, `/callback?code=7K3MQ9XA2P&state=${STATE}`);
    assert.match(res.body, /&#60;b&#62;ok&#60;\/b&#62;/);
  } finally {
    cb.close();
  }
});
