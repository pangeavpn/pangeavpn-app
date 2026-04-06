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
}

console.log("Packaging resource check passed.");

function assertFile(filePath, errorMessage) {
  if (!fs.existsSync(filePath)) {
    throw new Error(errorMessage);
  }
}

function ensureExecutable(filePath, label) {
  const stat = fs.statSync(filePath);
  if ((stat.mode & 0o111) === 0) {
    fs.chmodSync(filePath, 0o755);
    console.log(`Set executable mode on ${label}: ${filePath}`);
  }
}
