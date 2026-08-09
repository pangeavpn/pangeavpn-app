import assert from "node:assert/strict";
import test from "node:test";
import { DaemonClient, TransportExhaustedError } from "./daemonClient.ts";

test("DaemonClient exposes transport exhaustion as a typed error", async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });
  globalThis.fetch = async () => new Response(
    JSON.stringify({ ok: false, error: "transport_exhausted" }),
    { status: 500, headers: { "Content-Type": "application/json" } }
  );

  const client = new DaemonClient("http://127.0.0.1:8787", async () => "token");
  await assert.rejects(client.connect("profile"), TransportExhaustedError);
});

test("DaemonClient leaves unrelated daemon failures non-retryable", async (t) => {
  const originalFetch = globalThis.fetch;
  t.after(() => {
    globalThis.fetch = originalFetch;
  });
  globalThis.fetch = async () => new Response(
    JSON.stringify({ ok: false }),
    { status: 500, headers: { "Content-Type": "application/json" } }
  );

  const client = new DaemonClient("http://127.0.0.1:8787", async () => "token");
  await assert.rejects(
    client.connect("profile"),
    (error) => error instanceof Error && !(error instanceof TransportExhaustedError)
  );
});
