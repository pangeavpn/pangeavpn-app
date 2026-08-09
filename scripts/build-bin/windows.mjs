import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { npmCmd, relPath, rootDir, runOrThrow, selectArchTargets, sha256File, writeJson } from "./shared.mjs";

const platformName = "windows";
const appBuilderPath = path.join(rootDir, "node_modules", "app-builder-bin", "win", "x64", "app-builder.exe");
const allArchTargets = [
  { arch: "x64", goArch: "amd64" },
  { arch: "arm64", goArch: "arm64" }
];

if (process.platform !== "win32") {
  console.error("build-bin:windows must run on a Windows host.");
  process.exit(1);
}

const archTargets = selectArchTargets(allArchTargets, "Windows");

await cleanOutput();
runOrThrow(npmCmd, ["install", "--workspace", "@pangeavpn/desktop", "--include=dev"], { cwd: rootDir, shell: true });
runOrThrow(npmCmd, ["run", "build", "--workspace", "@pangeavpn/shared-types"], { cwd: rootDir, shell: true });
runOrThrow(npmCmd, ["run", "build", "--workspace", "@pangeavpn/desktop"], { cwd: rootDir, shell: true });

const artifacts = [];
for (const target of archTargets) {
  console.log(`\n--- Windows ${target.arch} ---`);

  runOrThrow("node", ["./scripts/build-daemon.mjs"], {
    cwd: rootDir,
    env: {
      ...process.env,
      GOOS: "windows",
      GOARCH: target.goArch
    }
  });

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
      "--win",
      "nsis",
      `--${target.arch}`,
      "--publish",
      "never",
      "--config.electronVersion=34.1.0"
    ],
    {
      cwd: rootDir,
      shell: true,
      env: {
        ...process.env,
        CUSTOM_APP_BUILDER_PATH: appBuilderPath,
        // Nsis7z is 7-Zip 19.00 and silently drops ARM64-filtered entries.
        ELECTRON_BUILDER_7Z_FILTER: "BCJ2"
      }
    }
  );

  const archArtifacts = await collectArchArtifacts(target.arch, target.goArch, packagingStartedAtMs);
  artifacts.push(...archArtifacts);
}

const manifestPath = await writeManifest(artifacts);
console.log(`build-bin:windows completed. Manifest: ${manifestPath}`);

async function cleanOutput() {
  const windowsOutputRoot = path.join(rootDir, "dist", "bin", platformName);
  const installersDir = path.join(rootDir, "dist", "installers");
  await fs.rm(windowsOutputRoot, { recursive: true, force: true });
  await fs.rm(installersDir, { recursive: true, force: true });
  for (const target of archTargets) {
    await fs.mkdir(path.join(windowsOutputRoot, "installer", target.arch), { recursive: true });
  }
  await fs.mkdir(path.join(windowsOutputRoot, "daemon"), { recursive: true });
}

async function collectArchArtifacts(arch, goArch, packagingStartedAtMs) {
  const installersDir = path.join(rootDir, "dist", "installers");
  const windowsBinDir = path.join(rootDir, "dist", "bin", platformName);
  const installerOut = path.join(windowsBinDir, "installer", arch);
  const daemonOut = path.join(windowsBinDir, "daemon");
  const daemonSource = path.join(rootDir, "daemon", "bin", "PangeaDaemon.exe");
  const wireGuardDllSource = path.join(rootDir, "daemon", "bin", "wireguard.dll");
  const wintunDllSource = path.join(rootDir, "daemon", "bin", "wintun.dll");

  const installerEntries = await fs.readdir(installersDir, { withFileTypes: true });
  const exes = [];
  for (const entry of installerEntries) {
    if (!entry.isFile() || !entry.name.toLowerCase().endsWith(".exe")) {
      continue;
    }
    const fullPath = path.join(installersDir, entry.name);
    const stat = await fs.stat(fullPath);
    exes.push({
      name: entry.name,
      fullPath,
      mtimeMs: stat.mtimeMs
    });
  }

  // nsis artifactName carries the arch (PangeaVPN-Setup-<version>-<arch>.exe),
  // so match on that rather than guessing from mtime.
  const currentRunExes = exes.filter((entry) => entry.mtimeMs >= packagingStartedAtMs - 1000);
  const installerCandidates = (currentRunExes.length > 0 ? currentRunExes : exes).filter((entry) => {
    const name = entry.name.toLowerCase();
    return name.includes("setup") && name.includes(`-${arch.toLowerCase()}.`);
  });
  if (installerCandidates.length === 0) {
    throw new Error(`NSIS installer for ${arch} not found in ${installersDir}`);
  }

  const installerFile = newestByMtime(installerCandidates);

  const copied = [];
  copied.push(await copyArtifact(installerFile.fullPath, path.join(installerOut, installerFile.name), "installer", arch));
  copied.push(await copyArtifact(daemonSource, path.join(daemonOut, `PangeaDaemon-${arch}.exe`), "daemon", arch, goArch));
  copied.push(await copyArtifact(wireGuardDllSource, path.join(daemonOut, `wireguard-${arch}.dll`), "daemon-runtime", arch));
  copied.push(await copyArtifact(wintunDllSource, path.join(daemonOut, `wintun-${arch}.dll`), "daemon-runtime", arch));
  return copied;
}

function newestByMtime(entries) {
  return [...entries].sort((a, b) => b.mtimeMs - a.mtimeMs)[0];
}

async function copyArtifact(sourcePath, destinationPath, type, arch, goArch = null) {
  await fs.copyFile(sourcePath, destinationPath);
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
