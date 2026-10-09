import assert from "node:assert/strict";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import test from "node:test";
import { DaemonNotReadyError, ensureRuntimeFiles } from "./runtimeFiles.ts";

const VALID_TOKEN = "a".repeat(64);

async function tempDir(): Promise<string> {
  return fs.mkdtemp(path.join(os.tmpdir(), "pangea-runtime-"));
}

async function exists(target: string): Promise<boolean> {
  try {
    await fs.stat(target);
    return true;
  } catch {
    return false;
  }
}

test("writes nothing into a directory the daemon owns", async () => {
  const dir = await tempDir();
  await fs.writeFile(path.join(dir, "daemon-token.txt"), `${VALID_TOKEN}\n`);

  await ensureRuntimeFiles(dir, { daemonOwnsDir: true });

  // Regression: creating config.json here meant an EPERM on the admin-only
  // ProgramData dir, which failed every daemon start and restart.
  assert.equal(await exists(path.join(dir, "config.json")), false);
  assert.deepEqual(await fs.readdir(dir), ["daemon-token.txt"]);
});

test("leaves the daemon's token untouched", async () => {
  const dir = await tempDir();
  const tokenPath = path.join(dir, "daemon-token.txt");
  await fs.writeFile(tokenPath, `${VALID_TOKEN}\n`);

  await ensureRuntimeFiles(dir, { daemonOwnsDir: true });

  assert.equal((await fs.readFile(tokenPath, "utf8")).trim(), VALID_TOKEN);
});

test("reports a daemon whose token is not written yet", async () => {
  const dir = await tempDir();

  await assert.rejects(
    ensureRuntimeFiles(dir, { daemonOwnsDir: true }),
    (err: unknown) => err instanceof DaemonNotReadyError
  );
  assert.equal(await exists(path.join(dir, "daemon-token.txt")), false);
});

test("reports a malformed token rather than replacing it", async () => {
  const dir = await tempDir();
  const tokenPath = path.join(dir, "daemon-token.txt");
  await fs.writeFile(tokenPath, "not-a-token\n");

  await assert.rejects(
    ensureRuntimeFiles(dir, { daemonOwnsDir: true }),
    (err: unknown) => err instanceof DaemonNotReadyError
  );
  assert.equal((await fs.readFile(tokenPath, "utf8")).trim(), "not-a-token");
});

test("surfaces a read failure that is not a missing file", async () => {
  const dir = await tempDir();
  // A directory where the token belongs: reading it fails with EISDIR, which
  // must not be mistaken for "the daemon has not started yet".
  await fs.mkdir(path.join(dir, "daemon-token.txt"));

  await assert.rejects(
    ensureRuntimeFiles(dir, { daemonOwnsDir: true }),
    (err: unknown) => !(err instanceof DaemonNotReadyError)
  );
});

test("creates both files when the desktop owns the directory", async () => {
  const dir = path.join(await tempDir(), "nested");

  await ensureRuntimeFiles(dir, { daemonOwnsDir: false });

  const token = (await fs.readFile(path.join(dir, "daemon-token.txt"), "utf8")).trim();
  assert.match(token, /^[0-9a-f]{64}$/);
  assert.deepEqual(JSON.parse(await fs.readFile(path.join(dir, "config.json"), "utf8")), {
    profiles: []
  });
});

test("replaces a malformed token when the desktop owns the directory", async () => {
  const dir = await tempDir();
  const tokenPath = path.join(dir, "daemon-token.txt");
  await fs.writeFile(tokenPath, "garbage\n");

  await ensureRuntimeFiles(dir, { daemonOwnsDir: false });

  assert.match((await fs.readFile(tokenPath, "utf8")).trim(), /^[0-9a-f]{64}$/);
});

test("keeps a valid config when the desktop owns the directory", async () => {
  const dir = await tempDir();
  const configPath = path.join(dir, "config.json");
  await fs.writeFile(configPath, JSON.stringify({ profiles: [{ id: "keep" }] }));

  await ensureRuntimeFiles(dir, { daemonOwnsDir: false });

  const config = JSON.parse(await fs.readFile(configPath, "utf8")) as { profiles: { id: string }[] };
  assert.equal(config.profiles[0]?.id, "keep");
});
