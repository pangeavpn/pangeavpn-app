import { spawn, spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const npmCmd = "npm";
const isWin = process.platform === "win32";
const goCmd = resolveGoCommand();
const sudoContext = resolveSudoContext();

runNpmOrExit(["run", "build", "--workspace", "@pangeavpn/shared-types"]);
ensureSudoUserRuntimeFiles();

const daemonWasRunning = await isDaemonReachable();
if (daemonWasRunning && !isWin) {
  console.log("Detected existing daemon on 127.0.0.1:8787, reusing it for dev.");
}

if (daemonWasRunning && isWin) {
  console.log("Detected existing daemon on 127.0.0.1:8787, restarting with latest elevated build.");
  killPort8787();
}

const daemonHandle = isWin ? await startWindowsElevatedDaemon() : daemonWasRunning ? null : await startDaemon();
const desktop = startDesktopProcess();

const children = [desktop];
if (daemonHandle?.managed && daemonHandle.child) {
  children.push(daemonHandle.child);
}

let stopping = false;

function killDaemonSync() {
  if (!isWin) return;
  try {
    spawnSync("taskkill", ["/F", "/IM", "PangeaDaemon.exe"], {
      stdio: "pipe",
      shell: false,
      timeout: 5000
    });
  } catch {
    // best-effort
  }
}

function killPort8787() {
  try {
    if (isWin) {
      const result = spawnSync("netstat", ["-ano", "-p", "TCP"], { stdio: "pipe", shell: false, timeout: 5000 });
      const output = (result.stdout ?? "").toString();
      for (const line of output.split("\n")) {
        if (line.includes("127.0.0.1:8787") && line.includes("LISTENING")) {
          const pid = line.trim().split(/\s+/).pop();
          if (pid && pid !== "0") {
            spawnSync("taskkill", ["/F", "/PID", pid], { stdio: "pipe", shell: false, timeout: 5000 });
          }
        }
      }
    } else {
      const result = spawnSync("lsof", ["-ti", "tcp:8787"], { stdio: "pipe", shell: false, timeout: 5000 });
      const pids = (result.stdout ?? "").toString().trim().split("\n").filter(Boolean);
      for (const pid of pids) {
        spawnSync("kill", ["-9", pid], { stdio: "pipe", shell: false, timeout: 3000 });
      }
    }
  } catch {
    // best-effort
  }
}

function shutdown(exitCode = 0) {
  if (stopping) {
    return;
  }
  stopping = true;

  for (const child of children) {
    if (!child.killed) {
      child.kill("SIGTERM");
    }
  }

  killDaemonSync();

  setTimeout(() => {
    for (const child of children) {
      if (!child.killed) {
        child.kill("SIGKILL");
      }
    }
    process.exit(exitCode);
  }, 1500);
}

if (daemonHandle?.managed && daemonHandle.child) {
  daemonHandle.child.on("exit", (code) => {
    void handleDaemonExit(code);
  });
  daemonHandle.child.on("error", (error) => {
    console.error(`Failed to start daemon process: ${error.message}`);
    shutdown(1);
  });
}

desktop.on("exit", (code) => shutdown(code ?? 1));
desktop.on("error", (error) => {
  console.error(`Failed to start desktop process: ${error.message}`);
  shutdown(1);
});

process.on("SIGINT", () => shutdown(0));
process.on("SIGTERM", () => shutdown(0));
process.on("exit", () => killDaemonSync());

function runOrExit(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    shell: false,
    ...options
  });

  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }

  if (result.status !== 0) {
    process.exit(result.status ?? 1);
  }
}

async function handleDaemonExit(code) {
  if (stopping) {
    return;
  }

  if ((code ?? 1) !== 0 && (await isDaemonReachable())) {
    console.warn("Daemon startup exited, but another daemon is reachable; continuing.");
    return;
  }

  shutdown(code ?? 1);
}

async function startDaemon() {
  const daemonEnv = daemonRuntimeEnv();

  if (goCmd) {
    return {
      managed: true,
      child: spawn(goCmd, ["run", "./cmd/daemon"], {
        cwd: "daemon",
        stdio: "inherit",
        shell: false,
        env: daemonEnv
      })
    };
  }

  const daemonBinary = localDaemonBinaryPath();
  if (fs.existsSync(daemonBinary)) {
    return {
      managed: true,
      child: spawn(daemonBinary, [], {
        cwd: path.dirname(daemonBinary),
        stdio: "inherit",
        shell: false,
        env: daemonEnv
      })
    };
  }

  console.error("Go is not installed or not reachable from this shell.");
  console.error(`Install Go 1.22+ or place a daemon binary at ${daemonBinary}.`);
  process.exit(1);
}

