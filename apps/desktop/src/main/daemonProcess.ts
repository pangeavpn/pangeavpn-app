import { spawn, spawnSync, type ChildProcess } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { app } from "electron";
import { DaemonClient } from "./daemonClient";
import { getBundledDaemonPath } from "./resourcePaths";
import { ensureUserRuntimeFiles, getAppSupportDir } from "./platformPaths";
import { DaemonNotReadyError } from "./runtimeFiles";

function openDaemonLogStdio(): ["ignore", number, number] | "ignore" {
  try {
    const dir = getAppSupportDir();
    fs.mkdirSync(dir, { recursive: true });
    const logPath = path.join(dir, "daemon.log");
    const fd = fs.openSync(logPath, "a");
    fs.writeSync(fd, `\n--- daemon spawn ${new Date().toISOString()} (pid parent ${process.pid}) ---\n`);
    return ["ignore", fd, fd];
  } catch (err) {
    console.warn("could not open daemon log; falling back to stdio:ignore", err);
    return "ignore";
  }
}

function attachExitLogger(child: ChildProcess, label: string): void {
  child.on("exit", (code, signal) => {
    console.warn(`[daemon ${label}] exited code=${code} signal=${signal}`);
  });
  child.on("error", (err) => {
    console.warn(`[daemon ${label}] spawn error`, err);
  });
}

type EnsureDaemonOptions = {
  forceRestart?: boolean;
};

const macLaunchDaemonLabel = "com.pangea.pangeavpn.daemon";
const macLaunchDaemonPlist = `/Library/LaunchDaemons/${macLaunchDaemonLabel}.plist`;
const macSystemSupportDir = "/Library/Application Support/PangeaVPN";
const macElevationRetryBackoffMs = 10000;

export class DaemonProcessManager {
  private child: ChildProcess | null = null;
  private childGeneration = 0;
  private readonly client: DaemonClient;
  private ensureInFlight: Promise<void> | null = null;
  private ensureInFlightForceRestart = false;
  private lastMacElevationFailureAtMs = 0;

  constructor(client: DaemonClient) {
    this.client = client;
  }

  async ensureRunning(options: EnsureDaemonOptions = {}): Promise<void> {
    const forceRestart = options.forceRestart === true;
    if (this.ensureInFlight) {
      // A pending non-force run can't be trusted to have restarted anything; wait, then retry for real.
      if (forceRestart && !this.ensureInFlightForceRestart) {
        return this.ensureInFlight.catch(() => {}).then(() => this.ensureRunning(options));
      }
      return this.ensureInFlight;
    }

    this.ensureInFlightForceRestart = forceRestart;
    const task = this.ensureRunningInternal(options).finally(() => {
      this.ensureInFlight = null;
      this.ensureInFlightForceRestart = false;
    });
    this.ensureInFlight = task;
    return task;
  }

