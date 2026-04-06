import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { app } from "electron";
import { DaemonClient } from "./daemonClient";
import { getBundledDaemonPath } from "./resourcePaths";
import { ensureUserRuntimeFiles, getAppSupportDir } from "./platformPaths";

type EnsureDaemonOptions = {
  forceRestart?: boolean;
};

const macLaunchDaemonLabel = "com.pangea.pangeavpn.daemon";
const macLaunchDaemonPlist = `/Library/LaunchDaemons/${macLaunchDaemonLabel}.plist`;
const macElevationRetryBackoffMs = 10000;

export class DaemonProcessManager {
  private child: ChildProcess | null = null;
  private readonly client: DaemonClient;
  private ensureInFlight: Promise<void> | null = null;
  private lastMacElevationFailureAtMs = 0;

  constructor(client: DaemonClient) {
    this.client = client;
  }

  async ensureRunning(options: EnsureDaemonOptions = {}): Promise<void> {
    if (this.ensureInFlight) {
      return this.ensureInFlight;
    }

    const task = this.ensureRunningInternal(options).finally(() => {
      this.ensureInFlight = null;
    });
    this.ensureInFlight = task;
    return task;
  }

  private async ensureRunningInternal(options: EnsureDaemonOptions): Promise<void> {
    const forceRestart = options.forceRestart === true;

    if (process.platform === "win32" && app.isPackaged) {
      const serviceStart = ensureWindowsDaemonServiceRunning();
      if (!serviceStart.ok) {
        throw new Error(serviceStart.message);
      }

      await this.waitForReachable();
      return;
    }

    if (process.platform === "darwin" && app.isPackaged) {
      await this.ensureMacPackagedRunning(forceRestart);
      return;
    }

    const online = await this.safeApiReady();
    if (!forceRestart && online) {
      return;
    }

    if (this.child && !forceRestart) {
      return;
    }
    if (this.child && forceRestart) {
      this.child.kill();
      this.child = null;
    }

    if (!app.isPackaged && process.platform !== "win32") {
      return;
    }

    if (process.platform === "win32") {
      const daemonPath = this.resolveDaemonPath();
      if (!daemonPath) {
        throw new Error("daemon binary not found for this runtime");
      }

      const elevate = startProcessElevatedWindows(daemonPath, []);
      if (!elevate.ok) {
        throw new Error(elevate.message);
      }
    } else {
      const daemonPath = this.resolveDaemonPath();
      if (!daemonPath) {
        throw new Error("daemon binary not found for this runtime");
      }

      if (!app.isPackaged) {
        return;
      }

      this.child = spawn(daemonPath, [], {
        windowsHide: true,
        stdio: "ignore"
      });

      this.child.on("exit", () => {
        this.child = null;
      });
    }

    await this.waitForReachable();
  }

  private async ensureMacPackagedRunning(forceRestart: boolean): Promise<void> {
    const daemonPath = this.resolveDaemonPath();
    if (!daemonPath) {
      throw new Error("daemon binary not found for this runtime");
    }

    stripMacQuarantine(daemonPath);

    if (shouldUseManagedMacLaunchDaemon(daemonPath) && hasManagedMacLaunchDaemon()) {
      const online = await this.safeApiReady();
      if (!forceRestart && online) {
        return;
      }

      const kick = kickstartManagedMacLaunchDaemon();
      if (!kick.ok) {
        throw new Error(kick.message);
      }
      await this.waitForReachable();
      return;
    }

    await ensureUserRuntimeFiles().catch(() => {});
    const online = await this.safeApiReady();
    if (!forceRestart && online) {
      return;
    }

    const allowUnelevatedFallback = shouldUseUnelevatedMacFallback(daemonPath);
    if (!allowUnelevatedFallback && Date.now() - this.lastMacElevationFailureAtMs < macElevationRetryBackoffMs) {
      throw new Error("Previous daemon elevation failed. Wait a few seconds and retry.");
    }

    const context = resolveMacUserContext(daemonPath);
    if (typeof process.getuid === "function" && process.getuid() === 0) {
      this.startMacDaemonChild(daemonPath, context);
    } else {
      const elevate = restartProcessElevatedMac(daemonPath, context);
      if (!elevate.ok) {
        if (allowUnelevatedFallback) {
          console.warn(`daemon elevation failed (${elevate.message}); starting non-root daemon fallback`);
          this.startMacDaemonChild(daemonPath, context);
          await this.waitForReachable();
          return;
        }
        this.lastMacElevationFailureAtMs = Date.now();
        throw new Error(elevate.message);
      }
      this.lastMacElevationFailureAtMs = 0;
    }

    await this.waitForReachable();
  }

