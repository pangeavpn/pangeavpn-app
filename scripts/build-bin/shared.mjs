import crypto from "node:crypto";
import { spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";

export const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..");
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

  if (result.status !== 0) {
    const detail = result.signal ? `signal ${result.signal}` : `code ${result.status ?? "unknown"}`;
    throw new Error(`${command} ${args.join(" ")} exited with ${detail}`);
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

// All arches by default so releases keep shipping every one. `--arch x64` (or
// PANGEA_BUILD_ARCHES=x64) narrows a local build to one Go build + one pack.
export function selectArchTargets(allTargets, platformLabel) {
  const flagIndex = process.argv.indexOf("--arch");
  const raw = flagIndex !== -1 ? process.argv[flagIndex + 1] : process.env.PANGEA_BUILD_ARCHES;
  if (!raw) {
    return allTargets;
  }

  const known = allTargets.map((target) => target.arch);
  const wanted = raw.split(",").map((value) => value.trim().toLowerCase()).filter(Boolean);
  const unknown = wanted.filter((value) => !known.includes(value));
  if (unknown.length > 0) {
    console.error(`unknown arch(es): ${unknown.join(", ")}. Known: ${known.join(", ")}`);
    process.exit(1);
  }

  const selected = allTargets.filter((target) => wanted.includes(target.arch));
  console.log(`building ${platformLabel} arch(es): ${selected.map((target) => target.arch).join(", ")}`);
  return selected;
}
