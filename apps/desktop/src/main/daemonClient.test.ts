import assert from "node:assert/strict";
import http from "node:http";
import type { AddressInfo } from "node:net";
import test from "node:test";
import { DaemonClient, TransportExhaustedError } from "./daemonClient.ts";

async function serve(
  t: { after: (fn: () => unknown) => void },
  handler: http.RequestListener
): Promise<{ baseUrl: string; server: http.Server }> {
  const server = http.createServer(handler);
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  t.after(() => new Promise((resolve) => server.close(resolve)));
  const { port } = server.address() as AddressInfo;
  return { baseUrl: `http://127.0.0.1:${port}`, server };
}

test("DaemonClient exposes transport exhaustion as a typed error", async (t) => {
  const { baseUrl } = await serve(t, (_req, res) => {
    res.writeHead(500, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ ok: false, error: "transport_exhausted" }));
  });

  const client = new DaemonClient(baseUrl, async () => "token");
  await assert.rejects(client.connect("profile"), TransportExhaustedError);
});

test("DaemonClient leaves unrelated daemon failures non-retryable", async (t) => {
  const { baseUrl } = await serve(t, (_req, res) => {
    res.writeHead(500, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ ok: false }));
  });

  const client = new DaemonClient(baseUrl, async () => "token");
  await assert.rejects(
    client.connect("profile"),
    (error) => error instanceof Error && !(error instanceof TransportExhaustedError)
  );
});

// The field failure this guards: a pooled keep-alive socket that died quietly
// turned every later poll into a 5s timeout while the daemon sat healthy.
test("DaemonClient opens a fresh connection per request", async (t) => {
  const { baseUrl, server } = await serve(t, (_req, res) => {
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ state: "disconnected" }));
  });
  let connections = 0;
  server.on("connection", () => {
    connections += 1;
  });

  const client = new DaemonClient(baseUrl, async () => "token");
  await client.getStatus();
  await client.getStatus();
  assert.equal(connections, 2, "requests must not share a pooled socket");
});

test("DaemonClient falls through a stale token to a working one", async (t) => {
  const { baseUrl } = await serve(t, (req, res) => {
    if (req.headers.authorization !== "Bearer good") {
      res.writeHead(401, { "Content-Type": "application/json" });
      res.end(JSON.stringify({ error: "unauthorized" }));
      return;
    }
    res.writeHead(200, { "Content-Type": "application/json" });
    res.end(JSON.stringify({ state: "disconnected" }));
  });

  const client = new DaemonClient(baseUrl, async () => ["stale", "good"]);
  const status = await client.getStatus();
  assert.equal(status.state, "disconnected");
});

test("DaemonClient reports a stalled daemon as a timeout", async (t) => {
  const { baseUrl } = await serve(t, () => {
    // Never respond; the client's timer must fire.
  });

  const client = new DaemonClient(baseUrl, async () => "token", { defaultRequestTimeoutMs: 200 });
  await assert.rejects(client.getStatus(), /daemon request timeout \(GET \/status\)/);
});
