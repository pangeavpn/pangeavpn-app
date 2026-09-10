import {
  generateKeyPairSync,
  diffieHellman,
  hkdfSync,
  createCipheriv,
  createDecipheriv,
  randomBytes,
  createPublicKey,
  type KeyObject,
} from "node:crypto";

import { PQ_ALGORITHM, type Encapsulation } from "../shared/postQuantum.ts";

// Server X25519 public key (raw 32 bytes, base64)
const SERVER_PUBLIC_KEY_B64 = "dCdC/tJM0oSQPUDROrrZeGR8VUgww2YPUPHlaDhqWFM=";

// Server ML-KEM-768 public key (raw 1184 bytes, base64). Empty keeps every
// request on /v1/secure; pin the hub's key here to turn /v2/secure on.
export const SERVER_KEM_PUBLIC_KEY_B64 =
  "+VGdrHyFbqe1VCAIXhqVUKClytKNWNWF/ewL9SBBcNieILpWldUaXKyLxMxEZROHnFiDgRAfJ0cKfUh6a1Y6bIhDIZddpMc0kWHPbIUmHahUspeVHEbOICq7WZd7JIHIFCmY7gTMinDLcLLJXtJyOpk6XUAT4IQd3fNPVcuqeziGeZfPZeG1aeVYaXUR18Kv2VbHDGUp/fslJKMrv7jKYGR0CGHHNfucsrMF4GlVRYcSkXJHwao78pydQZEP+xYqDZkXEopAk0k94ORoJ1MmeUUZXFk47Lur58QxSXSeuzNrTcsdkNiomJK9m+mATsu1DluGOfyhuexzRrp8zWIPmzqpkLeG/PQOksVrOPKwd9OAEkavX9s6GVdBpad3bPsFdcxBNxt9GddxX9nAH4ybwqdoZtBcjksILlOMkMJb/MjL2ZkdjlIV+ZDNj/EJeIxl3tlqlMC2cewlZ9QBhHeOSsy47Fp50ChcifxcH/Z847grJCEeWeJ4RlTNqXMq3MY+v/bOldMWdfyacRG5+GdahZQM+fVQI8JmLiZd3kuKhPk15RGbMQADgCk9MJpUWkhxoewyBTbI3Oy659gXzxRq8IC0o1AvQroYBFI0+0pLWugF/sc7Yfl+6FOZo3TGqwm6PZbHtsEHKYdVV0OUiahC06K2lEPHGKBEQay6pIwVE5M4Twp+igzNrNjD17F9u9k2rmRvBhldPqq2Z0cnmrIsvqRhM2x+0bmH5ltXDBh3OEtOfmkNRiIHTfiqZnMS23hVArABO2MZoCMb4ppzzdR32vsOdfsBdNW9b4cmtNWtfcpf+LCKnpQJlmwe2SiVizMqUci/YtubJAtWpVA5A/isgdWbdRwWstwrLzt7+DTAkrQ7AgEbbCiwcyZL9eUjKAUfKBE5/ugD3UKx/lqDLZcP/vpLpFN+7llq/ARbFEmO9jQqUOhlB/uN5Ll7s+hb1PZCiUQXbWqpQ3UkVjWH57Ib21dkuiKbXqFefGJLU4Nbt7VQVMSsdWK5cnlY5PtYRqsO+DaTPXyf8YKAq5h3MYZ+TKl6E6AbIVkyHSh48QvP+EJ1WkGvihWRieG5EfG1qlRAsOkvScMxuXF66mt2ZiBZAO3Ax5YxvYIgjsibDSQ+v8pf9CZ4guwnBek3u8UXFGNR4mcuktkndvJzmwxatDAsTZkP1CR6MJFtSSMUaWxd0wureDy1TIU2ZGUSOhqje6KuyAJlysu959IxnqnEIURQBoFhL9yIVXcKKrmLctEhGSItPkUnRnI1AnLFEwTL/vGH82pdntfKBqRLRXdvYTWcIbUi1peaRQifvIdTJuK/u5hGxBBIVvaZ7uLCRSx34uUoMtKmfOhb7vJvcLvG96LGuLs+ICxMPhoRDBZtsORR+HgEhNItoWKm7aZBAXSM6AOujLy18EwSZVZdWuYfm+UQ7jkxv5eLeOg0ueAG6QReySF9BDErtYW3iIs/GYpExfKdMiTK2beC1RhbaLFRb6EkiiIUuQoEkDd/8sdUEelAVpZWvCwNngudPMgPzOx7G+FUUyXRUq8XMc/PUOlMmE3Y8Vp4XWxR9BF6mtpYbalmXlY=";

