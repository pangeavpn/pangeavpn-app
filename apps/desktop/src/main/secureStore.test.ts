import assert from "node:assert/strict";
import test from "node:test";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { readSecret, writeSecret } from "./secureStore.ts";

async function tempDir(): Promise<string> {
  return await fs.mkdtemp(path.join(os.tmpdir(), "pangea-secure-"));
}

test("writeSecret round-trips through readSecret", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "session.dat");

  await writeSecret(file, JSON.stringify({ token: "abc123" }));

  assert.equal(await readSecret(file), JSON.stringify({ token: "abc123" }));
});

test("writeSecret never leaves the plaintext on disk", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "license.dat");

  await writeSecret(file, "PANGEA-SECRET-KEY");

  const raw = await fs.readFile(file);
  assert.ok(!raw.toString("utf8").includes("PANGEA-SECRET-KEY"));
});

test("secrets in one directory share a single key file", async () => {
  const dir = await tempDir();
  const first = path.join(dir, "a.dat");
  const second = path.join(dir, "b.dat");

  await writeSecret(first, "one");
  await writeSecret(second, "two");

  assert.equal(await readSecret(first), "one");
  assert.equal(await readSecret(second), "two");
});

test("readSecret returns null when the file is missing", async () => {
  const dir = await tempDir();
  assert.equal(await readSecret(path.join(dir, "nope.dat")), null);
});

test("readSecret still reads a legacy plaintext file", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "legacy.dat");
  await fs.writeFile(file, "plain-token", { mode: 0o600 });

  assert.equal(await readSecret(file), "plain-token");
});

test("readSecret rejects a tampered ciphertext", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "tampered.dat");
  await writeSecret(file, "sensitive");

  const raw = await fs.readFile(file);
  raw[raw.length - 1] ^= 0xff;
  await fs.writeFile(file, raw);

  assert.equal(await readSecret(file), null);
});
