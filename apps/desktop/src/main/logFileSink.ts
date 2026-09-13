import { appendFileSync, mkdirSync, renameSync, statSync } from "node:fs";
import path from "node:path";
import { sanitizeLog } from "./logSanitize.ts";

export const LOG_FILE_NAME = "pangeavpn.log";
const DEFAULT_MAX_BYTES = 512 * 1024;
const LEVELS = ["log", "warn", "error"] as const;

type Level = (typeof LEVELS)[number];

export function formatLogLine(level: Level, parts: readonly unknown[], at = new Date()): string {
  const text = parts.map((part) => sanitizeLog(part)).join(" ");
  return `${at.toISOString()} [${level}] ${text}\n`;
}

/** Appends to a size-capped file, keeping one rollover. Never throws: a
 *  logging failure must not take the app with it. */
export function createLogWriter(
  dir: string,
  maxBytes = DEFAULT_MAX_BYTES
): (level: Level, parts: readonly unknown[]) => void {
  const file = path.join(dir, LOG_FILE_NAME);
  const rolled = `${file}.1`;
  let dirReady = false;

  return (level, parts) => {
    try {
      if (!dirReady) {
        mkdirSync(dir, { recursive: true });
        dirReady = true;
      }
      const line = formatLogLine(level, parts);
      let size = 0;
      try {
        size = statSync(file).size;
      } catch {
        size = 0;
      }
      if (size > 0 && size + Buffer.byteLength(line) > maxBytes) {
        renameSync(file, rolled);
      }
      appendFileSync(file, line);
    } catch {
      // Swallowed on purpose.
    }
  };
}

export function installConsoleFileSink(dir: string, maxBytes?: number): () => void {
  const write = createLogWriter(dir, maxBytes);
  const originals = LEVELS.map((level) => {
    const original = console[level].bind(console);
    console[level] = (...args: unknown[]) => {
      original(...args);
      write(level, args);
    };
    return [level, original] as const;
  });
  return () => {
    for (const [level, original] of originals) {
      console[level] = original;
    }
  };
}
