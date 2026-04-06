import { spawnSync } from "node:child_process";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

const npmCmd = "npm";
const isWin = process.platform === "win32";
const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const packageDir = path.resolve(scriptDir, "..");

runOrExit(npmCmd, ["run", "build"], {
  shell: isWin,
  cwd: packageDir
});

const env = { ...process.env };
delete env.ELECTRON_RUN_AS_NODE;

runOrExit(resolveElectronBinary(), ["."], {
  shell: false,
  env,
  cwd: packageDir
});

function runOrExit(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    shell: false,
    ...options
  });

  if (result.error) {
    console.error(result.error.message);
    process.exit(1);
  }

  if (result.status !== 0) {
    console.error(`Command failed: ${command} ${args.join(" ")}`);
    process.exit(result.status ?? 1);
  }
}

function resolveElectronBinary() {
  const distDir = path.resolve(packageDir, "..", "..", "node_modules", "electron", "dist");
  if (process.platform === "win32") {
    return path.join(distDir, "electron.exe");
  }
  if (process.platform === "darwin") {
    return path.join(distDir, "Electron.app", "Contents", "MacOS", "Electron");
  }
  return path.join(distDir, "electron");
}
