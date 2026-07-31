import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { resolveNaiveCgoConfig } from "./lib/naive-cgo.mjs";

const rootDir = process.cwd();
const daemonDir = path.join(rootDir, "daemon");
const manifestPath = path.join(daemonDir, "cmd", "daemon", "PangeaDaemon.manifest");
const supportedOSWin10Guid = "8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a";
const outName = process.platform === "win32" ? "PangeaDaemon.exe" : "daemon";
const outPath = path.join(daemonDir, "bin", outName);
const isWin = process.platform === "win32";
const goCmd = resolveGoCommand();

if (!goCmd) {
  console.error("Go executable not found.");
  console.error("Install Go 1.22+ or ensure go is on PATH.");
  process.exit(1);
}

// GOARCH is set by scripts/build-bin/windows.mjs per target (amd64/arm64);
// falls back to the host arch for local `node build-daemon.mjs` runs.
const goArch = process.env.GOARCH || (process.arch === "arm64" ? "arm64" : "amd64");
const naiveCgo = resolveNaiveCgoConfig(goArch, rootDir);
if (naiveCgo) {
  console.log(`naive_cgo: enabled for ${goArch} (pangea_naive lib found and toolchain resolved)`);
}

const env = goEnv(rootDir, naiveCgo);

if (isWin) {
  // Clean any pre-existing .syso that may have incompatible relocations.
  const sysoPath = path.join(daemonDir, "cmd", "daemon", "resource_windows.syso");
  if (fs.existsSync(sysoPath)) {
    fs.unlinkSync(sysoPath);
  }

  const genResult = spawnSync(goCmd, ["generate", "./cmd/daemon"], {
    cwd: daemonDir,
    stdio: "inherit",
    shell: false,
    env
  });
  if (genResult.error || (genResult.status ?? 1) !== 0) {
    console.warn(
      "goversioninfo generation skipped (install with: go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest)"
    );
  }

  // goversioninfo .syso relocations break newer Go linkers ("unknown
  // relocation type 7"); drop it so the build proceeds without resources.
  if (fs.existsSync(sysoPath)) {
    fs.unlinkSync(sysoPath);
    console.warn("removed resource_windows.syso (incompatible with current Go toolchain)");
  }
}

// with_utls is required by VLESS+REALITY: sing-box compiles uTLS out by
// default and REALITY's Start fails at runtime without it.
const buildTags = ["with_utls", ...(naiveCgo ? naiveCgo.tags : [])];
const tagsArgs = ["-tags", buildTags.join(",")];
const buildArgs = isWin
  ? ["build", ...tagsArgs, "-ldflags", windowsLdflags(naiveCgo), "-o", outPath, "./cmd/daemon"]
  : ["build", ...tagsArgs, "-o", outPath, "./cmd/daemon"];

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
    assertWindowsManifest(outPath, naiveCgo);
    stageWindowsWireGuardDll();
  } catch (error) {
    console.error(error instanceof Error ? error.message : String(error));
    process.exit(1);
  }
}

process.exit(0);

// Without a supportedOS manifest Windows reports kernel32 as 6.2, and naive's
// Chromium UA path maps that to WIN8 and hits NOTREACHED, killing the daemon.
function windowsLdflags(naiveCgo) {
  const flags = ["-H=windowsgui"];
  if (!naiveCgo) {
    return flags.join(" ");
  }

  if (!fs.existsSync(manifestPath)) {
    console.error(`application manifest missing at ${manifestPath}`);
    process.exit(1);
  }

  const forwardSlashPath = manifestPath.replaceAll("\\", "/");
  flags.push(`-extldflags=-Wl,/MANIFEST:EMBED,/MANIFESTINPUT:${forwardSlashPath}`);
  return flags.join(" ");
}

// Only external (cgo) links can embed it, so a naive build without the manifest
// is the exact combination that crashes and must never ship.
function assertWindowsManifest(exePath, naiveCgo) {
  if (!naiveCgo) {
    console.warn("no naive_cgo: skipping manifest check (internal link cannot embed one)");
    return;
  }

  const exe = fs.readFileSync(exePath);
  if (!exe.includes(supportedOSWin10Guid)) {
    throw new Error(
      `${exePath} has no supportedOS manifest; naive would crash the daemon on connect`
    );
  }
  console.log("verified embedded supportedOS manifest");
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

function goEnv(projectRoot, naiveCgo) {
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
    GOTMPDIR: goTmp,
    ...(naiveCgo ? naiveCgo.env : {})
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
