import {
  createDecipheriv,
  createHash,
  createPrivateKey,
  createPublicKey,
  diffieHellman,
  generateKeyPairSync,
  hkdfSync,
  randomBytes,
  type KeyObject
} from "node:crypto";

// Browser sign-in ("desktop link", protocol v1). The website seals the
// account credential to a key that only this process holds, so the code that
// travels through the browser, the hub and the website's database is useless
// on its own. Kept free of electron so it can be tested directly.

export const DESKTOP_LINK_ORIGIN = "https://pangeavpn.org";
export const DESKTOP_LINK_PATH = "/app/link-desktop";
export const DESKTOP_LINK_REDEEM_ROUTE = "/api/client/desktop-link/redeem";

const SEAL_INFO = Buffer.from("pangea-desktop-link v1", "utf8");
const PAIR_INFO = Buffer.from("pangea-desktop-link pair v1", "utf8");
const SEAL_PREFIX = "pdl1";
const MAX_CREDENTIAL_LENGTH = 64;
// PKCS8 DER header for a raw 32-byte X25519 private key.
const X25519_PKCS8_PREFIX = Buffer.from("302e020100300506032b656e04220420", "hex");
const CROCKFORD = /^[0-9A-HJKMNP-TV-Z]{10}$/;

export interface DesktopLinkKeys {
  privateKey: KeyObject;
  /** Raw 32-byte X25519 public key. */
  publicKey: Buffer;
  /** `publicKey` as unpadded base64url: the `k` URL parameter. */
  k: string;
}

function keysFrom(privateKey: KeyObject): DesktopLinkKeys {
  const jwk = createPublicKey(privateKey).export({ format: "jwk" });
  const publicKey = Buffer.from(String(jwk.x), "base64url");
  return { privateKey, publicKey, k: publicKey.toString("base64url") };
}

export function generateDesktopLinkKeys(): DesktopLinkKeys {
  return keysFrom(generateKeyPairSync("x25519").privateKey);
}

/** For test vectors: rebuild the keypair from a raw 32-byte private key. */
export function desktopLinkKeysFromRaw(rawPrivateKey: Buffer): DesktopLinkKeys {
  if (rawPrivateKey.length !== 32) throw new Error("X25519 private key must be 32 bytes");
  const der = Buffer.concat([X25519_PKCS8_PREFIX, rawPrivateKey]);
  return keysFrom(createPrivateKey({ key: der, format: "der", type: "pkcs8" }));
}

/** 32 random bytes, unpadded base64url: the `s` URL parameter. */
export function newDesktopLinkState(): string {
  return randomBytes(32).toString("base64url");
}

export function buildDesktopLinkUrl(k: string, state: string, port: number): string {
  const url = new URL(DESKTOP_LINK_PATH, DESKTOP_LINK_ORIGIN);
  url.searchParams.set("k", k);
  url.searchParams.set("s", state);
  url.searchParams.set("p", String(port));
  return url.toString();
}

/** Six digits shown both here and on the website, so the user can check the
 *  browser is approving *this* app and not one an attacker started. */
export function desktopLinkPairingCode(publicKey: Buffer): string {
  const digest = createHash("sha256").update(PAIR_INFO).update(publicKey).digest();
  const digits = String(digest.readUInt32BE(0) % 1_000_000).padStart(6, "0");
  return `${digits.slice(0, 3)} ${digits.slice(3)}`;
}

/** Normalises a typed or redirected code the way account numbers are
 *  (spaces, dashes, case, O/I/L lookalikes); null when it isn't one. */
export function normalizeDesktopLinkCode(input: unknown): string | null {
  if (typeof input !== "string" || input.length > 32) return null;
  const code = input
    .replace(/[\s-]/g, "")
    .toUpperCase()
    .replace(/O/g, "0")
    .replace(/[IL]/g, "1");
  return CROCKFORD.test(code) ? code : null;
}

