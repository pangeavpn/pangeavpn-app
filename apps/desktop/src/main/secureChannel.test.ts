import assert from "node:assert/strict";
import test from "node:test";
import crypto from "node:crypto";
import {
  decryptResponse,
  deriveV2Keys,
  encryptRequest,
  encryptRequestV2,
  sealRequest,
  v2AssociatedData,
  type EncryptedResponse
} from "./secureChannel.ts";
import { PQ_ALGORITHM } from "../shared/postQuantum.ts";

const X25519_SPKI_PREFIX = Buffer.from("302a300506032b656e032100", "hex");
const MLKEM_SPKI_PREFIX = Buffer.from("308204b2300b0609608648016503040402038204a100", "hex");

function nativeMlKemWorks(): boolean {
  try {
    crypto.encapsulate(crypto.generateKeyPairSync("ml-kem-768").publicKey);
    return true;
  } catch {
    return false;
  }
}
const skipWithoutMlKem = { skip: nativeMlKemWorks() ? false : "no native ML-KEM in this Node" };

interface V2Envelope {
  eph: string;
  kem: string;
  iv: string;
  ct: string;
  tag: string;
}

/** A stand-in hub: its static keys and the hub side of the v2 key schedule. */
function makeHub() {
  const dh = crypto.generateKeyPairSync("x25519");
  const kem = crypto.generateKeyPairSync("ml-kem-768");
  const kemDer = kem.publicKey.export({ type: "spki", format: "der" }) as Buffer;
  const encapsulate = async (algorithm: string, kemPublicKey: string) => {
    assert.equal(algorithm, PQ_ALGORITHM);
    const key = crypto.createPublicKey({
      key: Buffer.concat([MLKEM_SPKI_PREFIX, Buffer.from(kemPublicKey, "base64")]),
      format: "der",
      type: "spki"
    });
    const { sharedKey, ciphertext } = crypto.encapsulate(key);
    return { kemCiphertext: ciphertext.toString("base64"), sharedSecret: Buffer.from(sharedKey).toString("base64") };
  };
  const keysFor = (ephB64: string, kemB64: string) => {
    const eph = crypto.createPublicKey({
      key: Buffer.concat([X25519_SPKI_PREFIX, Buffer.from(ephB64, "base64")]),
      format: "der",
      type: "spki"
    });
    const dhSecret = crypto.diffieHellman({ privateKey: dh.privateKey, publicKey: eph });
    const kemSecret = Buffer.from(crypto.decapsulate(kem.privateKey, Buffer.from(kemB64, "base64")));
    return deriveV2Keys(dhSecret, kemSecret);
  };
  const open = (key: Buffer, enc: EncryptedResponse, aad: Buffer) => {
    const d = crypto.createDecipheriv("aes-256-gcm", key, Buffer.from(enc.iv, "base64"));
    d.setAAD(aad);
    d.setAuthTag(Buffer.from(enc.tag, "base64"));
    return Buffer.concat([d.update(Buffer.from(enc.ct, "base64")), d.final()]).toString("utf8");
  };
  const seal = (key: Buffer, plaintext: string, aad: Buffer): EncryptedResponse => {
    const iv = crypto.randomBytes(12);
    const c = crypto.createCipheriv("aes-256-gcm", key, iv);
    c.setAAD(aad);
    const ct = Buffer.concat([c.update(plaintext, "utf8"), c.final()]);
    return { iv: iv.toString("base64"), ct: ct.toString("base64"), tag: c.getAuthTag().toString("base64") };
  };
  return {
    dhPublic: dh.publicKey,
    kemPublicB64: kemDer.subarray(MLKEM_SPKI_PREFIX.length).toString("base64"),
    encapsulate,
    keysFor,
    open,
    seal
  };
}

test("v1 request and response round-trip under the derived key", () => {
  const { envelope, aesKey } = encryptRequest("GET", "/api/x", { a: "b" }, undefined);
  assert.ok(envelope.eph && envelope.iv && envelope.ct && envelope.tag);
  const iv = crypto.randomBytes(12);
  const c = crypto.createCipheriv("aes-256-gcm", aesKey, iv);
  const ct = Buffer.concat([c.update(JSON.stringify({ status: 200, body: { ok: 1 } })), c.final()]);
  const inner = decryptResponse(aesKey, {
    iv: iv.toString("base64"),
    ct: ct.toString("base64"),
    tag: c.getAuthTag().toString("base64")
  });
  assert.deepEqual(inner, { status: 200, body: { ok: 1 } });
});

