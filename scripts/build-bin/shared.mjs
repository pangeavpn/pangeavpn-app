import crypto from "node:crypto";
import { spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";

export const rootDir = process.cwd();
export const isWin = process.platform === "win32";
export const npmCmd = isWin ? "npm.cmd" : "npm";

export function runOrThrow(command, args, options = {}) {
  const result = spawnSync(command, args, {
    stdio: "inherit",
    shell: false,
    ...options
  });

  if (result.error) {
    throw new Error(`${command} ${args.join(" ")} failed: ${result.error.message}`);
  }

  if (typeof result.status === "number" && result.status !== 0) {
    throw new Error(`${command} ${args.join(" ")} exited with code ${result.status}`);
  }
}

export async function writeJson(filePath, payload) {
  await fs.mkdir(path.dirname(filePath), { recursive: true });
  await fs.writeFile(filePath, `${JSON.stringify(payload, null, 2)}\n`, "utf8");
  return filePath;
}

export async function sha256File(filePath) {
  const content = await fs.readFile(filePath);
  const hash = crypto.createHash("sha256");
  hash.update(content);
  return hash.digest("hex");
}

export function relPath(filePath) {
  return path.relative(rootDir, filePath).replaceAll("\\", "/");
}
