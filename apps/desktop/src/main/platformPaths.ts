import fsSync from "node:fs";
import fs from "node:fs/promises";
import path from "node:path";
import { app } from "electron";
import { daemonTokenCandidatePaths } from "./daemonTokenPaths";
import { ensureRuntimeFiles } from "./runtimeFiles";

const APP_FOLDER = "pangeavpn-desktop";
const WINDOWS_SERVICE_FOLDER = "PangeaVPN";
const MAC_SYSTEM_FOLDER = "PangeaVPN";
const MAC_LAUNCH_DAEMON_PLIST = "/Library/LaunchDaemons/com.pangea.pangeavpn.daemon.plist";
const LINUX_SYSTEM_SUPPORT_DIR = "/etc/pangeavpn";

// install-linux.sh installs pangea-daemon as a root-owned systemd service whose
// unit pins PANGEA_APP_SUPPORT_DIR=/etc/pangeavpn. Its presence means that
// service — not this process — owns the daemon's state directory.
const linuxDaemonServiceUnitPaths = [
  "/etc/systemd/system/pangea-daemon.service",
  "/lib/systemd/system/pangea-daemon.service",
  "/usr/lib/systemd/system/pangea-daemon.service"
];

export function hasManagedLinuxDaemonService(): boolean {
  if (process.platform !== "linux") {
    return false;
  }
  return linuxDaemonServiceUnitPaths.some((servicePath) => fsSync.existsSync(servicePath));
}

export function getAppSupportDir(): string {
  if (process.platform === "win32") {
    return getWindowsServiceSupportDir();
  }
  if (process.platform === "darwin" && shouldUseMacSystemSupportDir()) {
    return path.join("/Library/Application Support", MAC_SYSTEM_FOLDER);
  }
  if (hasManagedLinuxDaemonService()) {
    return LINUX_SYSTEM_SUPPORT_DIR;
  }
  return path.join(app.getPath("appData"), APP_FOLDER);
}

// Desktop-owned state (settings, caches). Never getAppSupportDir(): that one is
// the daemon's, and it is admin-only on Windows and root-owned on macOS.
export function getUserStateDir(): string {
  return path.join(app.getPath("appData"), APP_FOLDER);
}

export async function ensureUserStateDir(): Promise<string> {
  const dir = getUserStateDir();
  await fs.mkdir(dir, { recursive: true, mode: 0o700 });
  // mkdir's mode is a no-op on a dir that already existed with looser perms.
  await fs.chmod(dir, 0o700).catch(() => {});
  return dir;
}

// Path a desktop state file used to live at, back when it was written into the
// daemon's directory. Read-only fallback for a one-time migration.
export function getLegacyStateFilePath(fileName: string): string | null {
  const legacyDir = getAppSupportDir();
  if (legacyDir === getUserStateDir()) {
    return null;
  }
  return path.join(legacyDir, fileName);
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
  await ensureRuntimeFiles(getAppSupportDir(), { daemonOwnsDir: daemonOwnsStateDir() });
}

// True where an elevated daemon owns the state directory. It is admin-only, so
// the desktop reads the token there and writes nothing.
function daemonOwnsStateDir(): boolean {
  return process.platform === "win32" || shouldUseMacSystemSupportDir() || hasManagedLinuxDaemonService();
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
  return daemonTokenCandidatePaths(process.platform, getTokenPath(), app.getPath("appData"));
}
