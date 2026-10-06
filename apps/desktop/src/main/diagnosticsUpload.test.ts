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
  sections: [{ name: "app.log", bytes: 4, text: "hi" }]
};

test("posts the report to the diagnostics route over the sealed hub path", async () => {
  const seen: { route: string; payload: DiagnosticsPayload }[] = [];
  const result = await uploadDiagnostics(PAYLOAD, {
    send: async (route, payload) => {
      seen.push({ route, payload });
      return { status: 201, body: { reportCode: "K7M2-9QXD" } };
    }
  });
  assert.deepEqual(result, { ok: true, reportCode: "K7M2-9QXD" });
  assert.deepEqual(seen, [{ route: DIAGNOSTICS_ROUTE, payload: PAYLOAD }]);
});

test("a hub that cannot be reached reports unreachable rather than throwing", async () => {
  const result = await uploadDiagnostics(PAYLOAD, {
    send: async () => {
      throw new Error("Hub API timeout (path resolution)");
    }
  });
  assert.deepEqual(result, { ok: false, reason: "unreachable" });
});

test("an error status from the hub reports rejected", async () => {
  const result = await uploadDiagnostics(PAYLOAD, {
    send: async () => ({ status: 413, body: { error: "too large" } })
  });
  assert.deepEqual(result, { ok: false, reason: "rejected" });
});

test("a reply without a usable report code is rejected, not reported as success", async () => {
  for (const body of [{}, { reportCode: 7 }, { reportCode: "  " }, { reportCode: "not a code!" }, undefined]) {
    const result = await uploadDiagnostics(PAYLOAD, { send: async () => ({ status: 201, body }) });
    assert.deepEqual(result, { ok: false, reason: "rejected" });
  }
});

test("the request carries an abort signal so a hung hub cannot wedge the UI", async () => {
  const result = await uploadDiagnostics(PAYLOAD, {
    timeoutMs: 10,
    send: (_route, _payload, signal) =>
      new Promise((_resolve, reject) => {
        signal.addEventListener("abort", () => reject(new Error("aborted")));
      })
  });
  assert.deepEqual(result, { ok: false, reason: "unreachable" });
});