test("v2 round-trips against a hub holding both static keys", skipWithoutMlKem, async () => {
  const hub = makeHub();
  const kem = await hub.encapsulate(PQ_ALGORITHM, hub.kemPublicB64);
  const sealed = encryptRequestV2("POST", "/api/register", { "X-License-Key": "k" }, { region: "eu" }, kem, hub.dhPublic);
  assert.equal(sealed.route, "/v2/secure");
  const envelope = sealed.envelope as V2Envelope;
  assert.equal(envelope.kem, kem.kemCiphertext);

  const keys = hub.keysFor(envelope.eph, envelope.kem);
  const aad = v2AssociatedData(envelope.eph, envelope.kem);
  const request = JSON.parse(hub.open(keys.c2s, envelope, aad));
  assert.deepEqual(request, {
    method: "POST",
    route: "/api/register",
    headers: { "X-License-Key": "k" },
    body: { region: "eu" }
  });

  const reply = hub.seal(keys.s2c, JSON.stringify({ status: 200, body: { assignedIP: "10.0.0.2" } }), aad);
  assert.deepEqual(sealed.open(reply), { status: 200, body: { assignedIP: "10.0.0.2" } });
});

test("v2 rejects a reflected request and a reply bound to another request", skipWithoutMlKem, async () => {
  const hub = makeHub();
  const kem = await hub.encapsulate(PQ_ALGORITHM, hub.kemPublicB64);
  const sealed = encryptRequestV2("GET", "/api/client/regions", {}, undefined, kem, hub.dhPublic);
  const envelope = sealed.envelope as V2Envelope;

  assert.throws(() => sealed.open(envelope));

  const keys = hub.keysFor(envelope.eph, envelope.kem);
  const otherAad = v2AssociatedData(envelope.eph, Buffer.alloc(1088).toString("base64"));
  const misbound = hub.seal(keys.s2c, JSON.stringify({ status: 200, body: null }), otherAad);
  assert.throws(() => sealed.open(misbound));
});

test("encryptRequestV2 refuses key material of the wrong size", () => {
  const ct = Buffer.alloc(1088).toString("base64");
  const ss = Buffer.alloc(32).toString("base64");
  assert.throws(() => encryptRequestV2("GET", "/x", {}, undefined, { kemCiphertext: ct, sharedSecret: "AAAA" }), /shared secret/);
  assert.throws(() => encryptRequestV2("GET", "/x", {}, undefined, { kemCiphertext: "AAAA", sharedSecret: ss }), /ciphertext/);
});

test("sealRequest stays on v1 without a pinned key, an encapsulator, or a daemon answer", async () => {
  const pinned = Buffer.alloc(1184).toString("base64");
  const failing = async () => {
    throw new Error("daemon down");
  };
  assert.equal((await sealRequest("GET", "/x", {}, undefined, null, pinned)).route, "/v1/secure");
  assert.equal((await sealRequest("GET", "/x", {}, undefined, async () => null, "")).route, "/v1/secure");
  assert.equal((await sealRequest("GET", "/x", {}, undefined, async () => null, pinned)).route, "/v1/secure");
  assert.equal((await sealRequest("GET", "/x", {}, undefined, failing, pinned)).route, "/v1/secure");
});

test("sealRequest takes v2 when the daemon encapsulates", skipWithoutMlKem, async () => {
  const hub = makeHub();
  const sealed = await sealRequest("GET", "/x", {}, undefined, hub.encapsulate, hub.kemPublicB64);
  assert.equal(sealed.route, "/v2/secure");
  assert.ok((sealed.envelope as V2Envelope).kem);
});

// The hub pins the same vector; a drift in salt, label or order fails both suites.
test("v2 key schedule matches the pinned vector", () => {
  const keys = deriveV2Keys(Buffer.alloc(32), Buffer.alloc(32));
  assert.equal(keys.c2s.toString("hex"), "d443eb63dab3ae24d1c3c7ea022d229564c44ace58e2cdcc4a622de51adf1aa9");
  assert.equal(keys.s2c.toString("hex"), "b22fe5e3f91412fd7a81a8c46dbf8f5ba54a565b688c8b52ad71335123c0e1b2");
  assert.equal(v2AssociatedData("ZXBo", "a2Vt").toString("utf8"), "ZXBo.a2Vt");
});
