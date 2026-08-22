/** Verification for the dead-drop bootstrap file: a signed blob, published
 *  publicly, carrying replacement addresses for a client that can no longer
 *  reach the hub by any of its usual paths. See docs/deaddrop-bootstrap-design.md. */

import { createPublicKey, verify } from "node:crypto";
import { isIPv4Literal } from "./ipLiteral.ts";
import { normalizeFrontedEndpoint } from "./frontedEndpoints.ts";

/** Raw 32-byte Ed25519 public keys, base64. Both are compiled into the client;
 *  the reserve exists so a compromised active key needs no emergency release. */
export interface DeadDropKeys {
  active: string;
  reserve: string;
}

export interface DeadDropPayload {
  seq: number;
  hubIps: string[];
  frontedEndpoints: string[];
}

export interface VerifyDeadDropOptions {
  keys: DeadDropKeys;
  /** Highest seq already accepted. A blob must beat it, which is what makes a
   *  replayed older file harmless. */
  minSeq: number;
  nowMs: number;
}

// SPKI DER prefix for Ed25519 public keys (12 bytes).
const SPKI_PREFIX = Buffer.from("302a300506032b6570032100", "hex");

const PAYLOAD_VERSION = 1;
const ED25519_SIGNATURE_BYTES = 64;
// The real file is a few hundred bytes; anything near this is not our blob.
const MAX_BLOB_BYTES = 64 * 1024;

function parseJsonObject(text: string): Record<string, unknown> | null {
  try {
    const value: unknown = JSON.parse(text);
    if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
    return value as Record<string, unknown>;
  } catch {
    return null;
  }
}

/** Decodes base64 only when the input is its own canonical encoding, so a
 *  padded or otherwise mangled variant cannot ride along with a valid signature. */
function decodeCanonicalBase64(value: string): Buffer | null {
  const decoded = Buffer.from(value, "base64");
  if (decoded.length === 0) return null;
  return decoded.toString("base64") === value ? decoded : null;
}

function verifiesUnder(rawKeyB64: string, message: Buffer, signature: Buffer): boolean {
  try {
    const key = createPublicKey({
      key: Buffer.concat([SPKI_PREFIX, Buffer.from(rawKeyB64, "base64")]),
      format: "der",
      type: "spki"
    });
    return verify(null, message, key, signature);
  } catch {
    return false;
  }
}

function validExpiry(value: unknown, nowMs: number): boolean {
  if (typeof value !== "string") return false;
  const at = Date.parse(value);
  return Number.isFinite(at) && at > nowMs;
}

function acceptedSeq(value: unknown, minSeq: number): number | null {
  if (typeof value !== "number" || !Number.isSafeInteger(value)) return null;
  if (value < 0 || value <= minSeq) return null;
  return value;
}

function cleanList(value: unknown, normalize: (entry: string) => string | null): string[] {
  if (!Array.isArray(value)) return [];
  const out: string[] = [];
  for (const entry of value) {
    if (typeof entry !== "string") continue;
    const normalized = normalize(entry);
    if (normalized !== null && !out.includes(normalized)) out.push(normalized);
  }
  return out;
}

/**
 * The verified payload, or null for anything that fails a check. Null always
 * means "carry on as before" — a rejected blob must never leave the client
 * worse off than not having fetched one.
 *
 * The blob's authority is enumerated, not merged: it contributes addresses and
 * nothing else. It cannot name a hub, supply a key, or change which methods run.
 */
export function verifyDeadDropBlob(
  raw: string | Buffer,
  options: VerifyDeadDropOptions
): DeadDropPayload | null {
  const text = typeof raw === "string" ? raw : raw.toString("utf8");
  if (text.length === 0 || Buffer.byteLength(text, "utf8") > MAX_BLOB_BYTES) return null;

  const envelope = parseJsonObject(text);
  if (!envelope) return null;
  if (typeof envelope.payload !== "string" || typeof envelope.sig !== "string") return null;

  const message = decodeCanonicalBase64(envelope.payload);
  const signature = decodeCanonicalBase64(envelope.sig);
  if (!message || !signature || signature.length !== ED25519_SIGNATURE_BYTES) return null;

  // `key` is a hint about which key signed, never a widening of what we accept.
  const trusted = [options.keys.active, options.keys.reserve];
  if (!trusted.some((key) => verifiesUnder(key, message, signature))) return null;

  const body = parseJsonObject(message.toString("utf8"));
  if (!body) return null;
  if (body.v !== PAYLOAD_VERSION) return null;

  const seq = acceptedSeq(body.seq, options.minSeq);
  if (seq === null) return null;
  if (!validExpiry(body.expires, options.nowMs)) return null;

  const hubIps = cleanList(body.hubIps, (entry) => {
    const host = entry.trim();
    return isIPv4Literal(host) ? host : null;
  });
  const frontedEndpoints = cleanList(body.frontedEndpoints, normalizeFrontedEndpoint);

  // A blob that survives verification but names nothing usable is not an
  // instruction to forget what we already have.
  if (hubIps.length === 0 && frontedEndpoints.length === 0) return null;

  return { seq, hubIps, frontedEndpoints };
}