/** `XXXXX-XXXXX` as the user types it; tolerant of partial input. */
export function formatDesktopLinkCodeInput(input: string): string {
  const raw = input
    .replace(/[\s-]/g, "")
    .toUpperCase()
    .replace(/O/g, "0")
    .replace(/[IL]/g, "1")
    .replace(/[^0-9A-HJKMNP-TV-Z]/g, "")
    .slice(0, 10);
  return raw.length > 5 ? `${raw.slice(0, 5)}-${raw.slice(5)}` : raw;
}

/** The hub/website said no to this code: unknown, used, or expired (one
 *  answer for all three, by design). */
export class DesktopLinkCodeError extends Error {
  constructor(message = "browser sign-in code was not accepted") {
    super(message);
    this.name = "DesktopLinkCodeError";
  }
}

export class DesktopLinkSealError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DesktopLinkSealError";
  }
}

function part(value: string, bytes: number | null, what: string): Buffer {
  if (!/^[A-Za-z0-9_-]+$/.test(value)) throw new DesktopLinkSealError(`malformed ${what}`);
  const buf = Buffer.from(value, "base64url");
  if (bytes !== null && buf.length !== bytes) throw new DesktopLinkSealError(`bad ${what} length`);
  return buf;
}

/** Opens a `pdl1.` blob sealed to `keys` and parses the JSON inside. */
function openSealedJson(sealed: unknown, keys: DesktopLinkKeys): unknown {
  if (typeof sealed !== "string" || sealed.length > 4096) throw new DesktopLinkSealError("not a sealed credential");
  const parts = sealed.split(".");
  if (parts.length !== 4 || parts[0] !== SEAL_PREFIX) throw new DesktopLinkSealError("unknown seal format");
  const ephPub = part(parts[1], 32, "ephemeral key");
  const nonce = part(parts[2], 12, "nonce");
  const body = part(parts[3], null, "ciphertext");
  if (body.length < 17) throw new DesktopLinkSealError("ciphertext too short");

  let plaintext: Buffer;
  try {
    const ephKey = createPublicKey({ key: { kty: "OKP", crv: "X25519", x: ephPub.toString("base64url") }, format: "jwk" });
    const shared = diffieHellman({ privateKey: keys.privateKey, publicKey: ephKey });
    const key = Buffer.from(hkdfSync("sha256", shared, Buffer.concat([ephPub, keys.publicKey]), SEAL_INFO, 32));
    const decipher = createDecipheriv("aes-256-gcm", key, nonce);
    decipher.setAAD(SEAL_INFO);
    decipher.setAuthTag(body.subarray(body.length - 16));
    plaintext = Buffer.concat([decipher.update(body.subarray(0, body.length - 16)), decipher.final()]);
  } catch {
    throw new DesktopLinkSealError("sealed credential did not open");
  }

  try {
    return JSON.parse(plaintext.toString("utf8"));
  } catch {
    throw new DesktopLinkSealError("sealed payload is not JSON");
  }
}

/** Opens a `pdl1.` blob sealed to `keys`; returns the credential inside. */
export function openDesktopLinkSeal(sealed: unknown, keys: DesktopLinkKeys): string {
  const parsed = openSealedJson(sealed, keys);
  const credential = (parsed as { credential?: unknown } | null)?.credential;
  if (typeof credential !== "string" || credential.length === 0 || credential.length > MAX_CREDENTIAL_LENGTH) {
    throw new DesktopLinkSealError("sealed payload has no credential");
  }
  return credential;
}

/** Opens the browser's sealed `{"namesWrap": <base64url 32 bytes>}`: the key
 *  that unwraps the account's device-names key, which the server never sees. */
export function openDesktopLinkNamesWrap(sealed: unknown, keys: DesktopLinkKeys): Buffer {
  const parsed = openSealedJson(sealed, keys);
  const encoded = (parsed as { namesWrap?: unknown } | null)?.namesWrap;
  if (typeof encoded !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(encoded)) {
    throw new DesktopLinkSealError("sealed payload has no names wrap key");
  }
  const key = Buffer.from(encoded, "base64url");
  if (key.length !== 32) throw new DesktopLinkSealError("names wrap key is not 32 bytes");
  return key;
}
