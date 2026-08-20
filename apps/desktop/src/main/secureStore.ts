import crypto from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

const MAGIC = "PGV1";
const KEY_FILE = "storage-key.bin";
const KEY_BYTES = 32;
const IV_BYTES = 12;
const TAG_BYTES = 16;

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

// macOS ties the keychain item to the app's ad-hoc signature, so every update
// makes it demand the login password. Keep our own key there instead.
function keychainUsable(): boolean {
  if (process.platform === "darwin") return false;
  const storage = getSafeStorage();
  return storage ? storage.isEncryptionAvailable() : false;
}

function looksSafeStorageEncrypted(data: Buffer): boolean {
  if (data.subarray(0, 3).toString("latin1") === "v10") return true;
  return data.length > 4 && data[0] === 0x01 && data[1] === 0x00 && data[2] === 0x00 && data[3] === 0x00;
}

async function loadOrCreateKey(dir: string): Promise<Buffer> {
  const keyPath = path.join(dir, KEY_FILE);
  try {
    const existing = await fs.readFile(keyPath);
    if (existing.length === KEY_BYTES) return existing;
  } catch {
    // no key yet
  }

  await fs.mkdir(dir, { recursive: true, mode: 0o700 });
  const key = crypto.randomBytes(KEY_BYTES);
  try {
    await fs.writeFile(keyPath, key, { mode: 0o600, flag: "wx" });
    return key;
  } catch {
    // lost a race with another writer: theirs is the key everything else uses
    const existing = await fs.readFile(keyPath);
    if (existing.length !== KEY_BYTES) throw new Error("secure store key is corrupt");
    return existing;
  }
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

/** Writes `value` encrypted at rest, creating the directory if needed. */
export async function writeSecret(filePath: string, value: string): Promise<void> {
  const dir = path.dirname(filePath);
  await fs.mkdir(dir, { recursive: true, mode: 0o700 });

  if (keychainUsable()) {
    const storage = getSafeStorage();
    if (storage) {
      await fs.writeFile(filePath, storage.encryptString(value), { mode: 0o600 });
      return;
    }
  }

  const key = await loadOrCreateKey(dir);
  await fs.writeFile(filePath, encryptLocal(key, value), { mode: 0o600 });
}

/** Reads a secret written by `writeSecret`, or null when it is missing or unreadable. */
export async function readSecret(filePath: string): Promise<string | null> {
  let data: Buffer;
  try {
    data = await fs.readFile(filePath);
  } catch {
    return null;
  }

  if (data.subarray(0, MAGIC.length).toString("latin1") === MAGIC) {
    try {
      return decryptLocal(await loadOrCreateKey(path.dirname(filePath)), data);
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

  return data.toString("utf8");
}
