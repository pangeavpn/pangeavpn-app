import crypto from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

const MAGIC = "PGV1";
const KEY_FILE = "storage-key.bin";
const KEY_BYTES = 32;
const IV_BYTES = 12;
const TAG_BYTES = 16;
const MANIFEST_FILE = ".secure-manifest.json";

interface SafeStorageLike {
  isEncryptionAvailable(): boolean;
  encryptString(plainText: string): Buffer;
  decryptString(encrypted: Buffer): string;
}

function getSafeStorage(): SafeStorageLike | null {
  try {
    const electron = require("electron") as { safeStorage?: SafeStorageLike };
    return electron?.safeStorage ?? null;
  } catch {
    return null;
  }
}

function keychainUsable(): boolean {
  const storage = getSafeStorage();
  return storage ? storage.isEncryptionAvailable() : false;
}

function looksSafeStorageEncrypted(data: Buffer): boolean {
  const prefix = data.subarray(0, 3).toString("latin1");
  if (prefix === "v10" || prefix === "v11") return true;
  return data.length > 4 && data[0] === 0x01 && data[1] === 0x00 && data[2] === 0x00 && data[3] === 0x00;
}

// Per-path write queues so concurrent writeSecret calls on the same file
// (e.g. saveSession racing a re-login) serialize instead of interleaving.
const writeQueues = new Map<string, Promise<unknown>>();

function enqueueWrite<T>(filePath: string, task: () => Promise<T>): Promise<T> {
  const prior = writeQueues.get(filePath) ?? Promise.resolve();
  const next = prior.then(task, task);
  writeQueues.set(
    filePath,
    next.catch(() => undefined)
  );
  return next;
}

async function writeFileAtomic(filePath: string, data: Buffer | string, mode: number): Promise<void> {
  const tmpPath = path.join(path.dirname(filePath), `.${path.basename(filePath)}.${process.pid}.${Date.now()}.tmp`);
  await fs.writeFile(tmpPath, data, { mode });
  await fs.rename(tmpPath, filePath);
}

async function loadManifest(dir: string): Promise<Set<string>> {
  try {
    const raw = await fs.readFile(path.join(dir, MANIFEST_FILE), "utf8");
    const names = JSON.parse(raw) as unknown;
    return Array.isArray(names) ? new Set(names.filter((n) => typeof n === "string")) : new Set();
  } catch {
    return new Set();
  }
}

// Marks a file as "known encrypted" so a future plaintext replacement is
// rejected instead of trusted as an unmigrated legacy file.
async function markManaged(dir: string, basename: string): Promise<void> {
  const manifestPath = path.join(dir, MANIFEST_FILE);
  const manifest = await loadManifest(dir);
  if (manifest.has(basename)) return;
  manifest.add(basename);
  await fs.mkdir(dir, { recursive: true, mode: 0o700 });
  await writeFileAtomic(manifestPath, JSON.stringify([...manifest]), 0o600);
}

async function loadKey(dir: string): Promise<Buffer | null> {
  try {
    const existing = await fs.readFile(path.join(dir, KEY_FILE));
    return existing.length === KEY_BYTES ? existing : null;
  } catch {
    return null;
  }
}

// Only called from the write path: mints a key when missing, and self-heals
// a corrupt (wrong-length) key file instead of rejecting writes forever.
async function loadOrCreateKey(dir: string): Promise<Buffer> {
  const existing = await loadKey(dir);
  if (existing) return existing;

  await fs.mkdir(dir, { recursive: true, mode: 0o700 });
  const key = crypto.randomBytes(KEY_BYTES);
  await writeFileAtomic(path.join(dir, KEY_FILE), key, 0o600);
  return key;
}

function encryptLocal(key: Buffer, value: string): Buffer {
  const iv = crypto.randomBytes(IV_BYTES);
  const cipher = crypto.createCipheriv("aes-256-gcm", key, iv);
  const body = Buffer.concat([cipher.update(value, "utf8"), cipher.final()]);
  return Buffer.concat([Buffer.from(MAGIC, "latin1"), iv, cipher.getAuthTag(), body]);
}

function decryptLocal(key: Buffer, data: Buffer): string {
  const iv = data.subarray(MAGIC.length, MAGIC.length + IV_BYTES);
  const tag = data.subarray(MAGIC.length + IV_BYTES, MAGIC.length + IV_BYTES + TAG_BYTES);
  const body = data.subarray(MAGIC.length + IV_BYTES + TAG_BYTES);
  const decipher = crypto.createDecipheriv("aes-256-gcm", key, iv);
  decipher.setAuthTag(tag);
  return Buffer.concat([decipher.update(body), decipher.final()]).toString("utf8");
}

async function writeSecretNow(filePath: string, value: string): Promise<void> {
  const dir = path.dirname(filePath);
  await fs.mkdir(dir, { recursive: true, mode: 0o700 });

  if (keychainUsable()) {
    const storage = getSafeStorage();
    if (storage) {
      await writeFileAtomic(filePath, storage.encryptString(value), 0o600);
      await markManaged(dir, path.basename(filePath));
      return;
    }
  }

  const key = await loadOrCreateKey(dir);
  await writeFileAtomic(filePath, encryptLocal(key, value), 0o600);
  await markManaged(dir, path.basename(filePath));
}

/** Writes `value` encrypted at rest, creating the directory if needed. */
export async function writeSecret(filePath: string, value: string): Promise<void> {
  return enqueueWrite(filePath, () => writeSecretNow(filePath, value));
}

/** Reads a secret written by `writeSecret`, or null when it is missing or unreadable. */
export async function readSecret(filePath: string): Promise<string | null> {
  let data: Buffer;
  try {
    data = await fs.readFile(filePath);
  } catch {
    return null;
  }

  const dir = path.dirname(filePath);
  const basename = path.basename(filePath);

  if (data.subarray(0, MAGIC.length).toString("latin1") === MAGIC) {
    const key = await loadKey(dir);
    if (!key) return null;
    try {
      return decryptLocal(key, data);
    } catch {
      return null;
    }
  }

  // Written by an older build through the OS keychain: read it once, then
  // rewrite it in the current format so the keychain is never touched again.
  if (looksSafeStorageEncrypted(data)) {
    const storage = getSafeStorage();
    try {
      if (storage && storage.isEncryptionAvailable()) {
        const value = storage.decryptString(data);
        await writeSecret(filePath, value).catch(() => {});
        return value;
      }
    } catch {
      return null;
    }
    return null;
  }

  // Anything else is only trusted as pre-encryption legacy plaintext the
  // very first time we see it; once managed, a plaintext file is a forgery.
  const manifest = await loadManifest(dir);
  if (manifest.has(basename)) return null;

  const value = data.toString("utf8");
  await writeSecret(filePath, value).catch(() => {});
  return value;
}
