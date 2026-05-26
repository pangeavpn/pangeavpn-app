import { app } from "electron";
import fs from "node:fs/promises";
import path from "node:path";
import os from "node:os";

const HIDDEN_ARG = "--hidden";
const LINUX_AUTOSTART_FILENAME = "pangeavpn.desktop";

function linuxAutostartDir(): string {
  const xdg = process.env.XDG_CONFIG_HOME;
  const base = xdg && xdg.trim().length > 0 ? xdg : path.join(os.homedir(), ".config");
  return path.join(base, "autostart");
}

function linuxAutostartPath(): string {
  return path.join(linuxAutostartDir(), LINUX_AUTOSTART_FILENAME);
}

function buildDesktopFile(execPath: string): string {
  const quoted = execPath.includes(" ") ? `"${execPath}"` : execPath;
  return [
    "[Desktop Entry]",
    "Type=Application",
    "Name=PangeaVPN",
    `Exec=${quoted} ${HIDDEN_ARG}`,
    "X-GNOME-Autostart-enabled=true",
    "Hidden=false",
    "Terminal=false",
    "Comment=PangeaVPN tray client",
    ""
  ].join("\n");
}

export async function setLoginItemEnabled(enabled: boolean): Promise<void> {
  // Skip in dev: would point .desktop / login item at the dev electron binary.
  if (!app.isPackaged) {
    if (enabled) {
      console.warn("loginItem: skipping setLoginItemEnabled in dev (app not packaged)");
    }
    return;
  }

  if (process.platform === "darwin" || process.platform === "win32") {
    app.setLoginItemSettings({
      openAtLogin: enabled,
      openAsHidden: enabled,
      args: enabled ? [HIDDEN_ARG] : []
    });
    return;
  }

  if (process.platform === "linux") {
    const filePath = linuxAutostartPath();
    if (enabled) {
      await fs.mkdir(linuxAutostartDir(), { recursive: true });
      await fs.writeFile(filePath, buildDesktopFile(process.execPath), { encoding: "utf8", mode: 0o600 });
    } else {
      await fs.rm(filePath, { force: true });
    }
  }
}

export async function isLoginItemEnabled(): Promise<boolean> {
  if (!app.isPackaged) return false;

  if (process.platform === "darwin" || process.platform === "win32") {
    try {
      return app.getLoginItemSettings().openAtLogin === true;
    } catch {
      return false;
    }
  }

  if (process.platform === "linux") {
    try {
      await fs.access(linuxAutostartPath());
      return true;
    } catch {
      return false;
    }
  }

  return false;
}

export function isHiddenLaunchArg(arg: string): boolean {
  return arg === HIDDEN_ARG || arg === "--start-minimized";
}
