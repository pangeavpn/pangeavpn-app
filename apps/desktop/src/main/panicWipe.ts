import path from "node:path";

// Hardcoded, not user-configurable. Three modifiers plus a function key: global,
// OS-registrable on all platforms, and effectively impossible to hit by accident.
export const PANIC_WIPE_ACCELERATOR = "CommandOrControl+Alt+Shift+F12";

// The second press must land within this window of the first to fire.
export const PANIC_DOUBLE_PRESS_WINDOW_MS = 2000;

export type PanicPhase = "start" | "done" | "error";

export interface PanicWipeContext {
  platform: NodeJS.Platform;
  isPackaged: boolean;
  appPid: number;
  homeDir: string;
  appDataDir: string;
  userDataDir: string;
  logsDir: string;
  userStateDir: string;
}

export interface PanicWipeActions {
  /** Disconnect and lower the kill switch so the network returns to normal first. */
  lowerLock: () => Promise<void>;
  /** Ask the already-privileged daemon to uninstall itself — no OS prompt. */
  requestDaemonUninstall: (appPid: number) => Promise<void>;
  stopDaemonProcess: () => void;
  removeLoginItem: () => Promise<void>;
  removePath: (target: string) => Promise<void>;
  spawnDetached: (file: string, args: string[]) => void;
  notify: (phase: PanicPhase, detail?: string) => void;
  quit: () => void;
  logf: (message: string) => void;
}

// User-owned app data we can delete without elevation: our own state plus
// Electron/Chromium's per-user caches, logs, and preferences.
export function userStatePathsToRemove(ctx: PanicWipeContext): string[] {
  // The host running the wipe is the target platform, but keep the join
  // separator explicit so the paths are correct (and tests deterministic) anywhere.
  const join = ctx.platform === "win32" ? path.win32.join : path.posix.join;
  const paths = [ctx.userStateDir, ctx.userDataDir, ctx.logsDir];
  if (ctx.platform === "win32") {
    paths.push(join(ctx.appDataDir, "PangeaVPN"));
  } else if (ctx.platform === "darwin") {
    const lib = join(ctx.homeDir, "Library");
    paths.push(
      join(lib, "Logs", "PangeaVPN"),
      join(lib, "Caches", "PangeaVPN"),
      join(lib, "Caches", "com.pangea.pangeavpn"),
      join(lib, "HTTPStorages", "com.pangea.pangeavpn"),
      join(lib, "Containers", "com.pangea.pangeavpn"),
      join(lib, "Saved Application State", "com.pangea.pangeavpn.savedState"),
      join(lib, "Preferences", "com.pangea.pangeavpn.plist")
    );
  } else {
    paths.push(join(ctx.homeDir, ".cache", "PangeaVPN"));
  }
  return [...new Set(paths)].filter((p) => p && p.length > 0);
}

// Unprivileged detached cleanup of user-owned paths that stay file-locked until
// the GUI exits (chiefly Chromium's state on Windows). No elevation needed.
export function deferredUserCleanupCommand(ctx: PanicWipeContext): { file: string; args: string[] } | null {
  const targets = userStatePathsToRemove(ctx);
  if (targets.length === 0) {
    return null;
  }
  const pid = Number.isFinite(ctx.appPid) ? Math.trunc(ctx.appPid) : 0;

  if (ctx.platform === "win32") {
    const removals = targets.map((p) => `Remove-Item -LiteralPath ${psQuote(p)} -Recurse -Force -ErrorAction SilentlyContinue`);
    const script = [
      "$ErrorActionPreference='SilentlyContinue'",
      pid > 0 ? `try { Wait-Process -Id ${pid} -Timeout 30 } catch {}` : "",
      ...removals
    ]
      .filter((line) => line.length > 0)
      .join("\n");
    return {
      file: "powershell.exe",
      args: ["-NoProfile", "-ExecutionPolicy", "Bypass", "-WindowStyle", "Hidden", "-EncodedCommand", psEncode(script)]
    };
  }

  const removals = targets.map((p) => `rm -rf ${shQuote(p)} >/dev/null 2>&1 || true`);
  const wait = pid > 0 ? `n=0; while kill -0 ${pid} 2>/dev/null && [ $n -lt 60 ]; do sleep 0.5; n=$((n+1)); done` : "";
  const script = [wait, ...removals].filter((line) => line.length > 0).join("; ");
  return { file: "/bin/sh", args: ["-c", script] };
}

export async function performPanicWipe(ctx: PanicWipeContext, actions: PanicWipeActions): Promise<void> {
  actions.notify("start");
  actions.logf("panic wipe: lowering kill switch and disconnecting");
  try {
    await actions.lowerLock();
  } catch (error) {
    actions.logf(`panic wipe: lowerLock failed: ${describe(error)}`);
  }

  // Before the user dirs go: without a system service, the daemon token lives in them.
  let uninstalled = true;
  if (ctx.isPackaged) {
    try {
      await actions.requestDaemonUninstall(ctx.appPid);
    } catch (error) {
      uninstalled = false;
      actions.logf(`panic wipe: daemon uninstall request failed: ${describe(error)}`);
    }
  }

  try {
    await actions.removeLoginItem();
  } catch (error) {
    actions.logf(`panic wipe: could not remove the login item: ${describe(error)}`);
  }

  for (const target of userStatePathsToRemove(ctx)) {
    try {
      await actions.removePath(target);
    } catch (error) {
      actions.logf(`panic wipe: could not remove ${target}: ${describe(error)}`);
    }
  }

  if (ctx.isPackaged) {
    const cleanup = deferredUserCleanupCommand(ctx);
    if (cleanup) {
      try {
        actions.spawnDetached(cleanup.file, cleanup.args);
      } catch (error) {
        actions.logf(`panic wipe: deferred cleanup failed to launch: ${describe(error)}`);
      }
    }
  }

  try {
    actions.stopDaemonProcess();
  } catch (error) {
    actions.logf(`panic wipe: stopDaemonProcess failed: ${describe(error)}`);
  }

  actions.notify(uninstalled ? "done" : "error");
  actions.quit();
}

function describe(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function psEncode(value: string): string {
  return Buffer.from(value, "utf16le").toString("base64");
}

function psQuote(value: string): string {
  return `'${value.replace(/'/g, "''")}'`;
}

function shQuote(value: string): string {
  return `'${value.replace(/'/g, `'"'"'`)}'`;
}
