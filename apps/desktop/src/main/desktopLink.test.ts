import assert from "node:assert/strict";
import { createCipheriv, createPublicKey, diffieHellman, generateKeyPairSync, hkdfSync, randomBytes } from "node:crypto";
import test from "node:test";
import {
  buildDesktopLinkUrl,
  desktopLinkKeysFromRaw,
  desktopLinkPairingCode,
  formatDesktopLinkCodeInput,
  generateDesktopLinkKeys,
  newDesktopLinkState,
  normalizeDesktopLinkCode,
  openDesktopLinkNamesWrap,
  openDesktopLinkSeal,
  type DesktopLinkKeys
} from "./desktopLink.ts";

// Shared with PangeaWebsite's sealing tests: both sides must agree byte for byte.
const VECTOR = {
  appPrivateKeyHex: "a1".repeat(32),
  appPublicKeyB64u: "wwb7DvK_i3-TutmBVfo32ux02wxMvtpsbx26nTZVglI",
  sealed:
    "pdl1.20glfhI3l2p0rYz-3KACE0CP6JrGJR8bkwJF8kK1wxo.w8PDw8PDw8PDw8PD.YgANvB5ipYXZbVrZfu7PSkyZ5JMedQr9bmyXLknqso-07qQNS7BGMqjtpsDQdQLYHC_sb__O68Vu",
  credential: "ABCDEFGHJKMNPQRSTVWXYZ01",
  pairingCode: "297 264"
};

const INFO = Buffer.from("pangea-desktop-link v1");

function seal(keys: DesktopLinkKeys, payload: string): string {
  const eph = generateKeyPairSync("x25519");
  const ephPub = Buffer.from(String(eph.publicKey.export({ format: "jwk" }).x), "base64url");
  const appKey = createPublicKey({ key: { kty: "OKP", crv: "X25519", x: keys.k }, format: "jwk" });
  const shared = diffieHellman({ privateKey: eph.privateKey, publicKey: appKey });
  const key = Buffer.from(hkdfSync("sha256", shared, Buffer.concat([ephPub, keys.publicKey]), INFO, 32));
  const nonce = randomBytes(12);
  const cipher = createCipheriv("aes-256-gcm", key, nonce);
  cipher.setAAD(INFO);
  const ct = Buffer.concat([cipher.update(payload, "utf8"), cipher.final(), cipher.getAuthTag()]);
  return `pdl1.${ephPub.toString("base64url")}.${nonce.toString("base64url")}.${ct.toString("base64url")}`;
}

const vectorKeys = desktopLinkKeysFromRaw(Buffer.from(VECTOR.appPrivateKeyHex, "hex"));

test("the shared test vector opens to the sealed credential", () => {
  assert.equal(vectorKeys.k, VECTOR.appPublicKeyB64u);
  assert.equal(openDesktopLinkSeal(VECTOR.sealed, vectorKeys), VECTOR.credential);
});

test("the pairing code matches the shared vector", () => {
  assert.equal(desktopLinkPairingCode(vectorKeys.publicKey), VECTOR.pairingCode);
});

test("a fresh keypair round-trips and encodes k as 43 base64url chars", () => {
  const keys = generateDesktopLinkKeys();
  assert.match(keys.k, /^[A-Za-z0-9_-]{43}$/);
  assert.equal(openDesktopLinkSeal(seal(keys, JSON.stringify({ credential: "1234567890123456" })), keys), "1234567890123456");
});

test("a seal for another app's key does not open", () => {
  const other = generateDesktopLinkKeys();
  assert.throws(() => openDesktopLinkSeal(seal(other, JSON.stringify({ credential: "X" })), vectorKeys), {
    name: "DesktopLinkSealError"
  });
});

test("any flipped byte in the ciphertext is rejected", () => {
  const parts = VECTOR.sealed.split(".");
  const body = Buffer.from(parts[3], "base64url");
  for (const index of [0, body.length - 1]) {
    const tampered = Buffer.from(body);
    tampered[index] ^= 0x01;
    const blob = [parts[0], parts[1], parts[2], tampered.toString("base64url")].join(".");
    assert.throws(() => openDesktopLinkSeal(blob, vectorKeys), { name: "DesktopLinkSealError" });
  }
});

