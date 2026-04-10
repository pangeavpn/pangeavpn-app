import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { npmCmd, relPath, rootDir, runOrThrow, sha256File, writeJson } from "./shared.mjs";

const platformName = "linux";
const archTargets = [
  { arch: "x64", goArch: "amd64" },
  { arch: "arm64", goArch: "arm64" }
];

if (process.platform !== "linux") {
  console.error("build-bin:linux must run on a Linux host.");
  process.exit(1);
}

await cleanOutput();
runOrThrow(npmCmd, ["install", "--workspace", "@pangeavpn/desktop", "--include=dev"], { cwd: rootDir, shell: true });
runOrThrow(npmCmd, ["run", "build", "--workspace", "@pangeavpn/shared-types"], { cwd: rootDir, shell: true });
runOrThrow(npmCmd, ["run", "build", "--workspace", "@pangeavpn/desktop"], { cwd: rootDir, shell: true });

const artifacts = [];
for (const target of archTargets) {
  console.log(`\n--- Linux ${target.arch} ---`);

  runOrThrow("node", ["./scripts/build-daemon.mjs"], {
    cwd: rootDir,
    env: {
      ...process.env,
      GOOS: "linux",
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
      "--linux",
      "AppImage",
      "deb",
      `--${target.arch}`,
      "--publish",
      "never",
      "--config.electronVersion=34.1.0"
    ],
    { cwd: rootDir, shell: true }
  );

  const archArtifacts = await collectArchArtifacts(target.arch, target.goArch, packagingStartedAtMs);
  artifacts.push(...archArtifacts);
}

const manifestPath = await writeManifest(artifacts);
console.log(`build-bin:linux completed. Manifest: ${manifestPath}`);

async function cleanOutput() {
  const linuxOutputRoot = path.join(rootDir, "dist", "bin", platformName);
  const installersDir = path.join(rootDir, "dist", "installers");
  await fs.rm(linuxOutputRoot, { recursive: true, force: true });
  await fs.rm(installersDir, { recursive: true, force: true });
  for (const target of archTargets) {
    await fs.mkdir(path.join(linuxOutputRoot, "appimage", target.arch), { recursive: true });
    await fs.mkdir(path.join(linuxOutputRoot, "deb", target.arch), { recursive: true });
  }
  await fs.mkdir(path.join(linuxOutputRoot, "daemon"), { recursive: true });
}

async function collectArchArtifacts(arch, goArch, packagingStartedAtMs) {
  const installersDir = path.join(rootDir, "dist", "installers");
  const linuxBinDir = path.join(rootDir, "dist", "bin", platformName);
  const appImageOut = path.join(linuxBinDir, "appimage", arch);
  const debOut = path.join(linuxBinDir, "deb", arch);
  const daemonOut = path.join(linuxBinDir, "daemon");
  const daemonSource = path.join(rootDir, "daemon", "bin", "daemon");

  const entries = await fs.readdir(installersDir, { withFileTypes: true });
  const files = [];
  for (const entry of entries) {
    if (!entry.isFile()) continue;
    const fullPath = path.join(installersDir, entry.name);
    const stat = await fs.stat(fullPath);
    files.push({ name: entry.name, fullPath, mtimeMs: stat.mtimeMs });
  }

  const currentRunFiles = files.filter((f) => f.mtimeMs >= packagingStartedAtMs - 1000);
  const selected = currentRunFiles.length >= 2 ? currentRunFiles : files;

  const appImageCandidates = selected.filter((f) => f.name.toLowerCase().endsWith(".appimage"));
  const debCandidates = selected.filter((f) => f.name.toLowerCase().endsWith(".deb"));

  if (appImageCandidates.length === 0) {
    throw new Error(`AppImage not found for ${arch} in dist/installers`);
  }
  if (debCandidates.length === 0) {
    throw new Error(`.deb package not found for ${arch} in dist/installers`);
  }

  const appImageFile = newestByMtime(appImageCandidates);
  const debFile = newestByMtime(debCandidates);

  const copied = [];
  copied.push(await copyArtifact(appImageFile.fullPath, path.join(appImageOut, appImageFile.name), "appimage", arch));
  copied.push(await copyArtifact(debFile.fullPath, path.join(debOut, debFile.name), "deb", arch));
  copied.push(await copyArtifact(daemonSource, path.join(daemonOut, `daemon-${arch}`), "daemon", arch, goArch));
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
    targets: archTargets.map((t) => t.arch),
    artifacts
  });
  return manifestPath;
}