const HKDF_SALT = Buffer.from("b9a288d01062a270368f67495ebafcec7eb910bee52855df69b22025cd205ae2", "hex");
const HKDF_INFO = Buffer.from("pangea-secure-channel-v1");
const HKDF_INFO_V2_C2S = Buffer.from("pangea-secure-channel-v2/c2s");
const HKDF_INFO_V2_S2C = Buffer.from("pangea-secure-channel-v2/s2c");

const KEM_CIPHERTEXT_SIZE = 1088;
const KEM_SHARED_SECRET_SIZE = 32;

// SPKI DER prefix for X25519 public keys (12 bytes)
const SPKI_PREFIX = Buffer.from("302a300506032b656e032100", "hex");

interface EncryptedEnvelope {
  eph: string;
  iv: string;
  ct: string;
  tag: string;
}

/** The v2 envelope adds the ML-KEM ciphertext next to the ephemeral key. */
interface EncryptedEnvelopeV2 extends EncryptedEnvelope {
  kem: string;
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

/** A request sealed for one of the secure routes, and the way to read its reply. */
export interface SealedRequest {
  route: "/v1/secure" | "/v2/secure";
  envelope: EncryptedEnvelope | EncryptedEnvelopeV2;
  open(encrypted: EncryptedResponse): InnerResponse;
}

export type Encapsulator = (algorithm: string, kemPublicKey: string) => Promise<Encapsulation | null>;

const serverPublicKey = createPublicKey({
  key: Buffer.concat([SPKI_PREFIX, Buffer.from(SERVER_PUBLIC_KEY_B64, "base64")]),
  format: "der",
  type: "spki",
});

function ephemeralAgreement(serverKey: KeyObject): { ephB64: string; sharedSecret: Buffer } {
  const { publicKey: ephPub, privateKey: ephPriv } = generateKeyPairSync("x25519");
  const sharedSecret = diffieHellman({ privateKey: ephPriv, publicKey: serverKey });
  const ephPubDer = ephPub.export({ type: "spki", format: "der" }) as Buffer;
  return { ephB64: ephPubDer.subarray(12).toString("base64"), sharedSecret };
}

function gcmSeal(key: Buffer, plaintext: string, aad?: Buffer): EncryptedResponse {
  const iv = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", key, iv);
  if (aad) cipher.setAAD(aad);
  const ct = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
  return { iv: iv.toString("base64"), ct: ct.toString("base64"), tag: cipher.getAuthTag().toString("base64") };
}

function gcmOpen(key: Buffer, encrypted: EncryptedResponse, aad?: Buffer): string {
  const decipher = createDecipheriv("aes-256-gcm", key, Buffer.from(encrypted.iv, "base64"));
  if (aad) decipher.setAAD(aad);
  decipher.setAuthTag(Buffer.from(encrypted.tag, "base64"));
  return Buffer.concat([decipher.update(Buffer.from(encrypted.ct, "base64")), decipher.final()]).toString("utf8");
}

export function encryptRequest(
  method: string,
  route: string,
  headers: Record<string, string>,
  body?: unknown
): { envelope: EncryptedEnvelope; aesKey: Buffer } {
  // Fresh ephemeral client keypair per request (ephemeral-static ECDH against the pinned server key; not PFS)
  const { ephB64, sharedSecret } = ephemeralAgreement(serverPublicKey);
  const aesKey = Buffer.from(hkdfSync("sha256", sharedSecret, HKDF_SALT, HKDF_INFO, 32));
  const sealed = gcmSeal(aesKey, JSON.stringify({ method, route, headers, body }));
  return { envelope: { eph: ephB64, ...sealed }, aesKey };
}

export function decryptResponse(aesKey: Buffer, encrypted: EncryptedResponse): InnerResponse {
  return JSON.parse(gcmOpen(aesKey, encrypted)) as InnerResponse;
}

/** Both v2 keys from the hybrid secret; exported so a test can play the hub. */
export function deriveV2Keys(dhSecret: Buffer, kemSecret: Buffer): { c2s: Buffer; s2c: Buffer } {
  const ikm = Buffer.concat([dhSecret, kemSecret]);
  return {
    c2s: Buffer.from(hkdfSync("sha256", ikm, HKDF_SALT, HKDF_INFO_V2_C2S, 32)),
    s2c: Buffer.from(hkdfSync("sha256", ikm, HKDF_SALT, HKDF_INFO_V2_S2C, 32)),
  };
}

/** The bytes both directions authenticate, tying a reply to the request it answers. */
export function v2AssociatedData(ephB64: string, kemB64: string): Buffer {
  return Buffer.from(`${ephB64}.${kemB64}`, "utf8");
}

export function encryptRequestV2(
  method: string,
  route: string,
  headers: Record<string, string>,
  body: unknown,
  kem: Encapsulation,
  serverKey: KeyObject = serverPublicKey
): SealedRequest {
  const kemSecret = Buffer.from(kem.sharedSecret, "base64");
  if (kemSecret.length !== KEM_SHARED_SECRET_SIZE) {
    throw new Error(`KEM shared secret is ${kemSecret.length} bytes, want ${KEM_SHARED_SECRET_SIZE}`);
  }
  if (Buffer.from(kem.kemCiphertext, "base64").length !== KEM_CIPHERTEXT_SIZE) {
    throw new Error("KEM ciphertext has the wrong length");
  }
  const { ephB64, sharedSecret } = ephemeralAgreement(serverKey);
  const keys = deriveV2Keys(sharedSecret, kemSecret);
  const aad = v2AssociatedData(ephB64, kem.kemCiphertext);
  const sealed = gcmSeal(keys.c2s, JSON.stringify({ method, route, headers, body }), aad);
  return {
    route: "/v2/secure",
    envelope: { eph: ephB64, kem: kem.kemCiphertext, ...sealed },
    open: (encrypted) => JSON.parse(gcmOpen(keys.s2c, encrypted, aad)) as InnerResponse,
  };
}

// v2 when a server KEM key is pinned and the daemon can encapsulate for it,
// else v1. Only the local daemon decides, so the network cannot force v1.
export async function sealRequest(
  method: string,
  route: string,
  headers: Record<string, string>,
  body: unknown,
  encapsulate: Encapsulator | null,
  serverKemPublicKey: string = SERVER_KEM_PUBLIC_KEY_B64
): Promise<SealedRequest> {
  if (serverKemPublicKey && encapsulate) {
    try {
      const kem = await encapsulate(PQ_ALGORITHM, serverKemPublicKey);
      if (kem) return encryptRequestV2(method, route, headers, body, kem);
    } catch (err) {
      console.log(`[SecureChannel] v2 unavailable, using v1: ${err instanceof Error ? err.message : String(err)}`);
    }
  }
  const { envelope, aesKey } = encryptRequest(method, route, headers, body);
  return { route: "/v1/secure", envelope, open: (encrypted) => decryptResponse(aesKey, encrypted) };
}
