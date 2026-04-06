import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const desktopDir = path.resolve(scriptDir, "..");
const rootDir = path.resolve(desktopDir, "..", "..");
const outputDir = path.resolve(desktopDir, "../../dist/installers");

const src = path.join(rootDir, "scripts", "install-mac.sh");
const dest = path.join(outputDir, "install-mac.sh");

if (!fs.existsSync(src)) {
  console.log("install-mac.sh not found, skipping.");
  process.exit(0);
}

if (!fs.existsSync(outputDir)) {
  console.log(`Output directory ${outputDir} does not exist, skipping.`);
  process.exit(0);
}

fs.copyFileSync(src, dest);
try {
  fs.chmodSync(dest, 0o755);
} catch {
  // chmod may fail on Windows, which is fine
}

console.log(`Staged install-mac.sh -> ${dest}`);
