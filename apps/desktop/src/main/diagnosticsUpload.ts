import type { DiagnosticsPayload } from "./diagnosticsReport.ts";

export const DIAGNOSTICS_ROUTE = "/api/client/diagnostics";
const DEFAULT_TIMEOUT_MS = 30000;
const REPORT_CODE = /^[A-Z0-9-]{4,32}$/;

export type DiagnosticsUploadResult =
  | { ok: true; reportCode: string }
  | { ok: false; reason: "unreachable" | "rejected" };

export interface DiagnosticsUploadOptions {
  hosts: readonly string[];
  fetchImpl: (input: string, init: RequestInit) => Promise<Response>;
  timeoutMs?: number;
}

async function postOnce(
  host: string,
  body: string,
  options: DiagnosticsUploadOptions
): Promise<DiagnosticsUploadResult> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), options.timeoutMs ?? DEFAULT_TIMEOUT_MS);
  try {
    const response = await options.fetchImpl(`https://${host}${DIAGNOSTICS_ROUTE}`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body,
      signal: controller.signal
    });
    if (!response.ok) return { ok: false, reason: "rejected" };
    const parsed = (await response.json()) as { reportCode?: unknown };
    const code = typeof parsed.reportCode === "string" ? parsed.reportCode.trim() : "";
    if (!REPORT_CODE.test(code)) return { ok: false, reason: "rejected" };
    return { ok: true, reportCode: code };
  } finally {
    clearTimeout(timer);
  }
}

/** Plain anonymous POST, deliberately outside pangeaApiClient's secure channel:
 *  that path needs a license key and caps inner bodies at 16kb. */
export async function uploadDiagnostics(
  payload: DiagnosticsPayload,
  options: DiagnosticsUploadOptions
): Promise<DiagnosticsUploadResult> {
  const body = JSON.stringify(payload);
  let refused = false;
  for (const host of options.hosts) {
    try {
      const result = await postOnce(host, body, options);
      if (result.ok) return result;
      refused = true;
    } catch {
      // Try the next host; a transport failure is not the hub's answer.
    }
  }
  return { ok: false, reason: refused ? "rejected" : "unreachable" };
}
