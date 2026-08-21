#!/usr/bin/env node
/**
 * Builds and signs the dead-drop bootstrap file. The signing key is read from
 * the environment and never written anywhere. See docs/deaddrop-bootstrap-design.md.
 *
 * Usage:
 *   PANGEA_DEADDROP_KEY_FILE=/path/to/deaddrop-active.pem \
 *   node scripts/publish-deaddrop.mjs \
 *     --out ../PangeaConfig/bootstrap-v1.json \
 *     --hub-ip 203.0.113.4 --fronted reserve-a.example.workers.dev [--days 90] [--key reserve]
 *
 * Publish only reserve capacity: everything in this file is world-readable and
 * is burned the moment it ships. Never put credentials in it.
 */

import { createPrivateKey, sign } from "node:crypto";
import { readFileSync, writeFileSync, existsSync } from "node:fs";

const PAYLOAD_VERSION = 1;
const DEFAULT_VALID_DAYS = 90;

function fail(message) {
  console.error(`publish-deaddrop: ${message}`);
  process.exit(1);
}

function parseArgs(argv) {
  const out = { hubIps: [], frontedEndpoints: [], key: "active", days: DEFAULT_VALID_DAYS };
  for (let i = 0; i < argv.length; i += 1) {
    const flag = argv[i];
    const value = argv[i + 1];
    if (flag === "--out") out.out = value;
    else if (flag === "--hub-ip") out.hubIps.push(value);
    else if (flag === "--fronted") out.frontedEndpoints.push(value);
    else if (flag === "--days") out.days = Number(value);
    else if (flag === "--key") out.key = value;
    else if (flag === "--seq") out.seq = Number(value);
    else fail(`unknown argument ${flag}`);
    i += 1;
  }
  return out;
}

const IPV4 = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/;

function validIpv4(host) {
  const match = IPV4.exec(host ?? "");
  if (match === null) return false;
  return match
    .slice(1, 5)
    .every((octet) => octet === "0" || (!octet.startsWith("0") && Number(octet) <= 255));
}

function validHostname(host) {
  if (typeof host !== "string" || host.length === 0 || host.length > 253) return false;
  if (IPV4.test(host)) return false;
  const labels = host.toLowerCase().split(".");
  if (labels.length < 2) return false;
  return labels.every((label) => /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(label));
}

function loadPrivateKey() {
  const file = process.env.PANGEA_DEADDROP_KEY_FILE;
  const inline = process.env.PANGEA_DEADDROP_KEY_PEM;
  const pem = inline ?? (file ? readFileSync(file, "utf8") : null);
  if (!pem) {
    fail("set PANGEA_DEADDROP_KEY_FILE or PANGEA_DEADDROP_KEY_PEM to the signing key");
  }
  const passphrase = process.env.PANGEA_DEADDROP_PASSPHRASE;
  try {
    return createPrivateKey(passphrase ? { key: pem, passphrase } : pem);
  } catch (err) {
    fail(`could not read the signing key: ${err.message}`);
  }
}

/** The published file's seq, so a rebuild never reuses or lowers one. A client
 *  ignores anything that does not beat the seq it already accepted. */
function nextSeq(outPath, override) {
  if (Number.isSafeInteger(override) && override > 0) return override;
  if (!existsSync(outPath)) return 1;
  try {
    const existing = JSON.parse(readFileSync(outPath, "utf8"));
    const payload = JSON.parse(Buffer.from(existing.payload, "base64").toString("utf8"));
    return Number.isSafeInteger(payload.seq) ? payload.seq + 1 : 1;
  } catch {
    fail(`${outPath} exists but could not be read; pass --seq to set the sequence explicitly`);
  }
}

const args = parseArgs(process.argv.slice(2));
if (!args.out) fail("--out is required");
if (args.key !== "active" && args.key !== "reserve") fail("--key must be active or reserve");
if (!Number.isFinite(args.days) || args.days <= 0) fail("--days must be a positive number");
if (args.hubIps.length === 0 && args.frontedEndpoints.length === 0) {
  fail("give at least one --hub-ip or --fronted");
}
for (const ip of args.hubIps) if (!validIpv4(ip)) fail(`not a usable IPv4 address: ${ip}`);
for (const host of args.frontedEndpoints) if (!validHostname(host)) fail(`not a usable hostname: ${host}`);

const privateKey = loadPrivateKey();
if (privateKey.asymmetricKeyType !== "ed25519") {
  fail(`expected an ed25519 key, got ${privateKey.asymmetricKeyType}`);
}

const now = new Date();
const expires = new Date(now.getTime() + args.days * 24 * 60 * 60 * 1000);
const payload = {
  v: PAYLOAD_VERSION,
  seq: nextSeq(args.out, args.seq),
  issued: now.toISOString(),
  expires: expires.toISOString(),
  hubIps: args.hubIps,
  frontedEndpoints: args.frontedEndpoints.map((host) => host.toLowerCase())
};

const bytes = Buffer.from(JSON.stringify(payload), "utf8");
const blob = {
  payload: bytes.toString("base64"),
  sig: sign(null, bytes, privateKey).toString("base64"),
  key: args.key
};

writeFileSync(args.out, `${JSON.stringify(blob, null, 2)}\n`, "utf8");
console.log(`wrote ${args.out}`);
console.log(`  seq     ${payload.seq}  (signed with the ${args.key} key)`);
console.log(`  expires ${payload.expires}`);
console.log(`  hubIps  ${payload.hubIps.join(", ") || "(none)"}`);
console.log(`  fronted ${payload.frontedEndpoints.join(", ") || "(none)"}`);
