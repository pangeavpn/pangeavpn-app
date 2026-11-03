import assert from "node:assert/strict";
import test from "node:test";
import { checkDeviceName, deviceNameKey, openDeviceName, sealDeviceName } from "./deviceName.ts";

const CREDENTIAL = "0123-4567-89AB-CDEF-GHJK-MNPQ";
const SEALED = "zk1:AAECAwQFBgcICQoLHwxG/vnIxpQKsmvjxj/f6LESbeKr2oZxy10n5ycj7A==";
const PLAINTEXT = "Alice's MacBook";

test("the pinned zk1 vector derives, seals and opens", () => {
  const key = deviceNameKey(CREDENTIAL);
  assert.equal(key?.toString("hex"), "70e06e761f70ca150869c979046413980aaad153c012ed9a9ee5adfb4e9e3c3f");
  assert.equal(sealDeviceName(PLAINTEXT, key!, Buffer.from("000102030405060708090a0b", "hex")), SEALED);
  assert.equal(openDeviceName(SEALED, key), PLAINTEXT);
});

test("only an account number is a key source, in any spelling", () => {
  const want = deviceNameKey("0123456789ABCDEFGHJKMNPQ")!.toString("hex");
  for (const same of [CREDENTIAL, " 0123 4567 89ab cdef ghjk mnpq\n", "o123-4567-89AB-CDEF-GHJK-MNPQ"]) {
    assert.equal(deviceNameKey(same)?.toString("hex"), want);
  }
  for (const legacy of ["1234567890123456", "", null, "0123456789ABCDEFGHJKMNPU", "0123456789ABCDEFGHJKMNPQ0"]) {
    assert.equal(deviceNameKey(legacy), null);
  }
});

test("a fresh nonce every seal, and the name round-trips", () => {
  const key = deviceNameKey(CREDENTIAL)!;
  const a = sealDeviceName("Work Phone 😀", key);
  assert.notEqual(a, sealDeviceName("Work Phone 😀", key));
  assert.equal(openDeviceName(a, key), "Work Phone 😀");
});

test("anything unreadable opens to null", () => {
  const key = deviceNameKey(CREDENTIAL)!;
  const other = deviceNameKey("ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ");
  const tampered = SEALED.slice(0, 20) + "A" + SEALED.slice(21);
  assert.equal(openDeviceName(SEALED, other), null);
  assert.equal(openDeviceName(SEALED, null), null);
  assert.equal(openDeviceName(tampered, key), null);
  assert.equal(openDeviceName("Swift Fox", key), null);
  assert.equal(openDeviceName("enc1:AAECAwQFBgcICQoLHwxG", key), null);
  assert.equal(openDeviceName(sealDeviceName("", key), key), null);
  assert.equal(openDeviceName(null, key), null);
});

test("device names are trimmed and checked like the mobile core", () => {
  assert.equal(checkDeviceName("  Work Phone \n"), "Work Phone");
  assert.equal(checkDeviceName("😀".repeat(32)), "😀".repeat(32));
  for (const bad of ["", "   ", "a".repeat(65), "😀".repeat(33), "a\u0000b", "a\u0085b", "Phone\u202Egpj.exe", 42]) {
    assert.throws(() => checkDeviceName(bad));
  }
});
