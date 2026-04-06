import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

const rootDir = process.cwd();
const daemonDir = path.join(rootDir, "daemon");
const outName = process.platform === "win32" ? "PangeaDaemon.exe" : "daemon";
const outPath = path.join(daemonDir, "bin", outName);
const isWin = process.platform === "win32";
const goCmd = resolveGoCommand();

if (!goCmd) {
  console.error("Go executable not found.");
  console.error("Install Go 1.22+ or ensure go is on PATH.");
  process.exit(1);
}

const env = goEnv(rootDir);

if (isWin) {
  const genResult = spawnSync(goCmd, ["generate", "./cmd/daemon"], {
    cwd: daemonDir,
    stdio: "inherit",
    shell: false,
    env
  });
  if (genResult.error || (genResult.status ?? 1) !== 0) {
    console.warn("goversioninfo generation skipped (install with: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest)");
  }
}

const buildArgs = isWin
  ? ["build", "-ldflags", "-H=windowsgui", "-o", outPath, "./cmd/daemon"]
  : ["build", "-o", outPath, "./cmd/daemon"];

const result = spawnSync(goCmd, buildArgs, {
  cwd: daemonDir,
  stdio: "inherit",
  shell: false,
  env
});

if (result.error) {
  console.error(result.error.message);
  process.exit(1);
}

if ((result.status ?? 1) !== 0) {
  process.exit(result.status ?? 1);
}

if (isWin) {
  try {
    stageWindowsWireGuardDll();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  }
}

process.exit(0);

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
    const check = spawnSync(candidate, ["version"], {
      stdio: "ignore",
      shell: false
    });
    if (!check.error && check.status === 0) {
      return candidate;
    }
  }

  return null;
}

function goEnv(projectRoot) {
  const root = path.join(projectRoot, ".cache");
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

function stageWindowsWireGuardDll() {
  const archDir = resolveWindowsWireGuardArchDir();
  stageWindowsDll(archDir, "wireguard.dll");
  stageWindowsDll(archDir, "wintun.dll");
}

function stageWindowsDll(archDir, dllName) {
  const sourcePath = path.join(rootDir, "apps", "desktop", "build", archDir, dllName);
  const destinationPath = path.join(daemonDir, "bin", dllName);

  if (!fs.existsSync(sourcePath)) {
    throw new Error(`${dllName} missing for ${archDir} at ${sourcePath}`);
  }

  fs.mkdirSync(path.dirname(destinationPath), { recursive: true });
  fs.copyFileSync(sourcePath, destinationPath);
  console.log(`Staged ${archDir} ${dllName} to ${destinationPath}`);
}

function resolveWindowsWireGuardArchDir() {
  const mapping = {
    x64: "amd64",
    ia32: "x86",
    arm64: "arm64",
    arm: "arm"
  };

  const override = String(process.env.PANGEA_WIREGUARD_ARCH ?? "").trim().toLowerCase();
  if (override) {
    if (!Object.values(mapping).includes(override)) {
      throw new Error(`Unsupported PANGEA_WIREGUARD_ARCH=${override}`);
    }
    return override;
  }

  const mapped = mapping[process.arch];
  if (!mapped) {
    throw new Error(`Unsupported Windows architecture for wireguard.dll selection: ${process.arch}`);
  }
  return mapped;
}