async function startWindowsElevatedDaemon() {
  const daemonBinary = localDaemonBinaryPath();
  if (!goCmd) {
    console.error("Go is required to build PangeaDaemon.exe for elevated Windows dev start.");
    console.error(`Expected daemon binary at ${daemonBinary}.`);
    process.exit(1);
  }

  // Kill any stale daemon left over from a previous crashed session.
  killDaemonSync();

  runOrExit(process.execPath, ["./scripts/build-daemon.mjs"], {
    cwd: process.cwd(),
    shell: false
  });

  const elevateResult = restartDaemonElevatedWindows(daemonBinary);
  if (!elevateResult.ok) {
    console.error(elevateResult.message);
    process.exit(1);
  }

  const ready = await waitForDaemon(8000);
  if (!ready) {
    console.error("Elevated daemon was launched, but 127.0.0.1:8787 did not become reachable.");
    process.exit(1);
  }

  console.log("Started daemon with administrator privileges (UAC).");
  return { managed: false, child: null };
}

function restartDaemonElevatedWindows(filePath) {
  const escapedPath = psSingleQuote(filePath);
  const workingDir = psSingleQuote(path.dirname(filePath));
  const innerCommand = [
    "$ErrorActionPreference = 'SilentlyContinue'",
    `Start-Process -FilePath '${escapedPath}' -WorkingDirectory '${workingDir}' -WindowStyle Hidden`
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
      stdio: "inherit",
      shell: false
    }
  );

  if (result.error) {
    return { ok: false, message: `Failed to request elevation: ${result.error.message}` };
  }
  if (result.status !== 0) {
    return {
      ok: false,
      message: "Daemon elevation was cancelled or failed. Accept the UAC prompt to continue."
    };
  }
  return { ok: true, message: "" };
}

async function waitForDaemon(timeoutMs) {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    if (await isDaemonReachable()) {
      return true;
    }
    await sleep(250);
  }
  return false;
}

function goEnv() {
  const root = path.join(process.cwd(), ".cache");
  const goCache = path.join(root, "go-build");
  const goModCache = path.join(root, "go-mod");
  const goTmp = path.join(root, "go-tmp");

  fs.mkdirSync(goCache, { recursive: true });
  fs.mkdirSync(goModCache, { recursive: true });
  fs.mkdirSync(goTmp, { recursive: true });

  return {
    ...process.env,
    GOMODCACHE: goModCache,
    GOCACHE: goCache,
    GOTMPDIR: goTmp
  };
}

function daemonRuntimeEnv() {
  const env = goEnv();
  if (!sudoContext) {
    return env;
  }

  return {
    ...env,
    HOME: sudoContext.home,
    USER: sudoContext.user,
    LOGNAME: sudoContext.user
  };
}

function startDesktopProcess() {
  const desktopEnv = cleanElectronEnv(process.env);
  if (!sudoContext) {
    return spawn(npmCmd, ["run", "dev", "--workspace", "@pangeavpn/desktop"], {
      stdio: "inherit",
      shell: isWin,
      env: desktopEnv
    });
  }

  console.log(`Detected sudo launch; starting desktop process as ${sudoContext.user}.`);
  const args = [
    "-u",
    sudoContext.user,
    "env",
    `HOME=${sudoContext.home}`,
    `USER=${sudoContext.user}`,
    `LOGNAME=${sudoContext.user}`,
    `PATH=${process.env.PATH ?? ""}`,
    npmCmd,
    "run",
    "dev",
    "--workspace",
    "@pangeavpn/desktop"
  ];
  return spawn("sudo", args, {
    stdio: "inherit",
    shell: false,
    env: desktopEnv
  });
}

function runNpmOrExit(args) {
  if (!sudoContext) {
    runOrExit(npmCmd, args, { shell: isWin });
    return;
  }

  const commandArgs = [
    "-u",
    sudoContext.user,
    "env",
    `HOME=${sudoContext.home}`,
    `USER=${sudoContext.user}`,
    `LOGNAME=${sudoContext.user}`,
    `PATH=${process.env.PATH ?? ""}`,
    npmCmd,
    ...args
  ];
  runOrExit("sudo", commandArgs, { shell: false });
}

