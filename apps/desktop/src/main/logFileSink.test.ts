import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, readFileSync, existsSync, symlinkSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import { createLogWriter, formatLogLine, installConsoleFileSink, LOG_FILE_NAME } from "./logFileSink.ts";

function tempDir(): string {
  return mkdtempSync(path.join(tmpdir(), "pangea-log-"));
}

test("a line carries the level and the sanitized text", () => {
  const line = formatLogLine("warn", ["token login failed:", "Authorization: Bearer abc.def"]);
  assert.match(line, /\[warn\] token login failed: Authorization: Bearer \[redacted\]\n$/);
  assert.equal(line.split("\n").length, 2);
});

test("newlines in untrusted text cannot forge extra log lines", () => {
  const line = formatLogLine("error", ["boom\n2020-01-01 [warn] forged"]);
  assert.equal(line.split("\n").length, 2);
});

test("writes land in the log file under the given directory", () => {
  const dir = tempDir();
  const write = createLogWriter(dir);
  write("log", ["hello"]);
  assert.match(readFileSync(path.join(dir, LOG_FILE_NAME), "utf8"), /\[log\] hello/);
});

test("the file is capped and keeps exactly one rollover", () => {
  const dir = tempDir();
  const write = createLogWriter(dir, 200);
  for (let i = 0; i < 40; i += 1) write("log", [`line ${i} padding padding padding`]);
  const file = path.join(dir, LOG_FILE_NAME);
  assert.ok(readFileSync(file, "utf8").length <= 200);
  assert.ok(existsSync(`${file}.1`));
  assert.ok(!existsSync(`${file}.2`));
});

// Skipped on Windows, which has no O_NOFOLLOW for Node to ask for.
test("a symlink left at the log path is not written through", { skip: process.platform === "win32" }, () => {
  const dir = tempDir();
  const outside = path.join(tempDir(), "outside.txt");
  writeFileSync(outside, "untouched");
  symlinkSync(outside, path.join(dir, LOG_FILE_NAME));

  const write = createLogWriter(dir);
  write("log", ["secret"]);

  assert.equal(readFileSync(outside, "utf8"), "untouched");
});

test("an unwritable directory does not throw", () => {
  const write = createLogWriter(path.join(tempDir(), "nested\0bad"));
  assert.doesNotThrow(() => write("error", ["still alive"]));
});

test("the original console behaviour survives the tee", () => {
  const dir = tempDir();
  const seen: unknown[][] = [];
  const originalWarn = console.warn;
  console.warn = (...args: unknown[]) => seen.push(args);
  const restore = installConsoleFileSink(dir);
  console.warn("tee'd");
  restore();
  console.warn = originalWarn;
  assert.deepEqual(seen, [["tee'd"]]);
  assert.match(readFileSync(path.join(dir, LOG_FILE_NAME), "utf8"), /\[warn\] tee'd/);
});