test("malformed seals are rejected before any crypto", () => {
  const [, eph, nonce, body] = VECTOR.sealed.split(".");
  const bad = [
    undefined,
    42,
    "",
    `pdl2.${eph}.${nonce}.${body}`,
    `pdl1.${eph}.${nonce}`,
    `pdl1.${eph}.${nonce}.${body}.x`,
    `pdl1.${eph.slice(0, 40)}.${nonce}.${body}`,
    `pdl1.${eph}.${nonce.slice(0, 10)}.${body}`,
    `pdl1.${eph}.${nonce}.AAAA`,
    `pdl1.${eph}.${nonce}.${body}==`
  ];
  for (const blob of bad) {
    assert.throws(() => openDesktopLinkSeal(blob, vectorKeys), { name: "DesktopLinkSealError" }, String(blob));
  }
});

test("a payload without a usable credential is rejected", () => {
  const keys = generateDesktopLinkKeys();
  for (const payload of ["not json", "null", "{}", '{"credential":5}', '{"credential":""}', JSON.stringify({ credential: "A".repeat(65) })]) {
    assert.throws(() => openDesktopLinkSeal(seal(keys, payload), keys), { name: "DesktopLinkSealError" }, payload);
  }
});

test("codes normalise like account numbers and must be 10 Crockford chars", () => {
  assert.equal(normalizeDesktopLinkCode("7K3MQ-9XA2P"), "7K3MQ9XA2P");
  assert.equal(normalizeDesktopLinkCode(" 7k3mq 9xa2p "), "7K3MQ9XA2P");
  assert.equal(normalizeDesktopLinkCode("OIL00-11111"), "01100" + "11111");
  assert.equal(normalizeDesktopLinkCode("7K3MQ9XA2"), null);
  assert.equal(normalizeDesktopLinkCode("7K3MQ9XA2PP"), null);
  assert.equal(normalizeDesktopLinkCode("7K3MQ9XA2U"), null);
  assert.equal(normalizeDesktopLinkCode(null), null);
  assert.equal(normalizeDesktopLinkCode("A".repeat(33)), null);
});

test("code input is formatted as XXXXX-XXXXX while typing", () => {
  assert.equal(formatDesktopLinkCodeInput("7k3m"), "7K3M");
  assert.equal(formatDesktopLinkCodeInput("7k3mq9"), "7K3MQ-9");
  assert.equal(formatDesktopLinkCodeInput("7K3MQ-9XA2P-EXTRA"), "7K3MQ-9XA2P");
  assert.equal(formatDesktopLinkCodeInput("7K3MQ 9U!A"), "7K3MQ-9A");
});

test("the link URL carries k, s and p on pangeavpn.org", () => {
  const state = newDesktopLinkState();
  assert.match(state, /^[A-Za-z0-9_-]{43}$/);
  const url = new URL(buildDesktopLinkUrl(VECTOR.appPublicKeyB64u, state, 51234));
  assert.equal(url.origin, "https://pangeavpn.org");
  assert.equal(url.pathname, "/app/link-desktop");
  assert.equal(url.searchParams.get("k"), VECTOR.appPublicKeyB64u);
  assert.equal(url.searchParams.get("s"), state);
  assert.equal(url.searchParams.get("p"), "51234");
});

test("the browser's sealed names wrap key opens to the same 32 bytes", () => {
  const keys = generateDesktopLinkKeys();
  const wrap = randomBytes(32);
  const blob = seal(keys, JSON.stringify({ namesWrap: wrap.toString("base64url") }));
  assert.deepEqual(openDesktopLinkNamesWrap(blob, keys), wrap);
});

test("a names wrap blob that is malformed, foreign or the wrong size is rejected", () => {
  const keys = generateDesktopLinkKeys();
  const other = generateDesktopLinkKeys();
  const good = JSON.stringify({ namesWrap: randomBytes(32).toString("base64url") });
  assert.throws(() => openDesktopLinkNamesWrap(seal(other, good), keys), { name: "DesktopLinkSealError" });
  for (const payload of ["{}", '{"namesWrap":5}', JSON.stringify({ namesWrap: randomBytes(16).toString("base64url") }), JSON.stringify({ namesWrap: "!".repeat(43) }), '{"credential":"x"}']) {
    assert.throws(() => openDesktopLinkNamesWrap(seal(keys, payload), keys), { name: "DesktopLinkSealError" }, payload);
  }
  assert.throws(() => openDesktopLinkNamesWrap(null, keys), { name: "DesktopLinkSealError" });
});
