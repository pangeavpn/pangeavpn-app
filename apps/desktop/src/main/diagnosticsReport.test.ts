import assert from "node:assert/strict";
import test from "node:test";
import { mkdtempSync, mkdirSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import path from "node:path";
import {
  SECTION_MAX_BYTES,
  TOTAL_MAX_BYTES,
  collectDiagnostics,
  normalizeNote,
  type DiagnosticsSources
} from "./diagnosticsReport.ts";

function scratch(): string {
  return mkdtempSync(path.join(tmpdir(), "pangea-diag-"));
}

function sources(over: Partial<DiagnosticsSources> = {}): DiagnosticsSources {
  const root = scratch();
  return {
    appVersion: "0.7.1",
    platform: "darwin",
    arch: "arm64",
    osRelease: "24.5.0",
    logDir: root,
    appSupportDir: root,
    crashDumpsDir: path.join(root, "Crashpad"),
    logFileName: "pangeavpn.log",
    now: new Date("2026-09-13T10:00:00.000Z"),
    ...over
  };
}

function section(payload: { sections: { name: string; text: string }[] }, name: string): string {
  const found = payload.sections.find((s) => s.name === name);
  assert.ok(found, `missing section ${name}`);
  return found.text;
}

test("carries the environment fields the hub contract names", async () => {
  const payload = await collectDiagnostics(sources());
  assert.equal(payload.appVersion, "0.7.1");
  assert.equal(payload.platform, "darwin");
  assert.equal(payload.arch, "arm64");
  assert.equal(payload.osRelease, "24.5.0");
  assert.equal(payload.createdAt, "2026-09-13T10:00:00.000Z");
  assert.equal(payload.note, undefined);
});

test("reads the frontend log and its single rollover, redacted", async () => {
  const src = sources();
  writeFileSync(path.join(src.logDir, "pangeavpn.log"), "connect failed for 8.8.8.8\n");
  writeFileSync(path.join(src.logDir, "pangeavpn.log.1"), "older line ok\n");
  const payload = await collectDiagnostics(src);
  assert.match(section(payload, "app.log"), /connect failed for \[redacted:ipv4\]/);
  assert.match(section(payload, "app.log.1"), /older line ok/);
});

test("a missing or unreadable file becomes a section that says so, never a throw", async () => {
  const payload = await collectDiagnostics(sources());
  assert.match(section(payload, "daemon.log"), /^<daemon\.log not present>$/);
  assert.match(section(payload, "daemon-elevated.log"), /not present/);
  assert.match(section(payload, "crash-dumps"), /not present/);
});

test("crash dumps contribute names, sizes and times but never their bytes", async () => {
  const src = sources();
  mkdirSync(src.crashDumpsDir, { recursive: true });
  writeFileSync(path.join(src.crashDumpsDir, "9f2c.dmp"), Buffer.from([0x4d, 0x44, 0x4d, 0x50, 0x00]));
  const text = section(await collectDiagnostics(src), "crash-dumps");
  assert.match(text, /9f2c\.dmp\t5 bytes\t\d{4}-/);
  assert.ok(!text.includes("MDMP"));
});

test("each section is capped to its own tail", async () => {
  const src = sources();
  writeFileSync(path.join(src.logDir, "pangeavpn.log"), "x".repeat(SECTION_MAX_BYTES * 2));
  const text = section(await collectDiagnostics(src), "app.log");
  assert.match(text, /^\[\.\.\.truncated, showing last \d+ bytes of \d+\]/);
  assert.ok(Buffer.byteLength(text) < SECTION_MAX_BYTES + 200);
});

test("the whole payload stays under the total budget", async () => {
  const src = sources();
  writeFileSync(path.join(src.logDir, "pangeavpn.log"), "a".repeat(SECTION_MAX_BYTES));
  writeFileSync(path.join(src.logDir, "pangeavpn.log.1"), "b".repeat(SECTION_MAX_BYTES));
  writeFileSync(path.join(src.appSupportDir, "daemon.log"), "c".repeat(SECTION_MAX_BYTES));
  writeFileSync(path.join(src.appSupportDir, "daemon-elevated.log"), "d".repeat(SECTION_MAX_BYTES));
  const payload = await collectDiagnostics(src);
  const total = payload.sections.reduce((sum, s) => sum + s.bytes, 0);
  assert.ok(total <= TOTAL_MAX_BYTES, `total ${total}`);
  for (const s of payload.sections) {
    assert.equal(s.bytes, Buffer.byteLength(s.text));
  }
});

test("the user note is trimmed, length-capped and redacted", async () => {
  assert.equal(normalizeNote("   "), undefined);
  assert.equal(normalizeNote(undefined), undefined);
  assert.equal(normalizeNote(" cannot sign in "), "cannot sign in");
  assert.equal(normalizeNote("mail me at dana@example.com"), "mail me at [redacted:email]");
  assert.equal(normalizeNote("z".repeat(900))?.length, 500);
  const payload = await collectDiagnostics(sources({ note: "stuck on connecting" }));
  assert.equal(payload.note, "stuck on connecting");
});
