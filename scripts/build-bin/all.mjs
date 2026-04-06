import { spawnSync } from "node:child_process";
import path from "node:path";
import process from "node:process";
import { rootDir, writeJson } from "./shared.mjs";

const targets = [
  {
    name: "windows",
    script: path.join(rootDir, "scripts", "build-bin", "windows.mjs"),
    outputDir: path.join(rootDir, "dist", "bin", "windows"),
    manifestPath: path.join(rootDir, "dist", "bin", "windows", "manifest.json")
  },
  {
    name: "mac",
    script: path.join(rootDir, "scripts", "build-bin", "mac.mjs"),
    outputDir: path.join(rootDir, "dist", "bin", "mac"),
    manifestPath: path.join(rootDir, "dist", "bin", "mac", "manifest.json")
  }
];

const results = [];
for (const target of targets) {
  console.log(`\n=== build-bin:${target.name} ===`);
  const startedAt = Date.now();
  const result = spawnSync("node", [target.script], {
    cwd: rootDir,
    stdio: "inherit",
    shell: false
  });
  const durationMs = Date.now() - startedAt;

  let status = "success";
  let error = "";
  if (result.error) {
    status = "failed";
    error = result.error.message;
  } else if ((result.status ?? 1) !== 0) {
    status = "failed";
    error = `exit code ${result.status ?? 1}`;
  }

  results.push({
    target: target.name,
    status,
    durationMs,
    outputDir: toPosixRel(target.outputDir),
    manifestPath: toPosixRel(target.manifestPath),
    error
  });
}

const allManifestPath = path.join(rootDir, "dist", "bin", "manifest-all.json");
await writeJson(allManifestPath, {
  generatedAt: new Date().toISOString(),
  results
});

const failed = results.filter((entry) => entry.status !== "success");
if (failed.length > 0) {
  console.error("\nbuild-bin failed.");
  for (const entry of failed) {
    console.error(`- ${entry.target}: ${entry.error}`);
  }
  console.error(`Summary manifest: ${allManifestPath}`);
  process.exit(1);
}

console.log(`build-bin completed. Summary manifest: ${allManifestPath}`);

function toPosixRel(filePath) {
  return path.relative(rootDir, filePath).replaceAll("\\", "/");
}
