import {
  generateKeyPairSync,
  diffieHellman,
  hkdfSync,
  createCipheriv,
  createDecipheriv,
  randomBytes,
  createPublicKey,
} from "node:crypto";

// Server X25519 public key (raw 32 bytes, base64)
const SERVER_PUBLIC_KEY_B64 = "dCdC/tJM0oSQPUDROrrZeGR8VUgww2YPUPHlaDhqWFM=";

const HKDF_SALT = Buffer.from("b9a288d01062a270368f67495ebafcec7eb910bee52855df69b22025cd205ae2", "hex");
const HKDF_INFO = Buffer.from("pangea-secure-channel-v1");

// SPKI DER prefix for X25519 public keys (12 bytes)
const SPKI_PREFIX = Buffer.from("302a300506032b656e032100", "hex");

interface EncryptedEnvelope {
  eph: string;
  iv: string;
  ct: string;
  tag: string;
}

export interface EncryptedResponse {
  iv: string;
  ct: string;
  tag: string;
}

export interface InnerResponse {
  status: number;
  body: unknown;
}

const serverPublicKey = createPublicKey({
  key: Buffer.concat([SPKI_PREFIX, Buffer.from(SERVER_PUBLIC_KEY_B64, "base64")]),
  format: "der",
  type: "spki",
});

export function encryptRequest(
  method: string,
  route: string,
  headers: Record<string, string>,
  body?: unknown
): { envelope: EncryptedEnvelope; aesKey: Buffer } {
  // Fresh ephemeral keypair per request (forward secrecy)
  const { publicKey: ephPub, privateKey: ephPriv } = generateKeyPairSync("x25519");

  // X25519 ECDH
  const sharedSecret = diffieHellman({
    privateKey: ephPriv,
    publicKey: serverPublicKey,
  });

  // HKDF-SHA256 → 32-byte AES key
  const aesKey = Buffer.from(
    hkdfSync("sha256", sharedSecret, HKDF_SALT, HKDF_INFO, 32)
  );

  const plaintext = JSON.stringify({ method, route, headers, body });

  // AES-256-GCM encrypt
  const iv = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", aesKey, iv);
  const ct = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();

  // Extract raw 32-byte ephemeral public key
  const ephPubDer = ephPub.export({ type: "spki", format: "der" }) as Buffer;
  const ephPubRaw = ephPubDer.subarray(12);

  return {
    envelope: {
      eph: ephPubRaw.toString("base64"),
      iv: iv.toString("base64"),
      ct: ct.toString("base64"),
      tag: tag.toString("base64"),
    },
    aesKey,
  };
}

export function decryptResponse(
  aesKey: Buffer,
  encrypted: EncryptedResponse
): InnerResponse {
  const iv = Buffer.from(encrypted.iv, "base64");
  const ct = Buffer.from(encrypted.ct, "base64");
  const tag = Buffer.from(encrypted.tag, "base64");

  const decipher = createDecipheriv("aes-256-gcm", aesKey, iv);
  decipher.setAuthTag(tag);
  const plaintext = Buffer.concat([
    decipher.update(ct),
    decipher.final(),
  ]).toString("utf8");

  return JSON.parse(plaintext) as InnerResponse;
}