  private startMacDaemonChild(daemonPath: string, context: MacDaemonContext): void {
    this.child?.kill();
    this.child = spawn(daemonPath, [], {
      windowsHide: true,
      stdio: "ignore",
      env: {
        ...process.env,
        HOME: context.home,
        USER: context.user,
        LOGNAME: context.user,
        PANGEA_APP_SUPPORT_DIR: context.appSupportDir
      }
    });
    this.child.on("exit", () => {
      this.child = null;
    });
  }

  stop(): void {
    if (this.child) {
      this.child.kill();
      this.child = null;
    }
  }

  private async safeApiReady(): Promise<boolean> {
    try {
      await this.client.getStatus();
      return true;
    } catch {
      return false;
    }
  }

  private async waitForReachable(): Promise<void> {
    for (let attempt = 0; attempt < 40; attempt += 1) {
      await sleep(250);
      const reachable = await this.safeApiReady();
      if (reachable) {
        return;
      }
    }

    throw new Error("daemon did not become reachable");
  }

  private resolveDaemonPath(): string | null {
    if (app.isPackaged) {
      const bundledPath = getBundledDaemonPath();
      return fs.existsSync(bundledPath) ? bundledPath : null;
    }

    const candidates: string[] = [];
    if (process.platform === "win32") {
      candidates.push(
        path.resolve(process.cwd(), "..", "..", "daemon", "bin", "PangeaDaemon.exe"),
        path.resolve(process.cwd(), "daemon", "bin", "PangeaDaemon.exe"),
        path.resolve(app.getAppPath(), "..", "..", "daemon", "bin", "PangeaDaemon.exe")
      );
    }

    for (const candidate of candidates) {
      if (fs.existsSync(candidate)) {
        return candidate;
      }
    }
    return null;
  }
}

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

type MacDaemonContext = {
  user: string;
  home: string;
  appSupportDir: string;
};

function resolveMacUserContext(_daemonPath: string): MacDaemonContext {
  const user = String(process.env.USER ?? os.userInfo().username ?? "").trim() || "root";
  const home = String(process.env.HOME ?? os.homedir()).trim() || os.homedir();
  return {
    user,
    home,
    appSupportDir: getAppSupportDir()
  };
}

function stripMacQuarantine(daemonPath: string): void {
  if (process.platform !== "darwin") {
    return;
  }

  // Strip com.apple.quarantine from the daemon and helper binaries.
  // Downloaded zip archives propagate this xattr to all extracted files
  // and macOS Gatekeeper silently kills quarantined unsigned binaries.
  const resourcesDir = path.resolve(path.dirname(daemonPath), "..");
  const targets = [
    path.dirname(daemonPath),
    path.join(resourcesDir, "bin")
  ].filter((t) => fs.existsSync(t));

  for (const target of targets) {
    spawnSync("/usr/bin/xattr", ["-dr", "com.apple.quarantine", target], {
      stdio: "ignore",
      shell: false
    });
  }
}

