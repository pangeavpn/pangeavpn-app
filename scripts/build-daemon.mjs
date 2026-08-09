import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { resolveNaiveCgoConfig } from "./lib/naive-cgo.mjs";

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const daemonDir = path.join(rootDir, "daemon");
const daemonCommandDir = path.join(daemonDir, "cmd", "daemon");
const windowsResourcePath = path.join(daemonCommandDir, "resource_windows.syso");
const supportedOSWin10Guid = "8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a";
const manifestPath = path.join(daemonCommandDir, "PangeaDaemon.manifest");
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

// with_utls is required by VLESS+REALITY: sing-box compiles uTLS out by
// default and REALITY's Start fails at runtime without it.
const buildTags = ["with_utls", ...(naiveCgo ? naiveCgo.tags : [])];
const tagsArgs = ["-tags", buildTags.join(",")];
const buildArgs = isWin
  ? ["build", ...tagsArgs, "-ldflags", "-H=windowsgui", "-o", outPath, "./cmd/daemon"]
  : ["build", ...tagsArgs, "-o", outPath, "./cmd/daemon"];

try {
  let result;
  try {
    if (isWin) {
      cleanWindowsResources();
      validateWindowsDlls();
      generateWindowsResources();
    }
    result = spawnSync(goCmd, buildArgs, {
      cwd: daemonDir,
      stdio: "inherit",
      shell: false,
      env
    });
  } finally {
    if (isWin) {
      cleanWindowsResources();
    }
  }

  assertCommandSucceeded(result, "Go daemon build");
  if (isWin) {
    assertWindowsResources(outPath);
    stageWindowsWireGuardDll();
  }
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}

// Strips comments so a commented-out <supportedOS> cannot satisfy the check
// below. Repeats because one pass can splice a fresh <!-- out of the
// surrounding text, and rejects a leftover opener: that means an unterminated
// comment, so the rest of the file is not trustworthy to pattern-match.
function stripHtmlComments(text) {
  let out = text;
  let previous;
  do {
    previous = out;
    out = out.replace(/<!--[\s\S]*?-->/g, "");
  } while (out !== previous);
  if (out.includes("<!--")) {
    throw new Error(`${manifestPath} has an unterminated XML comment`);
  }
  return out;
}

function generateWindowsResources() {
  assertWindowsVersionInfo();
  const manifest = stripHtmlComments(fs.readFileSync(manifestPath, "utf8"));
  const supportedOsPattern = new RegExp(
    `<supportedOS\\s+Id=["']\\{${supportedOSWin10Guid}\\}["']\\s*/?>`,
    "i"
  );
  if (!supportedOsPattern.test(manifest)) {
    throw new Error(`${manifestPath} does not declare Windows 10/11 compatibility`);
  }
  const archFlags = {
    386: [],
    amd64: ["-64"],
    arm: ["-arm"],
    arm64: ["-64", "-arm"]
  }[goArch];
  if (!archFlags) {
    throw new Error(`Unsupported Windows GOARCH=${goArch}`);
  }

  const result = spawnSync(
    "goversioninfo",
    [...archFlags, "-o", windowsResourcePath, "versioninfo.json"],
    {
      cwd: daemonCommandDir,
      stdio: "inherit",
      shell: false,
      env
    }
  );
  try {
    assertCommandSucceeded(result, "Windows resource generation");
  } catch (error) {
    throw new Error(
      `${error instanceof Error ? error.message : String(error)}. Install goversioninfo with: ` +
        "go install github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0"
    );
  }
}

function assertWindowsVersionInfo() {
  const packageVersion = JSON.parse(fs.readFileSync(path.join(rootDir, "package.json"), "utf8")).version;
  const desktopVersion = JSON.parse(
    fs.readFileSync(path.join(rootDir, "apps", "desktop", "package.json"), "utf8")
  ).version;
  if (desktopVersion !== packageVersion) {
    throw new Error(`desktop package version ${desktopVersion} does not match root package version ${packageVersion}`);
  }
  const versionInfo = JSON.parse(fs.readFileSync(path.join(daemonCommandDir, "versioninfo.json"), "utf8"));
  const expected = `${packageVersion}.0`;
  const fixedFileVersion = versionInfo.FixedFileInfo?.FileVersion;
  const fixedProductVersion = versionInfo.FixedFileInfo?.ProductVersion;
  const fixedVersion = [fixedFileVersion?.Major, fixedFileVersion?.Minor, fixedFileVersion?.Patch, fixedFileVersion?.Build].join(".");
  const fixedProduct = [fixedProductVersion?.Major, fixedProductVersion?.Minor, fixedProductVersion?.Patch, fixedProductVersion?.Build].join(".");
  const versions = [fixedVersion, fixedProduct, versionInfo.StringFileInfo?.FileVersion, versionInfo.StringFileInfo?.ProductVersion];
  if (versions.some((version) => version !== expected)) {
    throw new Error(`versioninfo.json must match package version ${expected}; found ${versions.join(", ")}`);
  }
}

function assertCommandSucceeded(result, label) {
  if (result?.error) {
    throw new Error(`${label} failed: ${result.error.message}`);
  }
  if (result?.status !== 0) {
    const detail = result?.signal ? `signal ${result.signal}` : `code ${result?.status ?? "unknown"}`;
    throw new Error(`${label} exited with ${detail}`);
  }
}

