import fs from "node:fs/promises";
import path from "node:path";
import { redactDiagnostics } from "./diagnosticsRedact.ts";

/** Tail kept per log file. The frontend sink rolls at 512kb, so 64kb is a few
 *  thousand recent lines: enough for one failed session, small enough to post. */
export const SECTION_MAX_BYTES = 64 * 1024;
/** Hub body limit is 1mb and JSON escaping inflates text, so half is the cap. */
export const TOTAL_MAX_BYTES = 512 * 1024;
const MAX_NOTE_CHARS = 500;
const MAX_CRASH_ENTRIES = 40;

export interface DiagnosticsSection {
  name: string;
  bytes: number;
  text: string;
}

export interface DiagnosticsPayload {
  appVersion: string;
  platform: string;
  arch: string;
  osRelease: string;
  createdAt: string;
  note?: string;
  sections: DiagnosticsSection[];
}

export interface DiagnosticsSources {
  appVersion: string;
  platform: string;
  arch: string;
  osRelease: string;
  logDir: string;
  appSupportDir: string;
  crashDumpsDir: string;
  logFileName: string;
  /** The daemon's in-memory log ring, fetched over its API: on Windows the
   *  service's file log is admin-only, so this is the only daemon log a report can carry. */
  daemonRing?: () => Promise<DaemonRingEntry[]>;
  /** Security products, VPN services, adapters and firewall profiles: where a
   *  probe reply can vanish outside anything the daemon itself can log. */
  hostSnapshot?: () => Promise<string>;
  note?: string;
  now?: Date;
}

export interface DaemonRingEntry {
  ts: number;
  level: string;
  source: string;
  msg: string;
}

export function formatDaemonRing(entries: readonly DaemonRingEntry[]): string {
  return entries
    .map((entry) => `${new Date(entry.ts).toISOString()} [${entry.level}] [${entry.source}] ${entry.msg}`)
    .join("\n");
}

function tailText(text: string, maxBytes: number): string {
  const size = Buffer.byteLength(text);
  if (size <= maxBytes) return text;
  const kept = Buffer.from(text, "utf8").subarray(size - maxBytes).toString("utf8");
  return `[...truncated, showing last ${Buffer.byteLength(kept)} bytes of ${size}]\n${kept}`;
}

async function daemonRingSection(fetchRing: DiagnosticsSources["daemonRing"]): Promise<DiagnosticsSection> {
  const name = "daemon-ring";
  if (!fetchRing) {
    const text = `<${name} not collected on this platform>`;
    return { name, bytes: Buffer.byteLength(text), text };
  }
  try {
    const ring = await fetchRing();
    const text = ring.length === 0
      ? `<${name} empty>`
      : redactDiagnostics(tailText(formatDaemonRing(ring), SECTION_MAX_BYTES));
    return { name, bytes: Buffer.byteLength(text), text };
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    const text = `<${name} unavailable: ${redactDiagnostics(reason)}>`;
    return { name, bytes: Buffer.byteLength(text), text };
  }
}

async function hostSection(snapshot: DiagnosticsSources["hostSnapshot"]): Promise<DiagnosticsSection> {
  const name = "host";
  if (!snapshot) {
    const text = `<${name} not collected on this platform>`;
    return { name, bytes: Buffer.byteLength(text), text };
  }
  try {
    const text = redactDiagnostics(tailText(await snapshot(), SECTION_MAX_BYTES));
    return { name, bytes: Buffer.byteLength(text), text };
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    const text = `<${name} unavailable: ${redactDiagnostics(reason)}>`;
    return { name, bytes: Buffer.byteLength(text), text };
  }
}

function describeError(error: unknown): string {
  const code = typeof error === "object" && error !== null && "code" in error
    ? String((error as { code?: unknown }).code)
    : "";
  if (code === "ENOENT") return "not present";
  if (code === "EACCES" || code === "EPERM") return "unreadable (permission denied; this directory is owned by the elevated daemon)";
  if (code) return `unreadable (${code})`;
  return "unreadable";
}

