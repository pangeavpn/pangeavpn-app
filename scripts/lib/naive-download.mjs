import { spawnSync } from "node:child_process";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

// Pinned prebuilt pangea_naive engine (lib + header) from the fork's public
// release, so the daemon build skips a local Chromium build. Bump on re-release.
const RELEASE_REPO = "pangeavpn/naiveproxy";
const RELEASE_TAG = "pangea-naive-v150.0.7871.63-1";

// goArch ("amd64" | "arm64") -> the CI matrix arch in the release asset name.
const ASSET_ARCH = { amd64: "x64", arm64: "arm64" };

// sha256 of pangea-naive-<arch>.zip; empty entry falls back to HTTPS trust.
const ASSET_SHA256 = {
  // x64: "...",
  // arm64: "...",
};

// ensurePangeaNaiveLib returns { libDir, headerDir } for the pinned prebuilt,
// downloading + caching under .cache on first use, or null if it can't fetch.
export function ensurePangeaNaiveLib(goArch, rootDir) {
  const assetArch = ASSET_ARCH[goArch];
  if (!assetArch) {
    return null;
  }

  const cacheDir = path.join(rootDir, ".cache", "pangea-naive", RELEASE_TAG, assetArch);
  const libPath = path.join(cacheDir, "pangea_naive.lib");
  const headerPath = path.join(cacheDir, "pangea_naive_capi.h");
  if (fs.existsSync(libPath) && fs.existsSync(headerPath)) {
    return { libDir: cacheDir, headerDir: cacheDir };
  }

  const asset = `pangea-naive-${assetArch}.zip`;
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

  const expectedSha = ASSET_SHA256[assetArch];
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

  // tar (bsdtar) extracts the .zip; it holds a top pangea-naive-<arch>/ dir.
  const untar = spawnSync("tar", ["-xf", zipPath, "-C", cacheDir], { stdio: "inherit", shell: false });
  if (untar.error || (untar.status ?? 1) !== 0) {
    console.warn(`naive_cgo: failed to extract ${asset}; naive transport falls back to the stub for ${goArch}.`);
    return null;
  }

  const extractedDir = path.join(cacheDir, `pangea-naive-${assetArch}`);
  for (const name of ["pangea_naive.lib", "pangea_naive_capi.h"]) {
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
  return { libDir: cacheDir, headerDir: cacheDir };
}

function sha256File(filePath) {
  const hash = crypto.createHash("sha256");
  hash.update(fs.readFileSync(filePath));
  return hash.digest("hex");
}
