import { spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

// Pinned prebuilt pangea_naive engine (lib + header) from the fork's public
// release, so the daemon build skips a local Chromium build. Bump on re-release.
const RELEASE_REPO = "pangeavpn/naiveproxy";
const RELEASE_TAG = "pangea-naive-v150.0.7871.63-3";

// goArch ("amd64" | "arm64") -> the CI matrix arch in the release asset name.
const ASSET_ARCH = { amd64: "x64", arm64: "arm64" };

// Per-OS asset naming: Windows zips hold a COFF pangea_naive.lib, mac zips a
// Mach-O libpangea_naive.a.
const OS_ASSET = {
  win32: { prefix: "pangea-naive-", libName: "pangea_naive.lib" },
  darwin: { prefix: "pangea-naive-mac-", libName: "libpangea_naive.a" }
};

// sha256 of <prefix><arch>.zip; empty entry falls back to HTTPS trust.
const ASSET_SHA256 = {
  win32: {
    x64: "cc932c0d19cb95fa6deb85765d087ecf89db3821bf34c9af1f0b6df8bd525591",
    arm64: "8fc336d4e845c58d7ed9e5cfe500fadb936cde2e329d593e1762d6aebd52ab2f"
  },
  darwin: {
    x64: "634b4f1d4efbf64a4c5b214bfe2dc5db44a5dc8ab21f52c69a7cb3bc7c2af27f",
    arm64: "d9975a124dfc60b627060817ee3e70bcd7a8e0fd282e42ebf6a988713aef0c2a"
  }
};

// ensurePangeaNaiveLib returns { libDir, headerDir, libName } for the pinned
// prebuilt, downloading + caching under .cache on first use, or null if it
// can't fetch.
export function ensurePangeaNaiveLib(goArch, rootDir) {
  const assetArch = ASSET_ARCH[goArch];
  const osAsset = OS_ASSET[process.platform];
  if (!assetArch || !osAsset) {
    return null;
  }

  const assetStem = `${osAsset.prefix}${assetArch}`;
  const cacheDir = path.join(rootDir, ".cache", "pangea-naive", RELEASE_TAG, assetStem);
  const libPath = path.join(cacheDir, osAsset.libName);
  const headerPath = path.join(cacheDir, "pangea_naive_capi.h");
  if (fs.existsSync(libPath) && fs.existsSync(headerPath)) {
    return { libDir: cacheDir, headerDir: cacheDir, libName: osAsset.libName };
  }

  const asset = `${assetStem}.zip`;
  const url = `https://github.com/${RELEASE_REPO}/releases/download/${RELEASE_TAG}/${asset}`;
  fs.mkdirSync(cacheDir, { recursive: true });
  const zipPath = path.join(cacheDir, asset);

  console.log(`naive_cgo: downloading prebuilt engine ${asset} (${RELEASE_TAG})`);
  const curl = spawnSync("curl", ["-fSL", "--retry", "3", "-o", zipPath, url], {
    stdio: "inherit",
    shell: false
  });
  if (curl.error || (curl.status ?? 1) !== 0) {
    console.warn(`naive_cgo: failed to download ${url}; naive transport falls back to the stub for ${goArch}.`);
    return null;
  }

  const expectedSha = (ASSET_SHA256[process.platform] ?? {})[assetArch];
  if (expectedSha) {
    const actual = sha256File(zipPath);
    if (actual !== expectedSha) {
      console.warn(`naive_cgo: sha256 mismatch for ${asset} (want ${expectedSha}, got ${actual}); refusing it.`);
      fs.rmSync(zipPath, { force: true });
      return null;
    }
  } else {
    console.warn(`naive_cgo: no sha256 pin configured for ${asset}; trusting HTTPS only.`);
  }

  // The .zip holds a top <assetStem>/ dir.
  if (!extractZip(zipPath, cacheDir)) {
    console.warn(`naive_cgo: failed to extract ${asset}; naive transport falls back to the stub for ${goArch}.`);
    return null;
  }

  const extractedDir = path.join(cacheDir, assetStem);
  for (const name of [osAsset.libName, "pangea_naive_capi.h"]) {
    const src = path.join(extractedDir, name);
    const dst = path.join(cacheDir, name);
    if (fs.existsSync(src)) {
      fs.renameSync(src, dst);
    }
  }
  fs.rmSync(extractedDir, { recursive: true, force: true });
  fs.rmSync(zipPath, { force: true });

  if (!fs.existsSync(libPath) || !fs.existsSync(headerPath)) {
    console.warn(`naive_cgo: ${asset} did not contain the expected lib/header; falling back to the stub for ${goArch}.`);
    return null;
  }
  return { libDir: cacheDir, headerDir: cacheDir, libName: osAsset.libName };
}

function sha256File(filePath) {
  const hash = crypto.createHash("sha256");
  hash.update(fs.readFileSync(filePath));
  return hash.digest("hex");
}

// Extracts a .zip into destDir. Prefers tar (bsdtar handles zip; the default
// tar on macOS and Windows), falls back to PowerShell Expand-Archive on
// Windows — GNU tar (some Git-for-Windows shells) can't read zips. Returns
// true on success.
function extractZip(zipPath, destDir) {
  const bsd = spawnSync("tar", ["-xf", zipPath, "-C", destDir], { stdio: "inherit", shell: false });
  if (!bsd.error && (bsd.status ?? 1) === 0) {
    return true;
  }
  if (process.platform !== "win32") {
    return false;
  }
  const ps = spawnSync(
    "powershell",
    [
      "-NoProfile",
      "-NonInteractive",
      "-Command",
      `Expand-Archive -Path '${zipPath}' -DestinationPath '${destDir}' -Force`
    ],
    { stdio: "inherit", shell: false }
  );
  return !ps.error && (ps.status ?? 1) === 0;
}
