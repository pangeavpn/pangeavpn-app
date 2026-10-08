import { closeSync, constants, fstatSync, ftruncateSync, mkdirSync, openSync, readSync, writeSync } from "node:fs";
import path from "node:path";
import { sanitizeLog } from "./logSanitize.ts";

export const LOG_FILE_NAME = "pangeavpn.log";
// O_NOFOLLOW refuses a symlink left in the log directory. Windows has no
// equivalent for Node to ask for, so there it contributes nothing.
const NO_FOLLOW = constants.O_NOFOLLOW ?? 0;
// No O_APPEND: Windows opens an appending file without the right to truncate it.
const OPEN_FLAGS = constants.O_RDWR | constants.O_CREAT | NO_FOLLOW;
const ROLL_FLAGS = constants.O_WRONLY | constants.O_CREAT | constants.O_TRUNC | NO_FOLLOW;
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
    let fd = -1;
    try {
      if (!dirReady) {
        mkdirSync(dir, { recursive: true });
        dirReady = true;
      }
      const line = formatLogLine(level, parts);
      fd = openSync(file, OPEN_FLAGS);
      // Measured and rolled through the descriptor being written to, never the
      // path again, so nothing can swap the file for another one in between.
      const { size } = fstatSync(fd);
      let at = size;
      if (size > 0 && size + Buffer.byteLength(line) > maxBytes) {
        rollOver(fd, size, rolled);
        at = 0;
      }
      writeSync(fd, line, at);
    } catch {
      // Swallowed on purpose.
    } finally {
      if (fd !== -1) {
        try {
          closeSync(fd);
        } catch {
          // A throw here would escape the catch above.
        }
      }
    }
  };
}

/** Copies the open log into the rollover file, then empties it in place. */
function rollOver(fd: number, size: number, rolled: string): void {
  const kept = Buffer.alloc(size);
  const read = readSync(fd, kept, 0, size, 0);
  const out = openSync(rolled, ROLL_FLAGS);
  try {
    writeSync(out, kept, 0, read);
  } finally {
    closeSync(out);
  }
  ftruncateSync(fd, 0);
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