  async restartElevated(onElevationComplete: () => void = () => {}): Promise<void> {
    if (process.platform === "win32" && app.isPackaged) {
      const restart = await restartWindowsDaemonServiceElevated();
      onElevationComplete();
      if (!restart.ok) {
        throw new Error(restart.message);
      }
      await this.waitForReachable();
      return;
    }

    if (process.platform === "darwin" && app.isPackaged) {
      const daemonPath = this.resolveDaemonPath();
      if (!daemonPath) {
        throw new Error("daemon binary not found for this runtime");
      }

      const restart = shouldUseManagedMacLaunchDaemon(daemonPath) && hasManagedMacLaunchDaemon()
        ? await restartManagedMacLaunchDaemonElevated()
        : await restartProcessElevatedMac(daemonPath, resolveMacUserContext(daemonPath));
      onElevationComplete();
      if (!restart.ok) {
        throw new Error(restart.message);
      }
      await this.waitForReachable();
      return;
    }

    if (process.platform === "linux" && app.isPackaged) {
      const daemonPath = this.resolveDaemonPath();
      if (!daemonPath) {
        throw new Error("daemon binary not found for this runtime");
      }
      await ensureUserRuntimeFiles();
      const restart = await restartLinuxDaemonServiceElevated(daemonPath, getAppSupportDir());
      onElevationComplete();
      if (!restart.ok) {
        throw new Error(restart.message);
      }
      await this.waitForReachable();
      return;
    }

    await this.ensureRunning({ forceRestart: true });
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

      const elevate = await startProcessElevatedWindows(daemonPath, []);
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

      const generation = ++this.childGeneration;
      this.child = spawn(daemonPath, [], {
        windowsHide: true,
        stdio: openDaemonLogStdio()
      });

      attachExitLogger(this.child, "linux");
      this.child.on("exit", () => {
        if (generation === this.childGeneration) {
          this.child = null;
        }
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

    try {
      await ensureUserRuntimeFiles();
    } catch (err) {
      // Token not readable yet is expected right after boot; anything else
      // (permissions, corrupt config) is a real failure the caller must see.
      if (!isTokenNotReadyError(err)) {
        throw err;
      }
    }
    const online = await this.safeApiReady();
    if (!forceRestart && online) {
      return;
    }

    const allowUnelevatedFallback = shouldUseUnelevatedMacFallback(daemonPath);
    const backoffActive = Date.now() - this.lastMacElevationFailureAtMs < macElevationRetryBackoffMs;
    if (backoffActive && !allowUnelevatedFallback) {
      throw new Error("Previous daemon elevation failed. Wait a few seconds and retry.");
    }

    const context = resolveMacUserContext(daemonPath);
    if (typeof process.getuid === "function" && process.getuid() === 0) {
      this.startMacDaemonChild(daemonPath, context);
    } else if (backoffActive && allowUnelevatedFallback) {
      // Recently declined/failed; don't re-prompt the user, just fall back.
      console.warn("skipping repeated admin prompt after a recent elevation failure; starting non-root daemon fallback");
      this.startMacDaemonChild(daemonPath, context);
      await this.waitForReachable();
      return;
    } else {
      const elevate = await restartProcessElevatedMac(daemonPath, context);
      if (!elevate.ok) {
        this.lastMacElevationFailureAtMs = Date.now();
        if (allowUnelevatedFallback) {
          console.warn(`daemon elevation failed (${elevate.message}); starting non-root daemon fallback`);
          this.startMacDaemonChild(daemonPath, context);
          await this.waitForReachable();
          return;
        }
        throw new Error(elevate.message);
      }
      this.lastMacElevationFailureAtMs = 0;
    }

    await this.waitForReachable();
  }

  private startMacDaemonChild(daemonPath: string, context: MacDaemonContext): void {
    this.child?.kill();
    const generation = ++this.childGeneration;
    this.child = spawn(daemonPath, [], {
      windowsHide: true,
      stdio: openDaemonLogStdio(),
      env: {
        ...process.env,
        HOME: context.home,
        USER: context.user,
        LOGNAME: context.user,
        PANGEA_APP_SUPPORT_DIR: context.appSupportDir
      }
    });
    attachExitLogger(this.child, "mac-child");
    this.child.on("exit", () => {
      if (generation === this.childGeneration) {
        this.child = null;
      }
    });
  }

  stop(): void {
    this.childGeneration += 1;
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
      // app.getAppPath() is fixed by Electron, not by the process's (attacker-controllable) cwd.
      const trustedRoot = path.resolve(app.getAppPath(), "..", "..");
      const cwdCandidates = [
        path.resolve(process.cwd(), "..", "..", "daemon", "bin", "PangeaDaemon.exe"),
        path.resolve(process.cwd(), "daemon", "bin", "PangeaDaemon.exe")
      ].filter((candidate) => {
        const relative = path.relative(trustedRoot, candidate);
        return relative === "" || (!relative.startsWith("..") && !path.isAbsolute(relative));
      });

      candidates.push(
        path.resolve(app.getAppPath(), "..", "..", "daemon", "bin", "PangeaDaemon.exe"),
        ...cwdCandidates
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

// The root daemon has not written its token yet; a real failure must not be
// mistaken for it, or callers swallow it instead of retrying.
function isTokenNotReadyError(err: unknown): boolean {
  return err instanceof DaemonNotReadyError;
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

function macTeamIdentifier(target: string): string | null {
  const result = spawnSync("/usr/bin/codesign", ["-dv", "--verbose=4", target], {
    stdio: "pipe",
    shell: false
  });
  const match = combineOutput(result).match(/TeamIdentifier=([A-Za-z0-9]+)/);
  return match ? match[1] : null;
}

// Refuses to elevate a binary whose signature, signer, or location we can't trust.
function verifyMacDaemonBinaryForElevation(daemonPath: string): { ok: boolean; message: string } {
  let stat: fs.Stats;
  try {
    stat = fs.lstatSync(daemonPath);
  } catch {
    return { ok: false, message: "Daemon binary could not be inspected. Reinstall PangeaVPN." };
  }
  if (stat.isSymbolicLink() || (stat.mode & 0o022) !== 0) {
    return { ok: false, message: "Daemon binary location is not secure. Reinstall PangeaVPN." };
  }

  const verify = spawnSync("/usr/bin/codesign", ["--verify", "--strict", daemonPath], {
    stdio: "ignore",
    shell: false
  });
  if (verify.status !== 0) {
    return { ok: false, message: "Daemon binary failed code signature verification. Reinstall PangeaVPN." };
  }

  const daemonTeam = macTeamIdentifier(daemonPath);
  const appTeam = macTeamIdentifier(process.execPath);
  if (!daemonTeam || !appTeam || daemonTeam !== appTeam) {
    return { ok: false, message: "Daemon binary signing identity does not match the app. Reinstall PangeaVPN." };
  }

  return { ok: true, message: "" };
}

async function restartProcessElevatedMac(filePath: string, context: MacDaemonContext): Promise<{ ok: boolean; message: string }> {
  const verification = verifyMacDaemonBinaryForElevation(filePath);
  if (!verification.ok) {
    return verification;
  }

  const daemonPath = shSingleQuoteMac(filePath);
  const resourcesDir = shSingleQuoteMac(path.resolve(path.dirname(filePath), ".."));
  // The root daemon gets the system dir (its own default, and where the app
  // already looks for the token); chowning the user's state dir to root broke
  // every desktop write after the first elevation.
  const appSupportDir = shSingleQuoteMac(macSystemSupportDir);
  const configPath = shSingleQuoteMac(path.join(macSystemSupportDir, "config.json"));
  const targetUser = shSingleQuoteMac(context.user);
  const targetHome = shSingleQuoteMac(context.home);
  const shellCommand = [
    "set -e",
    `RESOURCES_DIR=${resourcesDir}`,
    `/usr/bin/xattr -dr com.apple.quarantine "$RESOURCES_DIR/daemon" "$RESOURCES_DIR/bin" >/dev/null 2>&1 || true`,
    `APP_SUPPORT_DIR=${appSupportDir}`,
    `CONFIG_PATH=${configPath}`,
    `TARGET_USER=${targetUser}`,
    `TARGET_HOME=${targetHome}`,
    `LOG_PATH="$APP_SUPPORT_DIR/daemon-elevated.log"`,
    `/bin/mkdir -p "$APP_SUPPORT_DIR"`,
    // rm -f unlinks any pre-planted symlink; the redirect below then creates a fresh root-owned file.
    `/bin/rm -f "$LOG_PATH"`,
    // The state dir stays traversable, so the log needs its own mode: it
    // carries daemon stderr and must not be world-readable.
    `/usr/bin/touch "$LOG_PATH" && /bin/chmod 600 "$LOG_PATH" >/dev/null 2>&1 || true`,
    // The daemon runs as root and mints its own token root:admin 0640, so the
    // dir stays root-owned and traversable rather than being handed to the user.
    `if [ ! -s "$CONFIG_PATH" ]; then /usr/bin/printf '{\\n  "profiles": []\\n}\\n' > "$CONFIG_PATH"; fi`,
    `/usr/sbin/chown root "$APP_SUPPORT_DIR" "$CONFIG_PATH" >/dev/null 2>&1 || true`,
    `/bin/chmod 755 "$APP_SUPPORT_DIR" >/dev/null 2>&1 || true`,
    `/bin/chmod 600 "$CONFIG_PATH" >/dev/null 2>&1 || true`,
    `for daemon_pid in $(/usr/sbin/lsof -tiTCP:8787 -sTCP:LISTEN 2>/dev/null); do /bin/kill -TERM "$daemon_pid" >/dev/null 2>&1 || true; done`,
    "/bin/sleep 0.2",
    `/usr/bin/nohup /usr/bin/env HOME="$TARGET_HOME" USER="$TARGET_USER" LOGNAME="$TARGET_USER" PANGEA_APP_SUPPORT_DIR="$APP_SUPPORT_DIR" ${daemonPath} >"$LOG_PATH" 2>&1 &`
  ].join("; ");

  const appleScript = `do shell script ${appleScriptString(shellCommand)} with administrator privileges`;
  const result = await runProcess("osascript", ["-e", appleScript]);
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

async function restartManagedMacLaunchDaemonElevated(): Promise<{ ok: boolean; message: string }> {
  const shellCommand = `/bin/launchctl kickstart -k system/${macLaunchDaemonLabel}`;
  const appleScript = `do shell script ${appleScriptString(shellCommand)} with administrator privileges`;
  const result = await runProcess("osascript", ["-e", appleScript]);
  const output = combineOutput(result).trim();

  if (result.error) {
    return { ok: false, message: `Failed to request daemon elevation: ${result.error.message}` };
  }
  if (result.status !== 0) {
    return {
      ok: false,
      message: output
        ? `Daemon service restart failed: ${output}`
        : "Daemon service restart was cancelled or failed."
    };
  }
  return { ok: true, message: "" };
}

async function startProcessElevatedWindows(filePath: string, args: string[]): Promise<{ ok: boolean; message: string }> {
  const escapedPath = psSingleQuote(filePath);
  const escapedWorkingDir = psSingleQuote(path.dirname(filePath));
  const appArgs = args.map((arg) => `'${psSingleQuote(arg)}'`).join(", ");
  const launchDaemon = appArgs.length > 0
    ? `Start-Process -FilePath '${escapedPath}' -WorkingDirectory '${escapedWorkingDir}' -ArgumentList @(${appArgs}) -WindowStyle Hidden`
    : `Start-Process -FilePath '${escapedPath}' -WorkingDirectory '${escapedWorkingDir}' -WindowStyle Hidden`;
  const innerCommand = [
    "$ErrorActionPreference = 'SilentlyContinue'",
    // The elevated daemon inherits this process's environment, so clear the
    // state-dir override — otherwise user-level code could redirect the
    // elevated daemon's token/config/kill-switch state to a directory it owns.
    "Remove-Item Env:PANGEA_APP_SUPPORT_DIR -ErrorAction SilentlyContinue",
    "$daemonPids = @()",
    "$daemonPids += (Get-Process -Name daemon,PangeaDaemon -ErrorAction SilentlyContinue | Select-Object -ExpandProperty Id)",
    "$daemonPids += (Get-NetTCPConnection -LocalAddress '127.0.0.1' -LocalPort 8787 -ErrorAction SilentlyContinue | Select-Object -ExpandProperty OwningProcess)",
    "$daemonPids = $daemonPids | Where-Object { $_ } | Select-Object -Unique",
    "foreach ($daemonPid in $daemonPids) { Stop-Process -Id $daemonPid -Force -ErrorAction SilentlyContinue }",
    // The daemon refuses a state dir the installing user owns; give it the
    // Administrators-owned, admin-only ACL the installer applies.
    "$stateDir = Join-Path $env:SystemDrive 'ProgramData\\PangeaVPN'",
    "if (Test-Path -LiteralPath $stateDir) { takeown.exe /F $stateDir /A | Out-Null; icacls.exe $stateDir /inheritance:r /grant:r '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-544:(OI)(CI)F' | Out-Null }",
    launchDaemon
  ].join("; ");
  const encodedInner = psEncodedCommand(innerCommand);
  const command = [
    "$ErrorActionPreference = 'Stop'",
    "try {",
    "  $process = Start-Process -FilePath 'powershell.exe' -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ArgumentList @(",
    "    '-NoProfile',",
    "    '-ExecutionPolicy', 'Bypass',",
    `    '-EncodedCommand', '${encodedInner}'`,
    "  )",
    "  exit $process.ExitCode",
    "} catch { exit 1 }"
  ].join("\n");
  const encodedOuter = psEncodedCommand(command);

  const result = await runProcess(
    "powershell.exe",
    ["-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", encodedOuter],
    {
      windowsHide: true
    }
  );

  if (result.error) {
    return { ok: false, message: `Failed to request daemon elevation: ${result.error.message}` };
  }
  if (result.status !== 0) {
    return { ok: false, message: "Administrator approval was cancelled or the daemon could not be started." };
  }
  return { ok: true, message: "" };
}

async function restartWindowsDaemonServiceElevated(): Promise<{ ok: boolean; message: string }> {
  const validation = validateWindowsDaemonServiceInstallation();
  if (!validation.ok) {
    return validation;
  }

  const innerCommand = [
    "$ErrorActionPreference = 'Stop'",
    "$service = Get-Service -Name 'PangeaDaemon'",
    "if ($service.Status -eq 'Stopped') { Start-Service -Name 'PangeaDaemon' } else { Restart-Service -Name 'PangeaDaemon' -Force }",
    "$service.WaitForStatus('Running', [TimeSpan]::FromSeconds(15))"
  ].join("; ");
  const encodedInner = psEncodedCommand(innerCommand);
  const outerCommand = [
    "$ErrorActionPreference = 'Stop'",
    "try {",
    "  $process = Start-Process -FilePath 'powershell.exe' -Verb RunAs -WindowStyle Hidden -Wait -PassThru -ArgumentList @(",
    "    '-NoProfile',",
    "    '-ExecutionPolicy', 'Bypass',",
    `    '-EncodedCommand', '${encodedInner}'`,
    "  )",
    "  exit $process.ExitCode",
    "} catch { exit 1 }"
  ].join("\n");
  const result = await runProcess(
    "powershell.exe",
    ["-NoProfile", "-ExecutionPolicy", "Bypass", "-EncodedCommand", psEncodedCommand(outerCommand)],
    {
      windowsHide: true
    }
  );

  if (result.error) {
    return { ok: false, message: `Failed to request daemon elevation: ${result.error.message}` };
  }
  if (result.status !== 0) {
    return { ok: false, message: "Administrator approval was cancelled or the daemon service could not be restarted." };
  }
  return { ok: true, message: "" };
}

async function restartLinuxDaemonServiceElevated(
  daemonPath: string,
  appSupportDir: string
): Promise<{ ok: boolean; message: string }> {
  const serviceInstalled = [
    "/etc/systemd/system/pangea-daemon.service",
    "/lib/systemd/system/pangea-daemon.service",
    "/usr/lib/systemd/system/pangea-daemon.service"
  ].some((servicePath) => fs.existsSync(servicePath));
  if (!serviceInstalled && !fs.existsSync("/usr/bin/pkexec")) {
    return {
      ok: false,
      message: "PolicyKit is required to restart the VPN service. Install policykit-1 or reinstall PangeaVPN."
    };
  }
  const command = serviceInstalled ? "systemctl" : "/usr/bin/pkexec";
  const args = serviceInstalled
    ? ["restart", "pangea-daemon"]
    : [
        "/bin/sh",
        "-c",
        "for proc in /proc/[0-9]*; do [ \"$(readlink \"$proc/exe\" 2>/dev/null)\" = \"$2\" ] && kill -TERM \"${proc##*/}\" >/dev/null 2>&1 || true; done; /bin/sleep 0.2; /usr/bin/nohup /usr/bin/env PANGEA_APP_SUPPORT_DIR=\"$1\" \"$2\" >/dev/null 2>&1 &",
        "pangeavpn-recovery",
        appSupportDir,
        daemonPath
      ];
  const result = await runProcess(command, args);
  const output = combineOutput(result).trim();

  if (result.error) {
    return { ok: false, message: `Failed to request daemon elevation: ${result.error.message}` };
  }
  if (result.status !== 0) {
    return {
      ok: false,
      message: output
        ? `Daemon service restart failed: ${output}`
        : "Daemon service restart was cancelled or failed."
    };
  }
  return { ok: true, message: "" };
}

type ProcessResult = {
  status: number | null;
  stdout: Buffer;
  stderr: Buffer;
  error?: Error;
};

function runProcess(
  command: string,
  args: string[],
  options: { windowsHide?: boolean } = {}
): Promise<ProcessResult> {
  return new Promise((resolve) => {
    let settled = false;
    const stdout: Buffer[] = [];
    const stderr: Buffer[] = [];
    const finish = (result: ProcessResult) => {
      if (settled) return;
      settled = true;
      resolve(result);
    };

    let child: ChildProcess;
    try {
      child = spawn(command, args, {
        shell: false,
        windowsHide: options.windowsHide === true,
        stdio: ["ignore", "pipe", "pipe"]
      });
    } catch (error) {
      finish({
        status: null,
        stdout: Buffer.alloc(0),
        stderr: Buffer.alloc(0),
        error: error instanceof Error ? error : new Error(String(error))
      });
      return;
    }

    child.stdout?.on("data", (chunk: Buffer) => stdout.push(Buffer.from(chunk)));
    child.stderr?.on("data", (chunk: Buffer) => stderr.push(Buffer.from(chunk)));
    child.once("error", (error) => {
      finish({ status: null, stdout: Buffer.concat(stdout), stderr: Buffer.concat(stderr), error });
    });
    child.once("close", (status) => {
      finish({ status, stdout: Buffer.concat(stdout), stderr: Buffer.concat(stderr) });
    });
  });
}

function ensureWindowsDaemonServiceRunning(): { ok: boolean; message: string } {
  const validation = validateWindowsDaemonServiceInstallation();
  if (!validation.ok) {
    return validation;
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

function validateWindowsDaemonServiceInstallation(): { ok: boolean; message: string } {
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
  if ((qc.status ?? 1) !== 0) {
    return {
      ok: false,
      message: "PangeaDaemon service configuration could not be verified. Run installer repair as administrator."
    };
  }
  if ((qc.status ?? 1) === 0) {
    const configuredExecutable = parseServiceExecutablePath(qcOutput);
    if (!configuredExecutable) {
      return {
        ok: false,
        message: "PangeaDaemon service path could not be verified. Run installer repair as administrator."
      };
    }
    if (!sameWindowsPath(configuredExecutable, expectedExecutable)) {
      return {
        ok: false,
        message: `PangeaDaemon service path is ${configuredExecutable}, expected ${expectedExecutable}. Run installer repair as administrator.`
      };
    }
  }
  return { ok: true, message: "" };
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
    return null;
  }

  // Unquoted BINARY_PATH_NAME has no reliable delimiter for a path containing
  // spaces, so trust the whole remainder rather than truncate at the first one.
  return raw || null;
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
