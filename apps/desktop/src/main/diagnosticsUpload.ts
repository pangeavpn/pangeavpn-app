import type { DiagnosticsPayload } from "./diagnosticsReport.ts";

export const DIAGNOSTICS_ROUTE = "/api/client/diagnostics";
const DEFAULT_TIMEOUT_MS = 30000;
const REPORT_CODE = /^[A-Z0-9-]{4,32}$/;

export type DiagnosticsUploadResult =
  | { ok: true; reportCode: string }
  | { ok: false; reason: "unreachable" | "rejected" };

export interface DiagnosticsUploadOptions {
  /** A sealed, anonymous POST over the hub path the app has resolved: the
   *  report never goes out as a bare request the way nothing else in the app does. */
  send: (route: string, payload: DiagnosticsPayload, signal: AbortSignal) => Promise<{ status: number; body: unknown }>;
  timeoutMs?: number;
}

export async function uploadDiagnostics(
  payload: DiagnosticsPayload,
  options: DiagnosticsUploadOptions
): Promise<DiagnosticsUploadResult> {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), options.timeoutMs ?? DEFAULT_TIMEOUT_MS);
  try {
    const { status, body } = await options.send(DIAGNOSTICS_ROUTE, payload, controller.signal);
    if (status < 200 || status >= 300) {
      console.warn(`diagnostics: the hub refused the report (${status})`);
      return { ok: false, reason: "rejected" };
    }
    const reportCode = typeof body === "object" && body !== null && "reportCode" in body ? body.reportCode : undefined;
    const code = typeof reportCode === "string" ? reportCode.trim() : "";
    if (!REPORT_CODE.test(code)) {
      console.warn("diagnostics: the hub answered without a usable report code");
      return { ok: false, reason: "rejected" };
    }
    return { ok: true, reportCode: code };
  } catch (error) {
    // A transport failure is not the hub's answer; the path cascade already
    // tried every way the app knows to reach it.
    console.warn(`diagnostics: hub unreachable: ${error instanceof Error ? error.message : String(error)}`);
    return { ok: false, reason: "unreachable" };
  } finally {
    clearTimeout(timer);
  }
}
