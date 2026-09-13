import assert from "node:assert/strict";
import test from "node:test";
import type { DiagnosticsPayload } from "./diagnosticsReport.ts";
import { DIAGNOSTICS_ROUTE, uploadDiagnostics } from "./diagnosticsUpload.ts";

const PAYLOAD: DiagnosticsPayload = {
  appVersion: "0.7.1",
  platform: "darwin",
  arch: "arm64",
  osRelease: "24.5.0",
  createdAt: "2026-09-13T10:00:00.000Z",
  sections: [{ name: "app.log", bytes: 4, text: "hi\n" }]
};

function reply(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
}

test("posts plain JSON to the diagnostics route with no credentials", async () => {
  const seen: { url: string; init: RequestInit }[] = [];
  const result = await uploadDiagnostics(PAYLOAD, {
    hosts: ["api.pangeavpn.org"],
    fetchImpl: async (url, init) => {
      seen.push({ url, init });
      return reply(201, { reportCode: "K7M2-9QXD" });
    }
  });
  assert.deepEqual(result, { ok: true, reportCode: "K7M2-9QXD" });
  assert.equal(seen[0].url, `https://api.pangeavpn.org${DIAGNOSTICS_ROUTE}`);
  assert.equal(seen[0].init.method, "POST");
  const headers = seen[0].init.headers as Record<string, string>;
  assert.deepEqual(Object.keys(headers), ["Content-Type"]);
  assert.equal(headers["Content-Type"], "application/json");
  assert.deepEqual(JSON.parse(String(seen[0].init.body)), PAYLOAD);
});

test("falls through to the mirror host when the first one cannot be reached", async () => {
  const tried: string[] = [];
  const result = await uploadDiagnostics(PAYLOAD, {
    hosts: ["api.pangeavpn.org", "api.pangeavpn.it"],
    fetchImpl: async (url) => {
      tried.push(url);
      if (url.includes(".org")) throw new Error("ENOTFOUND");
      return reply(201, { reportCode: "AB12-CD34" });
    }
  });
  assert.deepEqual(result, { ok: true, reportCode: "AB12-CD34" });
  assert.equal(tried.length, 2);
});

test("every host failing reports unreachable rather than throwing", async () => {
  const result = await uploadDiagnostics(PAYLOAD, {
    hosts: ["a.example", "b.example"],
    fetchImpl: async () => {
      throw new Error("ECONNREFUSED");
    }
  });
  assert.deepEqual(result, { ok: false, reason: "unreachable" });
});

test("an error status from the hub reports rejected", async () => {
  const result = await uploadDiagnostics(PAYLOAD, {
    hosts: ["api.pangeavpn.org"],
    fetchImpl: async () => reply(413, { error: "too large" })
  });
  assert.deepEqual(result, { ok: false, reason: "rejected" });
});

test("a reply without a usable report code is rejected, not reported as success", async () => {
  for (const body of [{}, { reportCode: 7 }, { reportCode: "  " }, { reportCode: "not a code!" }]) {
    const result = await uploadDiagnostics(PAYLOAD, {
      hosts: ["api.pangeavpn.org"],
      fetchImpl: async () => reply(201, body)
    });
    assert.deepEqual(result, { ok: false, reason: "rejected" });
  }
});

test("the request carries an abort signal so a hung hub cannot wedge the UI", async () => {
  const result = await uploadDiagnostics(PAYLOAD, {
    hosts: ["api.pangeavpn.org"],
    timeoutMs: 10,
    fetchImpl: (_url, init) =>
      new Promise<Response>((_resolve, reject) => {
        init.signal?.addEventListener("abort", () => reject(new Error("aborted")));
      })
  });
  assert.deepEqual(result, { ok: false, reason: "unreachable" });
});
