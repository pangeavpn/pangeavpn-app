import { createCipheriv, createDecipheriv, hkdfSync, randomBytes } from "node:crypto";

const SALT = "pangea-names";
const NAME_KEY_PREFIX = "wk1:";
const SEALED_NAME_PREFIX = "zk2:";
const NAME_KEY_AAD = "nameKey";
const ACCOUNT_NUMBER = /^[0-9A-HJKMNP-TV-Z]{24}$/;
const BASE64 = /^[A-Za-z0-9+/]+={0,2}$/;
const UNSAFE_CHARS = /[\p{Cc}\u061C\u200E\u200F\u202A-\u202E\u2066-\u2069]/u;

export const NAME_MAX = 40;
export const RENAME_NEEDS_ACCOUNT_NUMBER = "Sign in with your account number to rename devices";
export const NAME_KEY_UNREADABLE = "This account's device names key does not open with your account number";

export interface DeviceRow {
  id: string;
  identityPubkey?: string | null;
  friendlyName: string | null;
  createdAt: string;
  status: string;
}

export interface HubDeviceRow extends DeviceRow {
  label?: string | null;
  sealedName?: string | null;
}

/** `nameKey` is present, null at least, only on a hub that speaks names v2. */
export interface HubDeviceList {
  devices: HubDeviceRow[];
  nameKey?: string | null;
}

export interface SealedRename {
  deviceId: string;
  sealedName: string;
  nameKey?: string;
}

export type HubCall = (method: string, route: string, body?: unknown) => Promise<unknown>;

export function normalizeAccountNumber(raw: string): string {
  return raw.toUpperCase().replace(/[\s-]/g, "").replace(/O/g, "0").replace(/[IL]/g, "1");
}

// Null for a legacy token: the hub stores those readable.
export function numberWrapKey(credential: string | null): Buffer | null {
  const normalized = normalizeAccountNumber(credential ?? "");
  if (!ACCOUNT_NUMBER.test(normalized)) return null;
  return Buffer.from(hkdfSync("sha256", normalized, SALT, "wrap:number:v1", 32));
}

export const newNameKey = (): Buffer => randomBytes(32);

export function sealValue(prefix: string, key: Buffer, plain: Buffer, aad: string, nonce: Buffer = randomBytes(12)): string {
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  cipher.setAAD(Buffer.from(aad, "utf8"));
  const sealed = Buffer.concat([nonce, cipher.update(plain), cipher.final(), cipher.getAuthTag()]);
  return prefix + sealed.toString("base64");
}

/** Null for anything it cannot read, an empty plaintext included. */
export function openValue(prefix: string, key: Buffer | null, stored: unknown, aad: string): Buffer | null {
  if (key?.length !== 32 || typeof stored !== "string" || !stored.startsWith(prefix)) return null;
  const encoded = stored.slice(prefix.length);
  if (!BASE64.test(encoded)) return null;
  const raw = Buffer.from(encoded, "base64");
  if (raw.length < 29) return null;
  try {
    const decipher = createDecipheriv("aes-256-gcm", key, raw.subarray(0, 12));
    decipher.setAAD(Buffer.from(aad, "utf8"));
    decipher.setAuthTag(raw.subarray(raw.length - 16));
    return Buffer.concat([decipher.update(raw.subarray(12, raw.length - 16)), decipher.final()]);
  } catch {
    return null;
  }
}

export const wrapNameKey = (wrapKey: Buffer, k: Buffer, nonce?: Buffer): string =>
  sealValue(NAME_KEY_PREFIX, wrapKey, k, NAME_KEY_AAD, nonce);

export function unwrapNameKey(wrapKey: Buffer | null, wrapped: unknown): Buffer | null {
  const k = openValue(NAME_KEY_PREFIX, wrapKey, wrapped, NAME_KEY_AAD);
  return k?.length === 32 ? k : null;
}

export const sealName = (k: Buffer, deviceId: string, name: string, nonce?: Buffer): string =>
  sealValue(SEALED_NAME_PREFIX, k, Buffer.from(name, "utf8"), deviceId.toLowerCase(), nonce);

export function openName(k: Buffer | null, deviceId: string, sealed: unknown): string | null {
  return openValue(SEALED_NAME_PREFIX, k, sealed, deviceId.toLowerCase())?.toString("utf8") ?? null;
}

export function checkDeviceName(raw: unknown): string {
  const trimmed = typeof raw === "string" ? raw.trim() : "";
  if (trimmed.length < 1 || trimmed.length > NAME_MAX) throw new Error(`Device name must be 1-${NAME_MAX} characters`);
  if (UNSAFE_CHARS.test(trimmed)) throw new Error("Device name must not contain control characters");
  return trimmed;
}

const speaksNamesV2 = (list: HubDeviceList | undefined): boolean => list?.nameKey !== undefined;

export function displayName(row: HubDeviceRow, k: Buffer | null): string {
  return openName(k, row.id, row.sealedName) || row.label || row.friendlyName || row.id.slice(0, 8);
}

/** Puts each row's display name in friendlyName; an old hub's rows pass through. */
export function shownDevices(list: HubDeviceList, credential: string | null): DeviceRow[] {
  const v2 = speaksNamesV2(list);
  const k = v2 ? unwrapNameKey(numberWrapKey(credential), list.nameKey) : null;
  return list.devices.map((device) => {
    const { label: _label, sealedName: _sealedName, ...row } = device;
    return v2 ? { ...row, friendlyName: displayName(device, k) } : row;
  });
}

/** Seals name under the account's key, creating that key when the hub has none. */
export function sealRename(list: HubDeviceList, credential: string | null, deviceId: string, name: string): SealedRename {
  const wrapKey = numberWrapKey(credential);
  if (!wrapKey) throw new Error(RENAME_NEEDS_ACCOUNT_NUMBER);
  if (list.nameKey) {
    const k = unwrapNameKey(wrapKey, list.nameKey);
    if (!k) throw new Error(NAME_KEY_UNREADABLE);
    return { deviceId, sealedName: sealName(k, deviceId, name) };
  }
  const k = newNameKey();
  return { deviceId, sealedName: sealName(k, deviceId, name), nameKey: wrapNameKey(wrapKey, k) };
}

/** Retries once when another device created the account's key between the list and the rename. */
export async function renameDevice(call: HubCall, credential: string | null, deviceId: string, name: unknown): Promise<void> {
  const trimmed = checkDeviceName(name);
  for (let retried = false; ; retried = true) {
    const list = (await call("GET", "/api/device/list")) as HubDeviceList | undefined;
    if (!list || !speaksNamesV2(list)) {
      await call("POST", "/api/device/rename", { deviceId, friendlyName: trimmed });
      return;
    }
    const body = sealRename(list, credential, deviceId, trimmed);
    try {
      await call("POST", "/api/device/rename", body);
      return;
    } catch (err) {
      const keyExists = err instanceof Error && err.message.includes("NAME_KEY_EXISTS");
      if (retried || !body.nameKey || !keyExists) throw err;
    }
  }
}
