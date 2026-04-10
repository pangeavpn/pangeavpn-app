import fs from "node:fs/promises";
import fsSync from "node:fs";
import path from "node:path";
import process from "node:process";
import { spawnSync } from "node:child_process";
import { npmCmd, relPath, rootDir, runOrThrow, sha256File, writeJson } from "./shared.mjs";

const platformName = "mac";
const daemonDir = path.join(rootDir, "daemon");
const daemonBinDir = path.join(daemonDir, "bin");
const daemonLivePath = path.join(daemonBinDir, "daemon");
const macToolsSourceDir = path.join(rootDir, "apps", "desktop", "resources", "bin", "mac");
const macToolsOutputDir = path.join(rootDir, "dist", "bin", platformName, "bin", "mac");
const verifyPackResourcesScript = path.join(rootDir, "apps", "desktop", "scripts", "verify-pack-resources.mjs");
const goCmd = resolveGoCommand();
const archTargets = [
  { arch: "arm64", goArch: "arm64" },
  { arch: "x64", goArch: "amd64" }
];
const mebibyte = 1024 * 1024;
const installerDmgMinSizeBytes = 256 * mebibyte;
const installerDmgPaddingBytes = 128 * mebibyte;
const installerDmgAlignmentBytes = 32 * mebibyte;
const installerDmgGrowthFactor = 1.25;

if (process.platform !== "darwin") {
  console.error("build-bin:mac must run on a macOS host.");
  process.exit(1);
}
if (!goCmd) {
  console.error("Go executable not found.");
  console.error("Install Go 1.22+ or ensure go is available.");
  process.exit(1);
}

await cleanOutput();
await copyStandaloneMacTools();

runOrThrow(npmCmd, ["install", "--workspace", "@pangeavpn/desktop", "--include=dev"], { cwd: rootDir, shell: true });
runOrThrow(npmCmd, ["run", "build", "--workspace", "@pangeavpn/shared-types"], { cwd: rootDir, shell: true });
runOrThrow(npmCmd, ["run", "build", "--workspace", "@pangeavpn/desktop"], { cwd: rootDir, shell: true });

const artifacts = [];
for (const target of archTargets) {
  const daemonArchPath = path.join(daemonBinDir, `daemon-${target.arch}`);
  buildDaemon(target.goArch, daemonArchPath);

  await fs.copyFile(daemonArchPath, daemonLivePath);
  await fs.chmod(daemonLivePath, 0o755);
  runOrThrow(process.execPath, [verifyPackResourcesScript], { cwd: rootDir, shell: false });

  const packagingStartedAtMs = Date.now();
  runOrThrow(
    npmCmd,
    [
      "exec",
      "--workspace",
      "@pangeavpn/desktop",
      "electron-builder",
      "--",
      "--projectDir",
      ".",
      "--mac",
      "pkg",
      "zip",
      `--${target.arch}`,
      "--publish",
      "never",
      "--config.electronVersion=34.1.0"
    ],
    {
      cwd: rootDir,
      shell: true
    }
  );

  verifyPackagedMacAppBundle(resolveInstallerAppPath(target.arch), target.arch);

  const archArtifacts = await collectArchArtifacts(target.arch, target.goArch, daemonArchPath, packagingStartedAtMs);
  artifacts.push(...archArtifacts);
}

const manifestPath = await writeManifest(artifacts);
console.log(`build-bin:mac completed. Manifest: ${manifestPath}`);

async function cleanOutput() {
  const macOutputRoot = path.join(rootDir, "dist", "bin", platformName);
  const installersDir = path.join(rootDir, "dist", "installers");
  await removeOutputPath(macOutputRoot);
  await removeOutputPath(installersDir);
  for (const target of archTargets) {
    await fs.mkdir(path.join(macOutputRoot, "installer", target.arch), { recursive: true });
    await fs.mkdir(path.join(macOutputRoot, "portable", target.arch), { recursive: true });
  }
  await fs.mkdir(path.join(macOutputRoot, "daemon"), { recursive: true });
  await fs.mkdir(path.join(macOutputRoot, "bin", "mac"), { recursive: true });
  await fs.mkdir(daemonBinDir, { recursive: true });
}

async function removeOutputPath(targetPath) {
  try {
    await fs.rm(targetPath, { recursive: true, force: true });
  } catch (error) {
    if (isPermissionError(error)) {
      throw new Error(
        `Cannot remove ${targetPath} (permission denied). ` +
        "An older relocatable pkg install may have written root-owned files into dist/. " +
        `Repair ownership, then retry: sudo chown -R \"$(id -un)\":staff ${targetPath}`
      );
    }
    throw error;
  }
}

function isPermissionError(error) {
  return Boolean(
    error &&
      typeof error === "object" &&
      "code" in error &&
      ((error.code === "EACCES") || (error.code === "EPERM"))
  );
}

async function copyStandaloneMacTools() {
  const entries = await fs.readdir(macToolsSourceDir, { withFileTypes: true });
  for (const entry of entries) {
    if (!entry.isFile()) {
      continue;
    }

    const src = path.join(macToolsSourceDir, entry.name);
    const dst = path.join(macToolsOutputDir, entry.name);
    await fs.copyFile(src, dst);
    await fs.chmod(dst, 0o755).catch(() => {});
  }
}