function restartProcessElevatedMac(filePath: string, context: MacDaemonContext): { ok: boolean; message: string } {
  const daemonPath = shSingleQuoteMac(filePath);
  const resourcesDir = shSingleQuoteMac(path.resolve(path.dirname(filePath), ".."));
  const appSupportDir = shSingleQuoteMac(context.appSupportDir);
  const tokenPath = shSingleQuoteMac(path.join(context.appSupportDir, "daemon-token.txt"));
  const configPath = shSingleQuoteMac(path.join(context.appSupportDir, "config.json"));
  const targetUser = shSingleQuoteMac(context.user);
  const targetHome = shSingleQuoteMac(context.home);
  const shellCommand = [
    "set -e",
    `RESOURCES_DIR=${resourcesDir}`,
    `/usr/bin/xattr -dr com.apple.quarantine "$RESOURCES_DIR/daemon" "$RESOURCES_DIR/bin" >/dev/null 2>&1 || true`,
    `APP_SUPPORT_DIR=${appSupportDir}`,
    `TOKEN_PATH=${tokenPath}`,
    `CONFIG_PATH=${configPath}`,
    `TARGET_USER=${targetUser}`,
    `TARGET_HOME=${targetHome}`,
    `/bin/mkdir -p "$APP_SUPPORT_DIR"`,
    `if [ ! -s "$TOKEN_PATH" ]; then /usr/bin/openssl rand -hex 32 > "$TOKEN_PATH"; fi`,
    `if [ ! -s "$CONFIG_PATH" ]; then /usr/bin/printf '{\\n  "profiles": []\\n}\\n' > "$CONFIG_PATH"; fi`,
    `/usr/sbin/chown "$TARGET_USER" "$APP_SUPPORT_DIR" "$TOKEN_PATH" "$CONFIG_PATH" >/dev/null 2>&1 || true`,
    `/bin/chmod 700 "$APP_SUPPORT_DIR" >/dev/null 2>&1 || true`,
    `/bin/chmod 600 "$TOKEN_PATH" "$CONFIG_PATH" >/dev/null 2>&1 || true`,
    `for daemon_pid in $(/usr/sbin/lsof -tiTCP:8787 -sTCP:LISTEN 2>/dev/null); do /bin/kill -TERM "$daemon_pid" >/dev/null 2>&1 || true; done`,
    "/bin/sleep 0.2",
    `/usr/bin/nohup /usr/bin/env HOME="$TARGET_HOME" USER="$TARGET_USER" LOGNAME="$TARGET_USER" PANGEA_APP_SUPPORT_DIR="$APP_SUPPORT_DIR" ${daemonPath} >/tmp/pangeavpn-daemon.log 2>&1 &`
  ].join("; ");

  const appleScript = `do shell script ${appleScriptString(shellCommand)} with administrator privileges`;
  const result = spawnSync("osascript", ["-e", appleScript], {
    stdio: "pipe",
    shell: false
  });
  const output = combineOutput(result).trim();

  if (result.error) {
    return { ok: false, message: `Failed to request daemon elevation: ${result.error.message}` };
  }
  if (result.status !== 0) {
    return {
      ok: false,
      message: output
        ? `Daemon elevation failed: ${output}`
        : "Daemon elevation was cancelled or failed. Approve the macOS admin prompt to continue."
    };
  }
  return { ok: true, message: "" };
}

function hasManagedMacLaunchDaemon(): boolean {
  return fs.existsSync(macLaunchDaemonPlist);
}

function shouldUseManagedMacLaunchDaemon(daemonPath: string): boolean {
  if (!app.isPackaged || process.platform !== "darwin") {
    return false;
  }

  const normalizedDaemonPath = path.normalize(daemonPath);
  const applicationsPrefix = path.normalize("/Applications") + path.sep;
  if (!normalizedDaemonPath.startsWith(applicationsPrefix)) {
    return false;
  }

  const expectedSuffix = path.normalize(path.join("Contents", "Resources", "daemon", "daemon"));
  return normalizedDaemonPath.endsWith(expectedSuffix);
}

function shouldUseUnelevatedMacFallback(daemonPath: string): boolean {
  return app.isPackaged && process.platform === "darwin" && !shouldUseManagedMacLaunchDaemon(daemonPath);
}

