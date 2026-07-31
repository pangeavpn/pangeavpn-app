// Fails when an installer holds codecs its own Nsis7z (7-Zip 19.00) cannot
// decode. Usage: node scripts/verify-windows-installer.mjs [installer.exe ...]

import { spawnSync } from "node:child_process";
import fs from "node:fs";
import path from "node:path";
import process from "node:process";

// Filters absent from 7-Zip 19.00, so unreadable by the bundled Nsis7z plugin.
const UNSUPPORTED_CODECS = ["ARM64", "RISCV"];

// Payload members that must survive extraction for the install to be usable.
const REQUIRED_ENTRIES = [
  "PangeaVPN.exe",
  "resources/app.asar",
  "resources/daemon/PangeaDaemon.exe",
  "resources/daemon/wireguard.dll",
  "resources/daemon/wintun.dll"
];

const rootDir = path.resolve(import.meta.dirname, "..");

const sevenZip = resolveSevenZip();
if (!sevenZip) {
  console.error("7-Zip not found. Install it or put 7z on PATH.");
  process.exit(1);
}

const installers = process.argv.slice(2).length > 0 ? process.argv.slice(2) : discoverInstallers();
if (installers.length === 0) {
  console.error("No installers found under dist/bin/windows/installer.");
  process.exit(1);
}

let failed = false;
for (const installer of installers) {
  if (!verifyInstaller(installer)) {
    failed = true;
  }
}
process.exit(failed ? 1 : 0);

function verifyInstaller(installerPath) {
  const label = path.relative(rootDir, installerPath);
  if (!fs.existsSync(installerPath)) {
    console.error(`FAIL ${label}: file not found`);
    return false;
  }

  const payload = findPayload(installerPath);
  if (!payload) {
    console.error(`FAIL ${label}: no app payload found inside the installer`);
    return false;
  }

  // A zip payload goes through nsisunz, which has no branch filters at all and
  // reports failure to the user, so only 7z payloads need the codec audit.
  if (payload.endsWith(".zip")) {
    console.log(`OK   ${label}: zip payload (${payload}), no filter mismatch possible`);
    return true;
  }

  const entries = listPayloadEntries(installerPath, payload);
  if (entries.length === 0) {
    console.error(`FAIL ${label}: could not read ${payload}`);
    return false;
  }

  const problems = [];

  const undecodable = entries.filter((entry) =>
    UNSUPPORTED_CODECS.some((codec) => entry.method.split(/\s+/).includes(codec))
  );
  if (undecodable.length > 0) {
    problems.push(
      `${undecodable.length} entries use a codec Nsis7z (7-Zip 19.00) cannot decode, ` +
        `so they are silently dropped during install:\n` +
        undecodable.map((entry) => `       - ${entry.path} [${entry.method}]`).join("\n")
    );
  }

  const present = new Set(entries.map((entry) => entry.path.replace(/\\/g, "/")));
  const missing = REQUIRED_ENTRIES.filter((required) => !present.has(required));
  if (missing.length > 0) {
    problems.push(`missing required entries:\n${missing.map((m) => `       - ${m}`).join("\n")}`);
  }

  if (problems.length > 0) {
    console.error(`FAIL ${label}: ${problems.join("\n     ")}`);
    return false;
  }

  console.log(`OK   ${label}: ${entries.length} entries, all decodable by Nsis7z`);
  return true;
}

// Lists the payload's members with the codec chain 7-Zip recorded per entry.
function listPayloadEntries(installerPath, payloadName) {
  const scratch = fs.mkdtempSync(path.join(process.env.TEMP ?? ".", "pangea-verify-"));
  try {
    const extract = run(["e", installerPath, `-o${scratch}`, `$PLUGINSDIR\\${payloadName}`, "-y"]);
    if (extract.status !== 0) {
      return [];
    }
    const listing = run(["l", "-slt", path.join(scratch, payloadName)]);
    if (listing.status !== 0) {
      return [];
    }
    return parseListing(listing.stdout);
  } finally {
    fs.rmSync(scratch, { recursive: true, force: true });
  }
}

// `7z l -slt` emits a blank-line-separated block per entry; the header block is
// dropped by requiring Size, which only real entries carry.
function parseListing(stdout) {
  const entries = [];
  for (const block of stdout.split(/\r?\n\r?\n/)) {
    const fields = {};
    for (const line of block.split(/\r?\n/)) {
      const match = /^(Path|Size|Method|Attributes) = (.*)$/.exec(line);
      if (match) {
        fields[match[1]] = match[2];
      }
    }
    // Directories carry Size = 0 and no method; they extract as plain mkdir.
    if (fields.Path && fields.Size && fields.Size !== "0") {
      entries.push({ path: fields.Path, method: fields.Method ?? "" });
    }
  }
  return entries;
}

function findPayload(installerPath) {
  const listing = run(["l", installerPath]);
  if (listing.status !== 0) {
    return null;
  }
  const match = /\$PLUGINSDIR\\(app-[a-z0-9]+\.(?:7z|zip))/i.exec(listing.stdout);
  return match ? match[1] : null;
}

function discoverInstallers() {
  const installerRoot = path.join(rootDir, "dist", "bin", "windows", "installer");
  if (!fs.existsSync(installerRoot)) {
    return [];
  }
  return fs
    .readdirSync(installerRoot, { withFileTypes: true })
    .filter((entry) => entry.isDirectory())
    .flatMap((archDir) => {
      const dir = path.join(installerRoot, archDir.name);
      return fs
        .readdirSync(dir)
        .filter((name) => name.toLowerCase().endsWith(".exe"))
        .map((name) => path.join(dir, name));
    });
}

function run(args) {
  return spawnSync(sevenZip, args, { encoding: "utf8", maxBuffer: 64 * 1024 * 1024 });
}

function resolveSevenZip() {
  const candidates = [
    "C:\\Program Files\\7-Zip\\7z.exe",
    "C:\\Program Files (x86)\\7-Zip\\7z.exe",
    "7z"
  ];
  for (const candidate of candidates) {
    const check = spawnSync(candidate, ["i"], { stdio: "ignore" });
    if (!check.error && check.status === 0) {
      return candidate;
    }
  }
  return null;
}
