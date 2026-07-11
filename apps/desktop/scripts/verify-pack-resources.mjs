import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const desktopDir = path.resolve(scriptDir, "..");
const rootDir = path.resolve(desktopDir, "..", "..");

const isWin = process.platform === "win32";
const daemonName = isWin ? "PangeaDaemon.exe" : "daemon";
const daemonPath = path.join(rootDir, "daemon", "bin", daemonName);

assertFile(daemonPath, `Daemon binary missing at ${daemonPath}. Run the daemon build first.`);
if (!isWin) {
  ensureExecutable(daemonPath, "daemon binary");
}

if (process.platform === "win32") {
  const winBinDir = path.join(desktopDir, "resources", "bin", "win");
  assertFile(path.join(winBinDir, "wintun.dll"), `Missing Windows bundled file at ${path.join(winBinDir, "wintun.dll")}.`);
  assertFile(
    path.join(rootDir, "daemon", "bin", "wireguard.dll"),
    `Missing Windows daemon dependency at ${path.join(rootDir, "daemon", "bin", "wireguard.dll")}.`
  );
  assertFile(
    path.join(rootDir, "daemon", "bin", "wintun.dll"),
    `Missing Windows daemon dependency at ${path.join(rootDir, "daemon", "bin", "wintun.dll")}.`
  );

  // Branded NSIS wizard artwork — must be present at the exact sizes MUI needs.
  const buildDir = path.join(desktopDir, "build");
  assertBmp(path.join(buildDir, "installerSidebar.bmp"), 164, 314);
  assertBmp(path.join(buildDir, "installerHeader.bmp"), 150, 57);
}

console.log("Packaging resource check passed.");

function assertFile(filePath, errorMessage) {
  if (!fs.existsSync(filePath)) {
    throw new Error(errorMessage);
  }
}

// Validate a BMP by parsing its header (BITMAPINFOHEADER): "BM" magic at 0,
// biWidth int32 LE at offset 18, biHeight int32 LE at offset 22. Avoids any
// image-library dependency. Run `node ./scripts/build-installer-art.mjs` to
// (re)generate these from build/art-src/*.png.
function assertBmp(filePath, expectedWidth, expectedHeight) {
  assertFile(
    filePath,
    `Missing installer artwork at ${filePath}. Run: node ./scripts/build-installer-art.mjs`
  );
  const buf = fs.readFileSync(filePath);
  if (buf.length < 26 || buf[0] !== 0x42 || buf[1] !== 0x4d) {
    throw new Error(`Installer artwork is not a valid BMP: ${filePath}`);
  }
  const width = buf.readInt32LE(18);
  const height = buf.readInt32LE(22);
  if (width !== expectedWidth || height !== expectedHeight) {
    throw new Error(
      `Installer artwork ${filePath} is ${width}x${height}, expected ${expectedWidth}x${expectedHeight}.`
    );
  }
}

function ensureExecutable(filePath, label) {
  const stat = fs.statSync(filePath);
  if ((stat.mode & 0o111) === 0) {
    fs.chmodSync(filePath, 0o755);
    console.log(`Set executable mode on ${label}: ${filePath}`);
  }
}