function buildDaemon(goArch, outPath) {
  runOrThrow(goCmd, ["build", "-o", outPath, "./cmd/daemon"], {
    cwd: daemonDir,
    shell: false,
    env: {
      ...goEnv(),
      GOOS: "darwin",
      GOARCH: goArch
    }
  });
}

async function collectArchArtifacts(arch, goArch, daemonArchPath, packagingStartedAtMs) {
  const installersDir = path.join(rootDir, "dist", "installers");
  const macBinDir = path.join(rootDir, "dist", "bin", platformName);
  const installerOut = path.join(macBinDir, "installer", arch);
  const portableOut = path.join(macBinDir, "portable", arch);
  const daemonOut = path.join(macBinDir, "daemon");
  const daemonOutPath = path.join(daemonOut, `daemon-${arch}`);

  const installerEntries = await fs.readdir(installersDir, { withFileTypes: true });
  const artifacts = [];
  for (const entry of installerEntries) {
    if (!entry.isFile()) {
      continue;
    }
    const lower = entry.name.toLowerCase();
    if (!lower.endsWith(".pkg") && !lower.endsWith(".dmg") && !lower.endsWith(".zip")) {
      continue;
    }

    const fullPath = path.join(installersDir, entry.name);
    const stat = await fs.stat(fullPath);
    artifacts.push({
      name: entry.name,
      fullPath,
      mtimeMs: stat.mtimeMs
    });
  }

  const currentRun = artifacts.filter((item) => item.mtimeMs >= packagingStartedAtMs - 1000);
  const selected = currentRun.length > 0 ? currentRun : artifacts;
  const pkgCandidates = selected.filter((item) => item.name.toLowerCase().endsWith(".pkg"));
  const dmgCandidates = selected.filter((item) => item.name.toLowerCase().endsWith(".dmg"));
  const zipCandidates = selected.filter((item) => item.name.toLowerCase().endsWith(".zip"));
  if (pkgCandidates.length === 0 && dmgCandidates.length === 0) {
    throw new Error(`Installer artifact (.pkg or .dmg) not found for ${arch}`);
  }
  if (zipCandidates.length === 0) {
    throw new Error(`ZIP artifact not found for ${arch}`);
  }

  const installerFile = pkgCandidates.length > 0 ? newestByMtime(pkgCandidates) : newestByMtime(dmgCandidates);
  const zipFile = newestByMtime(zipCandidates);
  const copied = [];
  const installerName = arch === "x64" ? addArchSuffix(installerFile.name, "-x64") : installerFile.name;
  const installerDestPath = path.join(installerOut, installerName);
  copied.push(await copyArtifact(installerFile.fullPath, installerDestPath, "installer", arch));

  const installerDmg = await bundleInstallerDmg(installerDestPath, installerName, installerOut, arch);
  if (installerDmg) {
    copied.push(installerDmg);
  }

  const portableZipOutPath = path.join(portableOut, zipFile.name);
  copied.push(await copyArtifact(zipFile.fullPath, portableZipOutPath, "portable", arch));
  await materializePortableAppBundle(portableZipOutPath, portableOut, arch);
  copied.push(await copyArtifact(daemonArchPath, daemonOutPath, "daemon", arch, goArch));
  return copied;
}

function newestByMtime(entries) {
  return [...entries].sort((a, b) => b.mtimeMs - a.mtimeMs)[0];
}

function addArchSuffix(fileName, suffix) {
  const ext = path.extname(fileName);
  return `${fileName.slice(0, -ext.length)}${suffix}${ext}`;
}

async function getDirectorySizeBytes(targetDir) {
  const entries = await fs.readdir(targetDir, { withFileTypes: true });
  let total = 0;

  for (const entry of entries) {
    const entryPath = path.join(targetDir, entry.name);
    if (entry.isDirectory()) {
      total += await getDirectorySizeBytes(entryPath);
      continue;
    }

    const stat = await fs.lstat(entryPath);
    total += stat.size;
  }

  return total;
}

function roundUpToMultiple(value, multiple) {
  return Math.ceil(value / multiple) * multiple;
}

function getInstallerDmgSizeArg(payloadBytes) {
  const estimatedBytes = Math.max(
    installerDmgMinSizeBytes,
    payloadBytes + installerDmgPaddingBytes,
    Math.ceil(payloadBytes * installerDmgGrowthFactor)
  );

  return `${Math.ceil(roundUpToMultiple(estimatedBytes, installerDmgAlignmentBytes) / mebibyte)}m`;
}

