import { createCipheriv, createDecipheriv, hkdfSync, randomBytes } from "node:crypto";

const PREFIX = "zk1:";
const ACCOUNT_NUMBER = /^[0-9A-HJKMNP-TV-Z]{24}$/;
const UNSAFE_CHARS = /[\p{Cc}\u061C\u200E\u200F\u202A-\u202E\u2066-\u2069]/u;

export const RENAME_NEEDS_ACCOUNT_NUMBER = "Sign in with your account number to rename devices";

// Null for a legacy token: the hub stores those in plaintext, so they cannot keep a name secret.
export function deviceNameKey(credential: string | null): Buffer | null {
  const normalized = (credential ?? "")
    .toUpperCase()
    .replace(/[\s-]/g, "")
    .replace(/O/g, "0")
    .replace(/[IL]/g, "1");
  if (!ACCOUNT_NUMBER.test(normalized)) return null;
  return Buffer.from(hkdfSync("sha256", normalized, "pangea-device-name", "zk1", 32));
}

export function sealDeviceName(name: string, key: Buffer, nonce: Buffer = randomBytes(12)): string {
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  const sealed = Buffer.concat([nonce, cipher.update(name, "utf8"), cipher.final(), cipher.getAuthTag()]);
  return PREFIX + sealed.toString("base64");
}

export function openDeviceName(stored: unknown, key: Buffer | null): string | null {
  if (!key || typeof stored !== "string" || !stored.startsWith(PREFIX)) return null;
  const raw = Buffer.from(stored.slice(PREFIX.length), "base64");
  if (raw.length < 29) return null;
  try {
    const decipher = createDecipheriv("aes-256-gcm", key, raw.subarray(0, 12));
    decipher.setAuthTag(raw.subarray(raw.length - 16));
    return Buffer.concat([decipher.update(raw.subarray(12, raw.length - 16)), decipher.final()]).toString("utf8");
  } catch {
    return null;
  }
}

export function checkDeviceName(raw: unknown): string {
  const trimmed = typeof raw === "string" ? raw.trim() : "";
  if (trimmed.length < 1 || trimmed.length > 64) throw new Error("Device name must be 1-64 characters");
  if (UNSAFE_CHARS.test(trimmed)) throw new Error("Device name must not contain control characters");
  return trimmed;
}
