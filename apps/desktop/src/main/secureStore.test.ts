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

test("readSecret rejects a managed file replaced wholesale with plaintext", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "auth-session.dat");
  await writeSecret(file, JSON.stringify({ user: { id: "real" }, vpnAccessToken: "real-token" }));

  await fs.writeFile(file, JSON.stringify({ user: { id: "attacker" }, vpnAccessToken: "forged" }));

  assert.equal(await readSecret(file), null);
});

test("readSecret migrates legacy plaintext once, then rejects future plaintext", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "legacy.dat");
  await fs.writeFile(file, "plain-token", { mode: 0o600 });

  assert.equal(await readSecret(file), "plain-token");

  await fs.writeFile(file, "forged-token", { mode: 0o600 });
  assert.equal(await readSecret(file), null);
});

test("readSecret does not mint a new key when storage-key.bin is missing", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "session.dat");
  await writeSecret(file, "sensitive");

  await fs.rm(path.join(dir, "storage-key.bin"), { force: true });

  assert.equal(await readSecret(file), null);
  assert.equal(
    await fs.access(path.join(dir, "storage-key.bin")).then(() => true).catch(() => false),
    false
  );
});

test("writeSecret self-heals a corrupt storage key instead of failing forever", async () => {
  const dir = await tempDir();
  const keyFile = path.join(dir, "storage-key.bin");
  await fs.mkdir(dir, { recursive: true });
  await fs.writeFile(keyFile, Buffer.from("too-short"));

  const file = path.join(dir, "session.dat");
  await writeSecret(file, "value-after-heal");

  assert.equal(await readSecret(file), "value-after-heal");
});

test("readSecret discards a keychain-encrypted file instead of prompting", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "session.dat");
  await fs.writeFile(file, Buffer.concat([Buffer.from("v10", "latin1"), Buffer.from("opaque")]), { mode: 0o600 });

  assert.equal(await readSecret(file), null);
  await assert.rejects(fs.readFile(file));
});

test("a discarded keychain file cannot be replaced with plaintext", async () => {
  const dir = await tempDir();
  const file = path.join(dir, "license.dat");
  await fs.writeFile(file, Buffer.from("v11opaque", "latin1"), { mode: 0o600 });
  await readSecret(file);

  await fs.writeFile(file, "forged-key", { mode: 0o600 });
  assert.equal(await readSecret(file), null);
});
