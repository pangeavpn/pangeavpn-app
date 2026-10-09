import assert from "node:assert/strict";
import test from "node:test";
import { mkdirSync, mkdtempSync, readFileSync, existsSync, symlinkSync, writeFileSync } from "node:fs";
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

test("a rollover moves the old lines to .1 and starts the log afresh", () => {
  const dir = tempDir();
  const write = createLogWriter(dir, 200);
  write("log", [`first ${"x".repeat(120)}`]);
  write("log", [`second ${"y".repeat(120)}`]);
  const file = path.join(dir, LOG_FILE_NAME);
  const rolled = readFileSync(`${file}.1`, "utf8");
  const current = readFileSync(file, "utf8");
  assert.match(rolled, /first x+/);
  assert.doesNotMatch(rolled, /second/);
  assert.match(current, /second y+/);
  assert.doesNotMatch(current, /first/);
});

test("a rollover file that cannot be written costs the old lines, not the log", () => {
  const dir = tempDir();
  const file = path.join(dir, LOG_FILE_NAME);
  mkdirSync(`${file}.1`);
  const write = createLogWriter(dir, 200);
  write("log", [`first ${"x".repeat(120)}`]);
  write("log", [`second ${"y".repeat(120)}`]);
  write("log", ["third"]);
  const current = readFileSync(file, "utf8");
  assert.match(current, /second y+/);
  assert.match(current, /third/);
  assert.doesNotMatch(current, /first/);
});

test("an oversized log from an older build rolls over its tail only", () => {
  const dir = tempDir();
  const file = path.join(dir, LOG_FILE_NAME);
  writeFileSync(file, `${"old ".repeat(2000)}tail-marker\n`);
  const write = createLogWriter(dir, 200);
  write("log", ["fresh"]);
  const rolled = readFileSync(`${file}.1`, "utf8");
  assert.ok(rolled.length <= 200, `rolled ${rolled.length} bytes`);
  assert.match(rolled, /tail-marker/);
  assert.match(readFileSync(file, "utf8"), /fresh/);
});

test("a symlink left at the rollover path is not written through", { skip: process.platform === "win32" }, () => {
  const dir = tempDir();
  const outside = path.join(tempDir(), "outside.txt");
  writeFileSync(outside, "untouched");
  symlinkSync(outside, path.join(dir, `${LOG_FILE_NAME}.1`));

  const write = createLogWriter(dir, 200);
  write("log", [`first ${"x".repeat(120)}`]);
  write("log", [`second ${"y".repeat(120)}`]);

  assert.equal(readFileSync(outside, "utf8"), "untouched");
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