// The manifest prevents naive's Chromium code from treating Windows 10/11 as
// Windows 8. Verify the resource table too so an iconless daemon cannot ship.
function assertWindowsResources(exePath) {
  const exe = fs.readFileSync(exePath);
  if (!exe.includes(supportedOSWin10Guid)) {
    throw new Error(`${exePath} has no supportedOS manifest`);
  }
  const resourceTypes = readPeResourceTypes(exe, exePath);
  for (const [type, label] of [[3, "icon"], [14, "icon group"], [16, "version info"], [24, "manifest"]]) {
    if (!resourceTypes.has(type)) {
      throw new Error(`${exePath} has no ${label} resource`);
    }
  }
  console.log("verified embedded Windows icon and supportedOS manifest");
}

function readPeResourceTypes(exe, filePath) {
  if (exe.length < 0x40 || exe.toString("ascii", 0, 2) !== "MZ") {
    throw new Error(`${filePath} is not a PE executable`);
  }
  const peOffset = exe.readUInt32LE(0x3c);
  if (peOffset + 24 > exe.length || exe.toString("ascii", peOffset, peOffset + 4) !== "PE\0\0") {
    throw new Error(`${filePath} has an invalid PE header`);
  }

  const sectionCount = exe.readUInt16LE(peOffset + 6);
  const optionalHeaderSize = exe.readUInt16LE(peOffset + 20);
  const optionalHeaderOffset = peOffset + 24;
  const magic = exe.readUInt16LE(optionalHeaderOffset);
  const dataDirectoryOffset = optionalHeaderOffset + (magic === 0x20b ? 112 : magic === 0x10b ? 96 : 0);
  if (dataDirectoryOffset === optionalHeaderOffset) {
    throw new Error(`${filePath} has an unsupported PE optional header`);
  }
  const resourceRva = exe.readUInt32LE(dataDirectoryOffset + 16);
  const sectionTableOffset = optionalHeaderOffset + optionalHeaderSize;
  let resourceOffset = -1;
  for (let index = 0; index < sectionCount; index++) {
    const sectionOffset = sectionTableOffset + index * 40;
    const virtualSize = exe.readUInt32LE(sectionOffset + 8);
    const virtualAddress = exe.readUInt32LE(sectionOffset + 12);
    const rawSize = exe.readUInt32LE(sectionOffset + 16);
    const rawOffset = exe.readUInt32LE(sectionOffset + 20);
    if (resourceRva >= virtualAddress && resourceRva < virtualAddress + Math.max(virtualSize, rawSize)) {
      resourceOffset = rawOffset + resourceRva - virtualAddress;
      break;
    }
  }
  if (resourceOffset < 0 || resourceOffset + 16 > exe.length) {
    throw new Error(`${filePath} has no readable PE resource directory`);
  }

  const entryCount = exe.readUInt16LE(resourceOffset + 12) + exe.readUInt16LE(resourceOffset + 14);
  const types = new Set();
  for (let index = 0; index < entryCount; index++) {
    const name = exe.readUInt32LE(resourceOffset + 16 + index * 8);
    if ((name & 0x80000000) === 0) {
      types.add(name & 0xffff);
    }
  }
  return types;
}

function cleanWindowsResources() {
  for (const name of [
    "resource_windows.syso",
    "resource_windows_386.syso",
    "resource_windows_amd64.syso",
    "resource_windows_arm.syso",
    "resource_windows_arm64.syso"
  ]) {
    const resourcePath = path.join(daemonCommandDir, name);
    if (fs.existsSync(resourcePath)) {
      fs.unlinkSync(resourcePath);
    }
  }
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
  const archDir = resolveWindowsWireGuardArchDir(goArch);
  const sources = windowsDllSources(archDir);
  assertPeArchitecture(outPath, goArch);
  for (const { dllName, sourcePath } of sources) {
    const destinationPath = path.join(daemonDir, "bin", dllName);
    fs.mkdirSync(path.dirname(destinationPath), { recursive: true });
    fs.copyFileSync(sourcePath, destinationPath);
    console.log(`Staged ${archDir} ${dllName} to ${destinationPath}`);
  }
}

function validateWindowsDlls() {
  const archDir = resolveWindowsWireGuardArchDir(goArch);
  for (const { dllName, sourcePath } of windowsDllSources(archDir)) {
    if (!fs.existsSync(sourcePath)) {
      throw new Error(`${dllName} missing for ${archDir} at ${sourcePath}`);
    }
    assertPeArchitecture(sourcePath, goArch);
  }
}

function windowsDllSources(archDir) {
  return ["wireguard.dll", "wintun.dll"].map((dllName) => ({
    dllName,
    sourcePath: path.join(rootDir, "apps", "desktop", "build", archDir, dllName)
  }));
}

function assertPeArchitecture(filePath, arch) {
  const expectedMachine = { 386: 0x014c, amd64: 0x8664, arm: 0x01c4, arm64: 0xaa64 }[arch];
  if (!expectedMachine) {
    throw new Error(`Unsupported Windows GOARCH=${arch}`);
  }
  const binary = fs.readFileSync(filePath);
  const peOffset = binary.length >= 0x40 ? binary.readUInt32LE(0x3c) : -1;
  const machine = peOffset >= 0 && peOffset + 6 <= binary.length ? binary.readUInt16LE(peOffset + 4) : -1;
  if (machine !== expectedMachine) {
    throw new Error(`${filePath} does not match GOARCH=${arch}`);
  }
}

function resolveWindowsWireGuardArchDir(arch) {
  const mapping = {
    amd64: "amd64",
    386: "x86",
    arm64: "arm64",
    arm: "arm"
  };
  const mapped = mapping[arch];
  if (!mapped) {
    throw new Error(`Unsupported Windows GOARCH=${arch}`);
  }
  return mapped;
}
