import { promises as fs } from "node:fs";
import path from "node:path";

import type { DiagnosticsPayload } from "./diagnosticsReport.ts";

const KEEP = 5;

/** Writes the report beside the logs before it is uploaded: a hub that never
 *  answers then costs an attachment, not the evidence the report was built for. */
export async function spoolDiagnostics(payload: DiagnosticsPayload, dir: string): Promise<string> {
  await fs.mkdir(dir, { recursive: true, mode: 0o700 });
  const stamp = payload.createdAt.replace(/[:.]/g, "-");
  const file = path.join(dir, `diagnostics-${stamp}.json`);
  await fs.writeFile(file, JSON.stringify(payload, null, 2), { mode: 0o600 });
  const kept = (await fs.readdir(dir))
    .filter((name) => name.startsWith("diagnostics-") && name.endsWith(".json"))
    .sort();
  for (const name of kept.slice(0, Math.max(0, kept.length - KEEP))) {
    await fs.rm(path.join(dir, name), { force: true });
  }
  return file;
}