function ensureSudoUserRuntimeFiles() {
  if (!sudoContext) {
    return;
  }

  const appDir = path.join(sudoContext.home, "Library", "Application Support", "pangeavpn-desktop");
  const tokenPath = path.join(appDir, "daemon-token.txt");
  const configPath = path.join(appDir, "config.json");
  const initScript = [
    "const crypto = require('node:crypto');",
    "const fs = require('node:fs');",
    "const appDir = process.argv[1];",
    "const tokenPath = process.argv[2];",
    "const configPath = process.argv[3];",
    "fs.mkdirSync(appDir, { recursive: true });",
    "let token = '';",
    "if (fs.existsSync(tokenPath)) { token = String(fs.readFileSync(tokenPath, 'utf8')).trim(); }",
    "if (!token) {",
    "  token = crypto.randomBytes(32).toString('hex');",
    "  fs.writeFileSync(tokenPath, `${token}\\n`, { mode: 0o600 });",
    "}",
    "if (!fs.existsSync(configPath)) {",
    "  fs.writeFileSync(configPath, JSON.stringify({ profiles: [] }, null, 2) + '\\n', { mode: 0o600 });",
    "}"
  ].join(" ");

  const args = [
    "-u",
    sudoContext.user,
    "env",
    `HOME=${sudoContext.home}`,
    `USER=${sudoContext.user}`,
    `LOGNAME=${sudoContext.user}`,
    `PATH=${process.env.PATH ?? ""}`,
    "node",
    "-e",
    initScript,
    appDir,
    tokenPath,
    configPath
  ];

  runOrExit("sudo", args, { shell: false });
}

function cleanElectronEnv(baseEnv) {
  const next = { ...baseEnv };
  delete next.ELECTRON_RUN_AS_NODE;
  return next;
}

function localDaemonBinaryPath() {
  const name = isWin ? "PangeaDaemon.exe" : "daemon";
  return path.join(process.cwd(), "daemon", "bin", name);
}

function resolveGoCommand() {
  const candidates = isWin
    ? [
        "go",
        "C:\\Program Files\\Go\\bin\\go.exe",
        "C:\\Program Files (x86)\\Go\\bin\\go.exe",
        path.join(process.env.LOCALAPPDATA ?? "", "Programs", "Go", "bin", "go.exe")
      ]
    : ["go", "/usr/local/go/bin/go", "/opt/homebrew/bin/go"];

  for (const candidate of candidates) {
    if (!candidate) {
      continue;
    }

    const result = spawnSync(candidate, ["version"], {
      stdio: "ignore",
      shell: false
    });

    if (!result.error && result.status === 0) {
      return candidate;
    }
  }

  return null;
}

async function isDaemonReachable() {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), 500);

  try {
    const response = await fetch("http://127.0.0.1:8787/ping", {
      method: "GET",
      signal: controller.signal
    });
    return response.ok;
  } catch {
    return false;
  } finally {
    clearTimeout(timer);
  }
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function psSingleQuote(value) {
  return String(value).replace(/'/g, "''");
}

function psEncodedCommand(value) {
  return Buffer.from(String(value), "utf16le").toString("base64");
}

function resolveSudoContext() {
  if (isWin) {
    return null;
  }
  if (typeof process.getuid !== "function" || process.getuid() !== 0) {
    return null;
  }

  const sudoUser = String(process.env.SUDO_USER ?? "").trim();
  if (!sudoUser) {
    return null;
  }

  return {
    user: sudoUser,
    home: resolveUserHome(sudoUser)
  };
}

function resolveUserHome(username) {
  const result = spawnSync(
    "dscl",
    [".", "-read", `/Users/${username}`, "NFSHomeDirectory"],
    {
      stdio: ["ignore", "pipe", "ignore"],
      shell: false
    }
  );

  if (!result.error && result.status === 0) {
    const line = result.stdout
      .toString()
      .split("\n")
      .map((item) => item.trim())
      .find((item) => item.toLowerCase().startsWith("nfshomedirectory:"));
    if (line) {
      const home = line.split(":").slice(1).join(":").trim();
      if (home) {
        return home;
      }
    }
  }

  return path.join("/Users", username);
}
