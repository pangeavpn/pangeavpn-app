import { randomBytes } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";

export const TOKEN_SHAPE_PATTERN = /^[0-9a-f]{64}$/;

export interface RuntimeFileOptions {
  /** True when an elevated daemon owns the directory. Then nothing here may
   *  write to it: the dir is admin-only and the daemon owns both files. */
  daemonOwnsDir: boolean;
}

export class DaemonNotReadyError extends Error {}

export async function ensureRuntimeFiles(appDir: string, options: RuntimeFileOptions): Promise<void> {
  const tokenPath = path.join(appDir, "daemon-token.txt");

  if (options.daemonOwnsDir) {
    await verifyTokenFile(tokenPath);
    return;
  }

  await fs.mkdir(appDir, { recursive: true });
  await ensureTokenFile(tokenPath);
  await ensureConfigFile(path.join(appDir, "config.json"));
}

// Confirms the daemon's token exists and is well-formed, never (re)creating it,
// so the desktop never hands the daemon a token it will refuse to trust.
async function verifyTokenFile(tokenPath: string): Promise<void> {
  let token = "";
  try {
    token = (await fs.readFile(tokenPath, "utf8")).trim();
  } catch (error) {
    if (isNotFound(error)) {
      throw new DaemonNotReadyError(
        "PangeaVPN's background service has not finished starting up yet. Please wait a moment and try again."
      );
    }
    throw error;
  }

  if (!TOKEN_SHAPE_PATTERN.test(token)) {
    throw new DaemonNotReadyError(
      "PangeaVPN's background service access token is not ready yet. Please wait a moment and try again."
    );
  }
}

async function ensureTokenFile(tokenPath: string): Promise<void> {
  let token = "";

  try {
    token = (await fs.readFile(tokenPath, "utf8")).trim();
  } catch (error) {
    if (!isNotFound(error)) {
      throw error;
    }
  }

  if (token && !TOKEN_SHAPE_PATTERN.test(token)) {
    await tryRemoveFile(tokenPath);
    token = "";
  }

  if (!token) {
    token = randomBytes(32).toString("hex");
    await fs.writeFile(tokenPath, `${token}\n`, { mode: 0o600 });
  }

  await fs.chmod(tokenPath, 0o600).catch(() => {});
}

async function ensureConfigFile(configPath: string): Promise<void> {
  let content = "";

  try {
    content = await fs.readFile(configPath, "utf8");
  } catch (error) {
    if (!isNotFound(error)) {
      throw error;
    }
  }

  if (content.trim()) {
    try {
      JSON.parse(content);
      return;
    } catch {
      await tryRemoveFile(configPath);
    }
  }

  const defaultConfig = `${JSON.stringify({ profiles: [] }, null, 2)}\n`;
  await fs.writeFile(configPath, defaultConfig, { mode: 0o600 });
  await fs.chmod(configPath, 0o600).catch(() => {});
}

function isNotFound(error: unknown): boolean {
  return typeof error === "object" &&
    error !== null &&
    "code" in error &&
    (error as { code?: string }).code === "ENOENT";
}

async function tryRemoveFile(filePath: string): Promise<void> {
  try {
    await fs.rm(filePath, { force: true });
  } catch {
    // best-effort cleanup before recreating runtime files.
  }
}
