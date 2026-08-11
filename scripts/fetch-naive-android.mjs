import fs from "node:fs";
import path from "node:path";
import process from "node:process";
import { fileURLToPath } from "node:url";
import { ANDROID_ABI, ensurePangeaNaiveAndroidLib } from "./lib/naive-download.mjs";

// Stages the prebuilt Chromium naive engine where the cgo directives in
// daemon/internal/naive/cgo_android.go expect it, before `gomobile bind`.

const rootDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const destDir = path.join(rootDir, "daemon", "internal", "naive", "android", ANDROID_ABI);

const required = String(process.env.PANGEA_REQUIRE_NAIVE ?? "").trim().toLowerCase();
const isRequired = required !== "" && required !== "0" && required !== "false";

const fetched = ensurePangeaNaiveAndroidLib(rootDir);
if (!fetched) {
  const message = `naive_cgo: no Android engine for ${ANDROID_ABI}; the AAR builds without the naive transport.`;
  if (isRequired) {
    console.error(message.replace("the AAR builds without", "refusing to build without"));
    process.exit(1);
  }
  console.warn(message);
  process.exit(0);
}

fs.mkdirSync(destDir, { recursive: true });
for (const name of [fetched.libName, "pangea_naive_capi.h"]) {
  fs.copyFileSync(path.join(fetched.libDir, name), path.join(destDir, name));
}
console.log(`naive_cgo: staged ${fetched.libName} for ${ANDROID_ABI} in ${destDir}`);
