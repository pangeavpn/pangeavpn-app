import { app, safeStorage } from "electron";
import fs from "node:fs/promises";
import path from "node:path";
import type { AuthState, AuthUser } from "../shared/ipc";
import { getAppSupportDir } from "./platformPaths";

/**
 * User-writable directory for auth files (session, license key, identity keys).
 * Always uses the per-user path, never the root-owned system path,
 * because these files are written by the Electron app (running as the user),
 * not by the daemon (running as root).
 */
function getUserAuthDir(): string {
  const userDir = path.join(app.getPath("appData"), "pangeavpn-desktop");
  return userDir;
}

async function ensureUserAuthDir(): Promise<void> {
  await fs.mkdir(getUserAuthDir(), { recursive: true });
}

// --- Auth session persistence (stores user info from token login) ---

interface StoredSession {
  user: AuthUser;
  vpnAccessToken: string;
}

let cachedSession: StoredSession | null = null;

function getSessionFilePath(): string {
  return path.join(getUserAuthDir(), "auth-session.dat");
}

async function saveSession(session: StoredSession): Promise<void> {
  await ensureUserAuthDir();
  const json = JSON.stringify(session);
  const filePath = getSessionFilePath();

  if (safeStorage.isEncryptionAvailable()) {
    const encrypted = safeStorage.encryptString(json);
    await fs.writeFile(filePath, encrypted);
  } else {
    console.warn("OS keychain unavailable; session stored unencrypted (file permissions 0600)");
    await fs.writeFile(filePath, json, { mode: 0o600 });
  }

  cachedSession = session;
}

async function loadSession(): Promise<StoredSession | null> {
  if (cachedSession) return cachedSession;

  const filePath = getSessionFilePath();
  try {
    const data = await fs.readFile(filePath);
    let json: string;

    if (safeStorage.isEncryptionAvailable()) {
      try {
        json = safeStorage.decryptString(data);
      } catch {
        json = data.toString("utf8");
      }
    } else {
      json = data.toString("utf8");
    }

    const session = JSON.parse(json) as StoredSession;
    cachedSession = session;
    return session;
  } catch {
    return null;
  }
}

async function clearSession(): Promise<void> {
  cachedSession = null;
  try {
    await fs.rm(getSessionFilePath(), { force: true });
  } catch {
    // best-effort
  }
}

// --- Public auth API ---

export async function loginWithToken(
  vpnAccessToken: string,
  user: AuthUser,
): Promise<AuthState> {
  await saveSession({ user, vpnAccessToken });
  return { authenticated: true, user };
}

export async function getAuthState(): Promise<AuthState> {
  const session = await loadSession();
  if (!session) return { authenticated: false, user: null };
  return { authenticated: true, user: session.user };
}

export async function logout(): Promise<void> {
  await clearSession();
  await clearLicenseKey();
  await clearIdentityKeyPair();
  // Clean up legacy auth-tokens.dat from old Auth0 flow
  try {
    await fs.rm(path.join(getAppSupportDir(), "auth-tokens.dat"), { force: true });
  } catch {
    // best-effort
  }
}

// --- License key persistence (encrypted at rest via safeStorage) ---

function getLicenseKeyPath(): string {
  return path.join(getUserAuthDir(), "license-key.dat");
}

export async function saveLicenseKey(key: string): Promise<void> {
  await ensureUserAuthDir();
  const filePath = getLicenseKeyPath();
  if (safeStorage.isEncryptionAvailable()) {
    const encrypted = safeStorage.encryptString(key);
    await fs.writeFile(filePath, encrypted);
  } else {
    await fs.writeFile(filePath, key, { mode: 0o600 });
  }
}

export async function loadLicenseKey(): Promise<string | null> {
  const filePath = getLicenseKeyPath();
  try {
    const data = await fs.readFile(filePath);
    let key: string;

    if (safeStorage.isEncryptionAvailable()) {
      try {
        key = safeStorage.decryptString(data);
      } catch {
        key = data.toString("utf8");
      }
    } else {
      key = data.toString("utf8");
    }

    return key.trim() || null;
  } catch {
    return null;
  }
}

export async function clearLicenseKey(): Promise<void> {
  try {
    await fs.rm(getLicenseKeyPath(), { force: true });
  } catch {
    // best-effort
  }
}

// --- Identity key pair persistence (device identity, X25519) ---

interface IdentityKeyPair {
  privateKey: string;
  publicKey: string;
}

function getIdentityKeyPath(): string {
  return path.join(getUserAuthDir(), "identity-key.dat");
}

/** Legacy path used before the rename to identity-key.dat */
function getLegacyWgKeyPath(): string {
  return path.join(getUserAuthDir(), "wg-device-key.dat");
}

export async function saveIdentityKeyPair(keys: IdentityKeyPair): Promise<void> {
  await ensureUserAuthDir();
  const json = JSON.stringify(keys);
  const filePath = getIdentityKeyPath();
  if (safeStorage.isEncryptionAvailable()) {
    const encrypted = safeStorage.encryptString(json);
    await fs.writeFile(filePath, encrypted);
  } else {
    await fs.writeFile(filePath, json, { mode: 0o600 });
  }
}

export async function loadIdentityKeyPair(): Promise<IdentityKeyPair | null> {
  const filePath = getIdentityKeyPath();
  try {
    const data = await fs.readFile(filePath);
    let json: string;
    if (safeStorage.isEncryptionAvailable()) {
      try {
        json = safeStorage.decryptString(data);
      } catch {
        json = data.toString("utf8");
      }
    } else {
      json = data.toString("utf8");
    }
    const keys = JSON.parse(json) as IdentityKeyPair;
    if (keys.privateKey && keys.publicKey) return keys;
    return null;
  } catch {
    // identity-key.dat not found — try migrating from legacy wg-device-key.dat
  }

  // Migration: load from old path, save to new path, delete old file
  const legacyPath = getLegacyWgKeyPath();
  try {
    const data = await fs.readFile(legacyPath);
    let json: string;
    if (safeStorage.isEncryptionAvailable()) {
      try {
        json = safeStorage.decryptString(data);
      } catch {
        json = data.toString("utf8");
      }
    } else {
      json = data.toString("utf8");
    }
    const keys = JSON.parse(json) as IdentityKeyPair;
    if (keys.privateKey && keys.publicKey) {
      await saveIdentityKeyPair(keys);
      await fs.rm(legacyPath, { force: true }).catch(() => {});
      return keys;
    }
    return null;
  } catch {
    return null;
  }
}

export async function clearIdentityKeyPair(): Promise<void> {
  try {
    await fs.rm(getIdentityKeyPath(), { force: true });
  } catch {
    // best-effort
  }
  // Also clean up legacy file if it still exists
  try {
    await fs.rm(getLegacyWgKeyPath(), { force: true });
  } catch {
    // best-effort
  }
}
