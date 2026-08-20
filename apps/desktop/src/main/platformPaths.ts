import { randomBytes } from "node:crypto";
import fsSync from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import { app } from "electron";

const APP_FOLDER = "pangeavpn-desktop";
const WINDOWS_SERVICE_FOLDER = "PangeaVPN";
const MAC_SYSTEM_FOLDER = "PangeaVPN";
const MAC_LAUNCH_DAEMON_PLIST = "/Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist";
const TOKEN_SHAPE_PATTERN = /^[0-9a-f]{64}$/;

export function getAppSupportDir(): string {
  if (process.platform === "win32") {
    return getWindowsServiceSupportDir();
  }
  if (process.platform === "darwin" && shouldUseMacSystemSupportDir()) {
    return path.join("/Library/Application Support", MAC_SYSTEM_FOLDER);
  }
  return path.join(app.getPath("appData"), APP_FOLDER);
}

export function getTokenPath(): string {
  return path.join(getAppSupportDir(), "daemon-token.txt");
}

export async function readDaemonTokens(): Promise<string[]> {
  const tokens: string[] = [];
  const seen = new Set<string>();

  for (const tokenPath of daemonTokenCandidates()) {
    try {
      const content = await fs.readFile(tokenPath, "utf8");
      const token = content.trim();
      if (!token || seen.has(token)) {
        continue;
      }
      seen.add(token);
      tokens.push(token);
    } catch {
      // ignore missing/unreadable token candidate
    }
  }

  return tokens;
}

export async function ensureUserRuntimeFiles(): Promise<void> {
  const appDir = getAppSupportDir();
  const tokenPath = path.join(appDir, "daemon-token.txt");
  const configPath = path.join(appDir, "config.json");

  await fs.mkdir(appDir, { recursive: true });

  // The root daemon owns and creates the token in its system directory; the
  // desktop only reads it there instead of racing it with its own token.
  if (shouldUseMacSystemSupportDir()) {
    await ensureTokenFileReadable(tokenPath);
  } else {
    await ensureTokenFile(tokenPath);
  }

  await ensureConfigFile(configPath);
}

function getWindowsServiceSupportDir(): string {
  // process.env.ProgramData is user-settable via HKCU\Environment with no
  // admin rights; SystemDrive plus the fixed folder name is not.
  const systemDrive = process.env.SystemDrive?.trim() || "C:";
  return path.join(systemDrive, "ProgramData", WINDOWS_SERVICE_FOLDER);
}

function shouldUseMacSystemSupportDir(): boolean {
  if (process.platform !== "darwin") {
    return false;
  }
  if (!app.isPackaged) {
    return false;
  }
  if (!fsSync.existsSync(MAC_LAUNCH_DAEMON_PLIST)) {
    return false;
  }

  const normalizedExecPath = path.normalize(process.execPath);
  const applicationsPrefix = path.normalize("/Applications") + path.sep;
  return normalizedExecPath.startsWith(applicationsPrefix);
}

function daemonTokenCandidates(): string[] {
  const candidates: string[] = [];
  const seen = new Set<string>();
  const add = (candidate: string) => {
    const normalized = path.normalize(candidate);
    if (seen.has(normalized)) {
      return;
    }
    seen.add(normalized);
    candidates.push(normalized);
  };

  add(getTokenPath());

  if (process.platform === "darwin") {
    add(path.join("/Library/Application Support", MAC_SYSTEM_FOLDER, "daemon-token.txt"));
    add(path.join(app.getPath("appData"), APP_FOLDER, "daemon-token.txt"));
  }

  if (process.platform === "linux") {
    add(path.join("/etc/pangeavpn", "daemon-token.txt"));
  }

  return candidates;
}

// The system-directory token is owned and rotated by the root daemon; this
// only confirms it exists and is well-formed, never (re)creating it, so the
// desktop never hands the daemon a token it will refuse to trust.
async function ensureTokenFileReadable(tokenPath: string): Promise<void> {
  let token = "";
  try {
    token = (await fs.readFile(tokenPath, "utf8")).trim();
  } catch (error) {
    if (isNotFound(error)) {
      throw new Error(
        "PangeaVPN's background service has not finished starting up yet. Please wait a moment and try again."
      );
    }
    throw error;
  }

  if (!TOKEN_SHAPE_PATTERN.test(token)) {
    throw new Error(
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
