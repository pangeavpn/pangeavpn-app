import fs from "node:fs/promises";
import path from "node:path";
import { redactDiagnostics } from "./diagnosticsRedact.ts";

/** Tail kept per log file. The frontend sink rolls at 512kb, so 64kb is a few
 *  thousand recent lines: enough for one failed session, small enough to post. */
export const SECTION_MAX_BYTES = 64 * 1024;
/** Hub body limit is 1mb and JSON escaping inflates text, so half is the cap. */
export const TOTAL_MAX_BYTES = 512 * 1024;
const MAX_NOTE_CHARS = 500;
const CRASH_SECTION_MAX_BYTES = 16 * 1024;
const NOTE_SCAN_CHARS = 2000;
const REDACTED_HEAD = "[redacted:";
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
  /** The daemon's in-memory log ring, fetched over its API: it covers this boot
   *  only, and dies with the process. */
  daemonRing?: () => Promise<DaemonRingEntry[]>;
  /** The daemon's own log file, read by the daemon: a service install's file is
   *  admin-only here, so this is the only way a report carries pre-boot history. */
  daemonFileLog?: () => Promise<DaemonRingEntry[]>;
  /** The daemon's raw stderr log, which is where a panic or fatal fault lands. */
  daemonCrashLog?: () => Promise<string>;
  /** GET /status: build identity, process start and the session's transport state. */
  daemonStatus?: () => Promise<unknown>;
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

function entriesSection(name: string, entries: readonly DaemonRingEntry[]): DiagnosticsSection {
  const text = entries.length === 0
    ? `<${name} empty>`
    : redactDiagnostics(tailText(formatDaemonRing(entries), SECTION_MAX_BYTES));
  return { name, bytes: Buffer.byteLength(text), text };
}

function textSection(name: string, text: string, maxBytes = SECTION_MAX_BYTES): DiagnosticsSection {
  const kept = redactDiagnostics(tailText(text, maxBytes));
  return { name, bytes: Buffer.byteLength(kept), text: kept };
}

async function daemonLogSection(sources: DiagnosticsSources): Promise<DiagnosticsSection> {
  const name = "daemon.log";
  if (sources.daemonFileLog) {
    try {
      return entriesSection(name, await sources.daemonFileLog());
    } catch {
      // An older daemon has no /logs/file; the files below still may.
    }
  }
  // The service daemon writes under logs/, where this process is usually denied;
  // the flat path is only the desktop-spawned daemon's stderr.
  const candidates = [
    path.join(sources.appSupportDir, "logs", name),
    path.join(sources.appSupportDir, name)
  ];
  let failure: unknown;
  for (const candidate of candidates) {
    try {
      return textSection(name, await readTail(candidate, SECTION_MAX_BYTES));
    } catch (error) {
      failure ??= error;
    }
  }
  const text = `<${name} ${describeError(failure ?? new Error("unreadable"))}>`;
  return { name, bytes: Buffer.byteLength(text), text };
}

async function daemonCrashSection(sources: DiagnosticsSources): Promise<DiagnosticsSection> {
  const name = "daemon-crash.log";
  const missing = "<no daemon crash log>";
  let failure: unknown;
  if (sources.daemonCrashLog) {
    try {
      const raw = await sources.daemonCrashLog();
      if (raw.trim().length > 0) return textSection(name, raw, CRASH_SECTION_MAX_BYTES);
      return { name, bytes: Buffer.byteLength(missing), text: missing };
    } catch (error) {
      failure = error;
    }
  }
  try {
    const raw = await readTail(path.join(sources.appSupportDir, "logs", name), CRASH_SECTION_MAX_BYTES);
    if (raw.trim().length > 0) return textSection(name, raw, CRASH_SECTION_MAX_BYTES);
    return { name, bytes: Buffer.byteLength(missing), text: missing };
  } catch (error) {
    failure ??= error;
  }
  if ((failure as { code?: string } | undefined)?.code === "ENOENT") {
    return { name, bytes: Buffer.byteLength(missing), text: missing };
  }
  const text = `<${name} ${describeError(failure ?? new Error("unreadable"))}>`;
  return { name, bytes: Buffer.byteLength(text), text };
}

async function daemonStatusSection(status: DiagnosticsSources["daemonStatus"]): Promise<DiagnosticsSection> {
  const name = "daemon-status";
  if (!status) {
    const text = `<${name} not collected>`;
    return { name, bytes: Buffer.byteLength(text), text };
  }
  try {
    return textSection(name, JSON.stringify(await status(), null, 2));
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

/** Redacts the whole note before keeping its first 500 code points, so a cut
 *  never leaves part of a secret behind. */
export function normalizeNote(note: string | undefined): string | undefined {
  const trimmed = (note ?? "").trim();
  if (trimmed.length === 0) return undefined;
  // Anything past the scan window is dropped before the cut anyway, and bounding it keeps
  // the email rule (quadratic on long runs) cheap whatever length reaches the handler.
  const scanned = Array.from(trimmed).slice(0, NOTE_SCAN_CHARS).join("");
  const redacted = redactDiagnostics(scanned);
  const points = Array.from(redacted);
  if (points.length <= MAX_NOTE_CHARS) return redacted;
  return dropCutMarker(points.slice(0, MAX_NOTE_CHARS).join("")).trimEnd() || undefined;
}

/** Removes a [redacted:...] marker the cut ran through. */
function dropCutMarker(cut: string): string {
  const open = cut.lastIndexOf("[");
  if (open < 0 || cut.includes("]", open)) return cut;
  const tail = cut.slice(open);
  return tail.startsWith(REDACTED_HEAD) || REDACTED_HEAD.startsWith(tail) ? cut.slice(0, open) : cut;
}

export async function collectDiagnostics(sources: DiagnosticsSources): Promise<DiagnosticsPayload> {
  // Small and hard-won sections come first: capTotal truncates from the end, and
  // a log file is the cheapest thing to lose.
  const sections = [
    await daemonStatusSection(sources.daemonStatus),
    await hostSection(sources.hostSnapshot),
    await daemonRingSection(sources.daemonRing),
    await daemonLogSection(sources),
    await daemonCrashSection(sources),
    await fileSection("daemon-elevated.log", path.join(sources.appSupportDir, "daemon-elevated.log")),
    await fileSection("app.log", path.join(sources.logDir, sources.logFileName)),
    await fileSection("app.log.1", path.join(sources.logDir, `${sources.logFileName}.1`)),
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