function kickstartManagedMacLaunchDaemon(): { ok: boolean; message: string } {
  const serviceName = `system/${macLaunchDaemonLabel}`;
  const result = spawnSync("/bin/launchctl", ["kickstart", "-k", serviceName], {
    stdio: "ignore",
    shell: false
  });

  if (!result.error && result.status === 0) {
    return { ok: true, message: "" };
  }

  const probe = spawnSync("/bin/launchctl", ["print", serviceName], {
    stdio: "pipe",
    shell: false
  });
  const details = combineOutput(probe).toLowerCase();
  if (details.includes("could not find service") || details.includes("service does not exist")) {
    return {
      ok: false,
      message: "Installed daemon service is missing. Reinstall PangeaVPN.pkg."
    };
  }
  if (details.includes("not privileged") || details.includes("operation not permitted")) {
    return {
      ok: false,
      message: "Installed daemon service is not reachable. Reinstall PangeaVPN.pkg to repair launchd registration."
    };
  }

  return {
    ok: false,
    message: "Installed daemon service is not running. Reinstall PangeaVPN.pkg to repair it."
  };
}

function startProcessElevatedWindows(filePath: string, args: string[]): { ok: boolean; message: string } {
  const escapedPath = psSingleQuote(filePath);
  const escapedWorkingDir = psSingleQuote(path.dirname(filePath));
  const appArgs = args.map((arg) => `'${psSingleQuote(arg)}'`).join(", ");
  const launchDaemon = appArgs.length > 0
    ? `Start-Process -FilePath '${escapedPath}' -WorkingDirectory '${escapedWorkingDir}' -ArgumentList @(${appArgs}) -WindowStyle Hidden`
    : `Start-Process -FilePath '${escapedPath}' -WorkingDirectory '${escapedWorkingDir}' -WindowStyle Hidden`;
  const innerCommand = [
    "$ErrorActionPreference = 'SilentlyContinue'",
    "$daemonPids = @()",
    "$daemonPids += (Get-Process -Name daemon,PangeaDaemon -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id)",
    "$daemonPids += (Get-NetTCPConnection -LocalAddress '127.0.0.1' -LocalPort 8787 -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess)",
    "$daemonPids = $daemonPids | Where-Object { $_ } | Select-Object -Unique",
    "foreach ($daemonPid in $daemonPids) { Stop-Process -Id $daemonPid -Force -ErrorAction SilentlyContinue }",
    launchDaemon
  ].join("; ");
  const encodedInner = psEncodedCommand(innerCommand);
  const command = [
    "Start-Process -FilePath 'powershell.exe' -Verb RunAs -WindowStyle Hidden -ArgumentList @(",
    "  '-NoProfile',",
    "  '-ExecutionPolicy', 'Bypass',",
    `  '-EncodedCommand', '${encodedInner}'`,
    ")"
  ].join("\n");
  const encodedOuter = psEncodedCommand(command);

  const result = spawnSync(
    "powershell.exe",
    ["-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodedOuter],
    {
      stdio: "ignore",
      shell: false,
      windowsHide: true
    }
  );

  if (result.error) {
    return { ok: false, message: `Failed to request daemon elevation: ${result.error.message}` };
  }
  if (result.status !== 0) {
    return { ok: false, message: "Daemon elevation was cancelled or failed." };
  }
  return { ok: true, message: "" };
}

function ensureWindowsDaemonServiceRunning(): { ok: boolean; message: string } {
  const expectedExecutable = expectedWindowsServiceDaemonPath();
  const qc = spawnSync("sc.exe", ["qc", "PangeaDaemon"], {
    stdio: "pipe",
    shell: false,
    windowsHide: true
  });
  const qcOutput = combineOutput(qc);
  const qcLower = qcOutput.toLowerCase();
  if ((qc.status ?? 1) !== 0 && qcLower.includes("1060")) {
    return {
      ok: false,
      message: "PangeaDaemon service is not installed. Run the installer Repair option as administrator."
    };
  }
  if ((qc.status ?? 1) === 0) {
    const configuredExecutable = parseServiceExecutablePath(qcOutput);
    if (configuredExecutable && !sameWindowsPath(configuredExecutable, expectedExecutable)) {
      return {
        ok: false,
        message: `PangeaDaemon service path is ${configuredExecutable}, expected ${expectedExecutable}. Run installer repair as administrator.`
      };
    }
  }

  const query = spawnSync("sc.exe", ["query", "PangeaDaemon"], {
    stdio: "pipe",
    shell: false,
    windowsHide: true
  });
  const queryOutput = combineOutput(query);
  if ((query.status ?? 1) !== 0 && queryOutput.toLowerCase().includes("1060")) {
    return {
      ok: false,
      message: "PangeaDaemon service is not installed. Run the installer Repair option as administrator."
    };
  }
  if ((query.status ?? 1) !== 0 && queryOutput.toLowerCase().includes("access is denied")) {
    return {
      ok: false,
      message: "Access denied while checking PangeaDaemon service. Run installer repair as administrator."
    };
  }
  if (queryOutput.toUpperCase().includes("RUNNING")) {
    return { ok: true, message: "" };
  }

  const start = spawnSync("sc.exe", ["start", "PangeaDaemon"], {
    stdio: "pipe",
    shell: false,
    windowsHide: true
  });
  const startOutput = combineOutput(start).toLowerCase();

  if ((start.status ?? 1) === 0) {
    return { ok: true, message: "" };
  }
  if (startOutput.includes("already running")) {
    return { ok: true, message: "" };
  }
  if (startOutput.includes("1060")) {
    return {
      ok: false,
      message: "PangeaDaemon service is missing. Reinstall or run installer repair as administrator."
    };
  }
  if (startOutput.includes("access is denied")) {
    return {
      ok: false,
      message: "PangeaDaemon exists but cannot be started without elevated repair permissions."
    };
  }
  if (startOutput.includes("disabled")) {
    return {
      ok: false,
      message: "PangeaDaemon service is disabled. Enable it in Services or run installer repair."
    };
  }

  return {
    ok: false,
    message: `Failed to start PangeaDaemon service. ${startOutput.trim()}`
  };
}

function expectedWindowsServiceDaemonPath(): string {
  const programData = process.env.ProgramData?.trim() || "C:\\ProgramData";
  return path.join(programData, "PangeaVPN", "PangeaDaemon.exe");
}

function parseServiceExecutablePath(scQcOutput: string): string | null {
  const match = scQcOutput.match(/BINARY_PATH_NAME\s*:\s*(.+)/i);
  if (!match) {
    return null;
  }

  const raw = match[1].trim();
  if (!raw) {
    return null;
  }

  if (raw.startsWith("\"")) {
    const end = raw.indexOf("\"", 1);
    if (end > 1) {
      return raw.slice(1, end);
    }
  }

  const token = raw.split(/\s+/)[0];
  return token || null;
}

function sameWindowsPath(a: string, b: string): boolean {
  return path.normalize(a).toLowerCase() === path.normalize(b).toLowerCase();
}

function combineOutput(result: { stdout?: string | Buffer; stderr?: string | Buffer }): string {
  const out = result.stdout ? result.stdout.toString() : "";
  const err = result.stderr ? result.stderr.toString() : "";
  return `${out}\n${err}`.trim();
}

function psSingleQuote(value: string): string {
  return String(value).replace(/'/g, "''");
}

function psEncodedCommand(value: string): string {
  return Buffer.from(String(value), "utf16le").toString("base64");
}

function shSingleQuoteMac(value: string): string {
  return `'${String(value).replace(/'/g, `'\"'\"'`)}'`;
}

function appleScriptString(value: string): string {
  const escaped = String(value)
    .replace(/\\/g, "\\\\")
    .replace(/"/g, '\\"');
  return `"${escaped}"`;
}
