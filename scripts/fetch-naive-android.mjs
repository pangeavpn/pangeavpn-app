import { spawnSync } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { ANDROID_ABI, ensurePangeaNaiveAndroidLib, extractZip } from "./lib/naive-download.mjs";

// Stages the prebuilt Chromium naive engine where the cgo directives in
// daemon/internal/naive/cgo_android.go expect it, before `gomobile bind`.

const NAIVE_REPO = "pangeavpn/naiveproxy";
const ASSET_STEM = `pangea-naive-android-${ANDROID_ABI}`;

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const destDir = path.join(rootDir, "daemon", "internal", "naive", "android", ANDROID_ABI);

const required = String(process.env.PANGEA_REQUIRE_NAIVE ?? "").trim().toLowerCase();
const isRequired = required !== "" && required !== "0" && required !== "false";

const staged = stageFromRun() ?? stageFromRelease();
if (!staged) {
  const message = `naive_cgo: no Android engine for ${ANDROID_ABI}; the AAR builds without the naive transport.`;
  if (isRequired) {
    console.error(message.replace("the AAR builds without", "refusing to build without"));
    process.exit(1);
  }
  console.warn(message);
  process.exit(0);
}
console.log(`naive_cgo: staged libpangea_naive.a for ${ANDROID_ABI} in ${destDir}`);

// PANGEA_NAIVE_ANDROID_RUN_ID takes the engine straight from a fork CI run,
// so a linked build can be proven before anything is released.
function stageFromRun() {
  const runId = String(process.env.PANGEA_NAIVE_ANDROID_RUN_ID ?? "").trim();
  if (!runId) {
    return null;
  }

  const workDir = fs.mkdtempSync(path.join(os.tmpdir(), "pangea-naive-run-"));
  const gh = spawnSync(
    "gh",
    ["run", "download", runId, "-R", NAIVE_REPO, "-n", `${ASSET_STEM}.zip`, "-D", workDir],
    { stdio: "inherit", shell: false }
  );
  if (gh.error || (gh.status ?? 1) !== 0) {
    console.warn(`naive_cgo: could not download ${ASSET_STEM}.zip from run ${runId}.`);
    return null;
  }

  // The artifact wraps the same zip the release carries.
  if (!extractZip(path.join(workDir, `${ASSET_STEM}.zip`), workDir)) {
    console.warn(`naive_cgo: could not extract ${ASSET_STEM}.zip from run ${runId}.`);
    return null;
  }
  return copyInto(path.join(workDir, ASSET_STEM));
}

function stageFromRelease() {
  const fetched = ensurePangeaNaiveAndroidLib(rootDir);
  return fetched ? copyInto(fetched.libDir) : null;
}

function copyInto(sourceDir) {
  const names = ["libpangea_naive.a", "pangea_naive_capi.h"];
  if (!names.every((name) => fs.existsSync(path.join(sourceDir, name)))) {
    console.warn(`naive_cgo: ${sourceDir} is missing the lib or the header.`);
    return null;
  }
  fs.mkdirSync(destDir, { recursive: true });
  for (const name of names) {
    fs.copyFileSync(path.join(sourceDir, name), path.join(destDir, name));
  }
  return destDir;
}