async function readTail(filePath: string, maxBytes: number): Promise<string> {
  const handle = await fs.open(filePath, "r");
  try {
    const { size } = await handle.stat();
    const length = Math.min(size, maxBytes);
    const buffer = Buffer.alloc(length);
    await handle.read(buffer, 0, length, size - length);
    const text = buffer.toString("utf8");
    return size > length ? `[...truncated, showing last ${length} bytes of ${size}]\n${text}` : text;
  } finally {
    await handle.close();
  }
}

async function fileSection(name: string, filePath: string): Promise<DiagnosticsSection> {
  try {
    const text = redactDiagnostics(await readTail(filePath, SECTION_MAX_BYTES));
    return { name, bytes: Buffer.byteLength(text), text };
  } catch (error) {
    const text = `<${name} ${describeError(error)}>`;
    return { name, bytes: Buffer.byteLength(text), text };
  }
}

/** Names, sizes and times only. A minidump is an unredactable memory image. */
async function crashSection(dir: string): Promise<DiagnosticsSection> {
  const name = "crash-dumps";
  try {
    const entries = await fs.readdir(dir, { withFileTypes: true });
    const files = entries.filter((entry) => entry.isFile()).slice(0, MAX_CRASH_ENTRIES);
    if (files.length === 0) {
      const text = "<no crash dumps>";
      return { name, bytes: Buffer.byteLength(text), text };
    }
    const lines: string[] = [];
    for (const entry of files) {
      try {
        const info = await fs.stat(path.join(dir, entry.name));
        lines.push(`${entry.name}\t${info.size} bytes\t${info.mtime.toISOString()}`);
      } catch (error) {
        lines.push(`${entry.name}\t<${describeError(error)}>`);
      }
    }
    const text = lines.join("\n");
    return { name, bytes: Buffer.byteLength(text), text };
  } catch (error) {
    const text = `<${name} ${describeError(error)}>`;
    return { name, bytes: Buffer.byteLength(text), text };
  }
}

function capTotal(sections: DiagnosticsSection[], budget: number): DiagnosticsSection[] {
  let remaining = budget;
  return sections.map((section) => {
    if (section.bytes <= remaining) {
      remaining -= section.bytes;
      return section;
    }
    if (remaining < 256) {
      const text = `<${section.name} omitted: payload size limit reached>`;
      remaining = 0;
      return { name: section.name, bytes: Buffer.byteLength(text), text };
    }
    const kept = Buffer.from(section.text, "utf8").subarray(section.bytes - remaining).toString("utf8");
    const text = `[...truncated to fit the report]\n${kept}`;
    remaining = 0;
    return { name: section.name, bytes: Buffer.byteLength(text), text };
  });
}

export function normalizeNote(note: string | undefined): string | undefined {
  const trimmed = (note ?? "").trim();
  if (trimmed.length === 0) return undefined;
  return redactDiagnostics(trimmed.slice(0, MAX_NOTE_CHARS));
}

export async function collectDiagnostics(sources: DiagnosticsSources): Promise<DiagnosticsPayload> {
  const sections = [
    await fileSection("app.log", path.join(sources.logDir, sources.logFileName)),
    await fileSection("app.log.1", path.join(sources.logDir, `${sources.logFileName}.1`)),
    await fileSection("daemon.log", path.join(sources.appSupportDir, "daemon.log")),
    await fileSection("daemon-elevated.log", path.join(sources.appSupportDir, "daemon-elevated.log")),
    await daemonRingSection(sources.daemonRing),
    await hostSection(sources.hostSnapshot),
    await crashSection(sources.crashDumpsDir)
  ];

  const payload: DiagnosticsPayload = {
    appVersion: sources.appVersion,
    platform: sources.platform,
    arch: sources.arch,
    osRelease: sources.osRelease,
    createdAt: (sources.now ?? new Date()).toISOString(),
    sections: capTotal(sections, TOTAL_MAX_BYTES)
  };
  const note = normalizeNote(sources.note);
  if (note) payload.note = note;
  return payload;
}
