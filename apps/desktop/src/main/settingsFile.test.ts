import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { readSettings, writeSettings } from "./settingsFile.ts";

async function tempDir(): Promise<string> {
  return fs.mkdtemp(path.join(os.tmpdir(), "pangea-settings-"));
}

test("reads the user copy and ignores the legacy one", async () => {
  const dir = await tempDir();
  const primary = path.join(dir, "settings.json");
  const legacy = path.join(dir, "legacy.json");
  await fs.writeFile(primary, JSON.stringify({ lockdown: true, locale: "fr" }));
  await fs.writeFile(legacy, JSON.stringify({ lockdown: false }));

  assert.deepEqual(await readSettings({ primary, legacy }), { lockdown: true, locale: "fr" });
});

test("migrates the legacy copy when no user copy exists yet", async () => {
  const dir = await tempDir();
  const legacy = path.join(dir, "legacy.json");
  await fs.writeFile(legacy, JSON.stringify({ locale: "de" }));

  assert.deepEqual(await readSettings({ primary: path.join(dir, "settings.json"), legacy }), { locale: "de" });
});

test("a fresh install with nothing on disk reads as empty", async () => {
  const dir = await tempDir();
  assert.deepEqual(await readSettings({ primary: path.join(dir, "settings.json"), legacy: null }), {});
});

// Regression: an unreadable settings.json used to read as "no settings yet",
// and the caller's write-back then replaced every value with a default.
test("an unreadable settings file is never reported as empty", async () => {
  const dir = await tempDir();
  const primary = path.join(dir, "settings.json");
  await fs.mkdir(primary);

  await assert.rejects(readSettings({ primary, legacy: null }));
  await assert.rejects(readSettings({ primary, legacy: path.join(dir, "legacy.json") }));
});

test("an unreadable legacy file leaves the user copy authoritative", async () => {
  const dir = await tempDir();
  const primary = path.join(dir, "settings.json");
  const legacy = path.join(dir, "legacy.json");
  await fs.mkdir(legacy);
  await fs.writeFile(primary, JSON.stringify({ locale: "es" }));

  assert.deepEqual(await readSettings({ primary, legacy }), { locale: "es" });
});

test("a corrupt settings file is preserved, not overwritten", async () => {
  const dir = await tempDir();
  const primary = path.join(dir, "settings.json");
  await fs.writeFile(primary, "{ not json");

  assert.deepEqual(await readSettings({ primary, legacy: null }), {});
  const kept = (await fs.readdir(dir)).filter((name) => name.includes(".corrupt-"));
  assert.equal(kept.length, 1);
});

test("writeSettings replaces the file atomically and leaves no temp behind", async () => {
  const dir = await tempDir();
  const primary = path.join(dir, "nested", "settings.json");

  await writeSettings(primary, { locale: "it" });
  await writeSettings(primary, { locale: "it", lockdown: true });

  assert.deepEqual(JSON.parse(await fs.readFile(primary, "utf8")), { locale: "it", lockdown: true });
  assert.deepEqual(await fs.readdir(path.dirname(primary)), ["settings.json"]);
});