async function bundleInstallerDmg(pkgPath, pkgName, installerOut, arch) {
  const installScript = path.join(rootDir, "scripts", "install-mac.sh");
  if (!fsSync.existsSync(installScript)) {
    console.warn("install-mac.sh not found, skipping installer DMG bundle.");
    return null;
  }

  const dmgName = `${path.basename(pkgName, path.extname(pkgName))}-installer.dmg`;
  const dmgPath = path.join(installerOut, dmgName);
  const volumeName = `PangeaVPN Installer (${arch})`;
  const stagingDir = path.join(installerOut, ".bundle-staging");

  await fs.rm(stagingDir, { recursive: true, force: true });
  await fs.mkdir(stagingDir, { recursive: true });
  await fs.copyFile(pkgPath, path.join(stagingDir, pkgName));
  await fs.copyFile(installScript, path.join(stagingDir, "install-mac.sh"));
  await fs.chmod(path.join(stagingDir, "install-mac.sh"), 0o755);

  // Remove stale DMG if present (hdiutil won't overwrite)
  await fs.rm(dmgPath, { force: true });
  const stagingSizeBytes = await getDirectorySizeBytes(stagingDir);
  const dmgSizeArg = getInstallerDmgSizeArg(stagingSizeBytes);

  try {
    runOrThrow("hdiutil", [
      "create",
      "-volname", volumeName,
      "-srcfolder", stagingDir,
      "-ov",
      "-format", "UDZO",
      "-size", dmgSizeArg,
      dmgPath
    ], { cwd: rootDir, shell: false });

    const stat = await fs.stat(dmgPath);
    return {
      type: "installer-bundle",
      arch,
      fileName: dmgName,
      sourcePath: relPath(dmgPath),
      outputPath: relPath(dmgPath),
      sizeBytes: stat.size,
      sha256: await sha256File(dmgPath)
    };
  } catch (error) {
    await fs.rm(dmgPath, { force: true }).catch(() => {});
    throw error;
  } finally {
    await fs.rm(stagingDir, { recursive: true, force: true });
  }
}

function resolveInstallerAppPath(arch) {
  const folder = arch === "arm64" ? "mac-arm64" : "mac";
  return path.join(rootDir, "dist", "installers", folder, "PangeaVPN.app");
}

function verifyPackagedMacAppBundle(appBundlePath, arch) {
  const requiredFiles = [
    path.join(appBundlePath, "Contents", "MacOS", "PangeaVPN"),
    path.join(appBundlePath, "Contents", "Resources", "daemon", "daemon"),
    path.join(appBundlePath, "Contents", "Resources", "bin", "mac", "wireguard-go"),
    path.join(appBundlePath, "Contents", "Resources", "bin", "mac", "wg")
  ];

  const cloakCandidates = [
    path.join(appBundlePath, "Contents", "Resources", "bin", "mac", "ck-client"),
    path.join(appBundlePath, "Contents", "Resources", "bin", "mac", "cloak")
  ];

  for (const filePath of requiredFiles) {
    if (!fsSync.existsSync(filePath)) {
      throw new Error(`Missing required file in ${arch} app bundle: ${filePath}`);
    }
  }

  if (!cloakCandidates.some((candidate) => fsSync.existsSync(candidate))) {
    throw new Error(
      `Missing cloak binary in ${arch} app bundle. Expected one of: ${cloakCandidates.join(", ")}`
    );
  }
}

async function materializePortableAppBundle(zipPath, portableOut, arch) {
  const appPath = path.join(portableOut, "PangeaVPN.app");
  await fs.rm(appPath, { recursive: true, force: true });
  runOrThrow("ditto", ["-x", "-k", zipPath, portableOut], {
    cwd: rootDir,
    shell: false
  });

  if (!fsSync.existsSync(appPath)) {
    throw new Error(`Portable ${arch} app bundle was not extracted from ${zipPath}`);
  }

  verifyPackagedMacAppBundle(appPath, `${arch} portable`);
}

async function copyArtifact(sourcePath, destinationPath, type, arch, goArch = null) {
  await fs.copyFile(sourcePath, destinationPath);
  if (type === "daemon") {
    await fs.chmod(destinationPath, 0o755);
  }
  const stat = await fs.stat(destinationPath);
  return {
    type,
    arch,
    ...(goArch ? { goArch } : {}),
    fileName: path.basename(destinationPath),
    sourcePath: relPath(sourcePath),
    outputPath: relPath(destinationPath),
    sizeBytes: stat.size,
    sha256: await sha256File(destinationPath)
  };
}

async function writeManifest(artifacts) {
  const manifestPath = path.join(rootDir, "dist", "bin", platformName, "manifest.json");
  await writeJson(manifestPath, {
    generatedAt: new Date().toISOString(),
    platform: platformName,
    targets: archTargets.map((target) => target.arch),
    artifacts
  });
  return manifestPath;
}

function resolveGoCommand() {
  const candidates = ["go", "/usr/local/go/bin/go", "/opt/homebrew/bin/go"];

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

function goEnv() {
  const root = path.join(rootDir, ".cache");
  const goCache = path.join(root, "go-build");
  const goModCache = path.join(root, "go-mod");
  const goTmp = path.join(root, "go-tmp");

  fsSync.mkdirSync(goCache, { recursive: true });
  fsSync.mkdirSync(goModCache, { recursive: true });
  fsSync.mkdirSync(goTmp, { recursive: true });

  return {
    ...process.env,
    GOMODCACHE: goModCache,
    GOCACHE: goCache,
    GOTMPDIR: goTmp
  };
}
