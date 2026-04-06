import { spawnSync } from "node:child_process";
import fs from "node:fs/promises";
import path from "node:path";

const filesToCopy = [
  ["src/renderer/index.html", "dist/renderer/index.html"],
  ["src/renderer/styles.css", "dist/renderer/styles.css"],
  ["src/renderer/bg.js", "dist/renderer/bg.js"]
];

for (const [source, destination] of filesToCopy) {
  const targetDir = path.dirname(destination);
  await fs.mkdir(targetDir, { recursive: true });
  await fs.copyFile(source, destination);
}

await ensureMacIconFromIco();

async function ensureMacIconFromIco() {
  if (process.platform !== "darwin") {
    return;
  }

  const icoPath = await resolveIcoPath();
  const builtDir = path.resolve("built");
  const buildDir = path.resolve("build");
  const iconsetDir = path.join(builtDir, "pangeavpn.iconset");
  const icnsPath = path.join(builtDir, "pangeavpn.icns");
  const buildIcnsPath = path.join(buildDir, "PangeaVPN.icns");
  const basePngPath = path.join(builtDir, "pangeavpn.base.png");

  await fs.mkdir(builtDir, { recursive: true });
  await fs.mkdir(buildDir, { recursive: true });
  await fs.rm(iconsetDir, { recursive: true, force: true });
  await fs.mkdir(iconsetDir, { recursive: true });
  runTool("sips", ["-s", "format", "png", icoPath, "--out", basePngPath]);

  const specs = [
    ["icon_16x16.png", 16],
    ["icon_16x16@2x.png", 32],
    ["icon_32x32.png", 32],
    ["icon_32x32@2x.png", 64],
    ["icon_128x128.png", 128],
    ["icon_128x128@2x.png", 256],
    ["icon_256x256.png", 256],
    ["icon_256x256@2x.png", 512],
    ["icon_512x512.png", 512],
    ["icon_512x512@2x.png", 1024]
  ];

  for (const [fileName, size] of specs) {
    const outPath = path.join(iconsetDir, fileName);
    runTool("sips", ["-z", String(size), String(size), basePngPath, "--out", outPath]);
  }

  runTool("iconutil", ["-c", "icns", iconsetDir, "-o", icnsPath]);
  await fs.copyFile(icnsPath, buildIcnsPath);
  await fs.rm(iconsetDir, { recursive: true, force: true });
  await fs.rm(basePngPath, { force: true });
}

async function resolveIcoPath() {
  const candidates = [
    path.resolve("built", "pangeavpn.ico"),
    path.resolve("build", "PangeaVPN.ico")
  ];

  for (const candidate of candidates) {
    try {
      await fs.access(candidate);
      return candidate;
    } catch {
      // continue
    }
  }

  throw new Error("Icon source not found. Expected built/pangeavpn.ico or build/PangeaVPN.ico.");
}

function runTool(command, args) {
  const result = spawnSync(command, args, {
    stdio: "pipe",
    shell: false
  });

  if (!result.error && result.status === 0) {
    return;
  }

  const stderr = result.stderr ? result.stderr.toString().trim() : "";
  const stdout = result.stdout ? result.stdout.toString().trim() : "";
  const details = stderr || stdout || result.error?.message || "unknown error";
  throw new Error(`Failed running ${command} ${args.join(" ")}: ${details}`);
}
