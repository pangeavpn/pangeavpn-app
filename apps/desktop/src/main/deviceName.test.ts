import assert from "node:assert/strict";
import test from "node:test";
import { hkdfSync } from "node:crypto";
import { readFileSync } from "node:fs";
import {
  NAME_KEY_UNREADABLE,
  RENAME_NEEDS_ACCOUNT_NUMBER,
  checkDeviceName,
  newNameKey,
  normalizeAccountNumber,
  numberWrapKey,
  openName,
  openValue,
  renameDevice,
  sealName,
  sealRename,
  sealValue,
  shownDevices,
  unwrapNameKey,
  wrapNameKey,
  type HubCall,
  type HubDeviceList
} from "./deviceName.ts";

interface SealedVector {
  plain?: string;
  nonce_hex: string;
  aad: string;
  sealed: string;
}

const v = JSON.parse(
  readFileSync(new URL("../../../../daemon/mobile/testdata/names_v2_vectors.json", import.meta.url), "utf8")
) as {
  salt: string;
  names_key_hex: string;
  account_number: string;
  account_number_inputs: string[];
  names_secret_b64url: string;
  device_name_key: string;
  device_id: string;
  other_device_id: string;
  number_wrap_key_hex: string;
  secret_seal_key_hex: string;
  server_wrap_key_hex: string;
  name_key: SealedVector;
  account_number_sealed: SealedVector;
  name_key_server: SealedVector;
  sealed_names: SealedVector[];
};

const hex = (s: string): Buffer => Buffer.from(s, "hex");
const K = hex(v.names_key_hex);
const OTHER_NUMBER = "ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ";

test("the number wrap key and the wk1 vector reproduce byte for byte", () => {
  for (const input of [v.account_number, ...v.account_number_inputs]) {
    assert.equal(normalizeAccountNumber(input), v.account_number);
    assert.equal(numberWrapKey(input)?.toString("hex"), v.number_wrap_key_hex);
  }
  const wrapKey = numberWrapKey(v.account_number)!;
  assert.equal(v.name_key.aad, "nameKey");
  assert.equal(wrapNameKey(wrapKey, K, hex(v.name_key.nonce_hex)), v.name_key.sealed);
  assert.deepEqual(unwrapNameKey(wrapKey, v.name_key.sealed), K);
});

test("every zk2 vector seals and opens, only under its own device id", () => {
  for (const vec of v.sealed_names) {
    assert.equal(vec.aad, v.device_id);
    assert.equal(sealName(K, v.device_id, vec.plain!, hex(vec.nonce_hex)), vec.sealed);
    assert.equal(openName(K, v.device_id, vec.sealed), vec.plain);
    assert.equal(openName(K, v.device_id.toUpperCase(), vec.sealed), vec.plain);
    assert.equal(openName(K, v.other_device_id, vec.sealed), null);
  }
});

test("the server-side sn1 and ws1 vectors reproduce with the same primitives", () => {
  const cases = [
    {
      prefix: "sn1:",
      ikm: Buffer.from(v.names_secret_b64url, "base64url"),
      info: "seal:number:v1",
      keyHex: v.secret_seal_key_hex,
      plain: Buffer.from(v.account_number),
      vec: v.account_number_sealed
    },
    {
      prefix: "ws1:",
      ikm: Buffer.from(v.device_name_key),
      info: "wrap:server:v1",
      keyHex: v.server_wrap_key_hex,
      plain: K,
      vec: v.name_key_server
    }
  ];
  for (const c of cases) {
    const key = Buffer.from(hkdfSync("sha256", c.ikm, v.salt, c.info, 32));
    assert.equal(key.toString("hex"), c.keyHex);
    assert.equal(sealValue(c.prefix, key, c.plain, c.vec.aad, hex(c.vec.nonce_hex)), c.vec.sealed);
    assert.deepEqual(openValue(c.prefix, key, c.vec.sealed, c.vec.aad), c.plain);
  }
});

test("only an account number is a wrap key source, in any spelling", () => {
  for (const same of [" 0123 4567 89ab cdef ghjk mnpq\n", "o123-4567-89AB-CDEF-GHJK-MNPQ", "\uFEFF0123456789abcdefghjkmnpq\u00A0"]) {
    assert.equal(numberWrapKey(same)?.toString("hex"), v.number_wrap_key_hex);
  }
  assert.equal(normalizeAccountNumber("i1l1-oO0"), "1111000");
  for (const legacy of ["1234567890123456", "", null, "lk", "0123456789ABCDEFGHJKMNPU", "0123456789ABCDEFGHJKMNPQ0", "0123_4567_89AB_CDEF_GHJK_MNPQ", "0123456789ABCDEFGHJKMNP\u0085Q"]) {
    assert.equal(numberWrapKey(legacy), null);
  }
});

test("a fresh nonce every seal, and names and keys round-trip", () => {
  const k = newNameKey();
  assert.equal(k.length, 32);
  assert.notDeepEqual(k, newNameKey());
  const wrapKey = numberWrapKey(v.account_number)!;
  assert.deepEqual(unwrapNameKey(wrapKey, wrapNameKey(wrapKey, k)), k);
  const a = sealName(k, v.device_id, "Work Phone 😀");
  assert.notEqual(a, sealName(k, v.device_id, "Work Phone 😀"));
  assert.equal(openName(k, v.device_id, a), "Work Phone 😀");
});

test("anything unreadable opens to null", () => {
  const sealed = v.sealed_names[0].sealed;
  const body = sealed.slice(4);
  const tampered = sealed.slice(0, 20) + "A" + sealed.slice(21);
  for (const [key, stored] of [
    [numberWrapKey(v.account_number), sealed],
    [null, sealed],
    [K.subarray(0, 16), sealed],
    [K, tampered],
    [K, "Swift Fox"],
    [K, "enc1:" + body],
    [K, "zk1:" + body],
    [K, sealValue("zk2:", K, Buffer.alloc(0), v.device_id)],
    [K, "zk2:!!!"],
    [K, "zk2:AAECAwQFBgcICQoL"],
    [K, null],
    [K, 42]
  ] as const) {
    assert.equal(openName(key, v.device_id, stored), null);
  }
  assert.equal(unwrapNameKey(numberWrapKey(OTHER_NUMBER), v.name_key.sealed), null);
  assert.equal(unwrapNameKey(numberWrapKey(v.account_number), sealValue("wk1:", numberWrapKey(v.account_number)!, K.subarray(0, 16), "nameKey")), null);
});

test("device names are trimmed and checked like the mobile core", () => {
  assert.equal(checkDeviceName("  Work Phone \n"), "Work Phone");
  assert.equal(checkDeviceName("a".repeat(40)), "a".repeat(40));
  assert.equal(checkDeviceName("😀".repeat(20)), "😀".repeat(20));
  for (const bad of ["", "   ", "a".repeat(41), "😀".repeat(21), "a\u0000b", "a\u0085b", "Phone\u202Egpj.exe", 42]) {
    assert.throws(() => checkDeviceName(bad), /Device name must/);
  }
});

const row = (id: string, friendlyName: string | null, label: string | null, sealedName: string | null) =>
  ({ id, friendlyName, label, sealedName, createdAt: "2026-01-01", status: "active" });

const rows = (): HubDeviceList["devices"] => [
  { ...row(v.device_id, "Server Opened", "Swift Fox", v.sealed_names[0].sealed), identityPubkey: "pub" },
  row(v.other_device_id, "Server Opened", "Calm Owl", v.sealed_names[0].sealed),
  row("d3", "Legacy Name", null, null),
  row("d4", "", "", null),
  row("5e6d7a8b-9c0d-4b8a-9c2f-3f1c9a527d4e", null, null, null)
];

test("a names v2 list shows open(sealedName) ?? label ?? friendlyName ?? short id", () => {
  const fallback = ["Calm Owl", "Legacy Name", "d4", "5e6d7a8b"];
  const names = (credential: string | null, nameKey: string | null = v.name_key.sealed) =>
    shownDevices({ devices: rows(), nameKey }, credential).map((d) => d.friendlyName);
  assert.deepEqual(names(v.account_number_inputs[0]), ["Work laptop", ...fallback]);
  assert.deepEqual(names("lk"), ["Swift Fox", ...fallback]);
  assert.deepEqual(names(OTHER_NUMBER), ["Swift Fox", ...fallback]);
  assert.deepEqual(names(v.account_number, null), ["Swift Fox", ...fallback]);

  const shown = shownDevices({ devices: rows(), nameKey: v.name_key.sealed }, v.account_number)[0];
  assert.deepEqual(Object.keys(shown).sort(), ["createdAt", "friendlyName", "id", "identityPubkey", "status"]);
});

test("an old hub's list passes through untouched", () => {
  const shown = shownDevices({ devices: rows() }, v.account_number);
  assert.deepEqual(shown.map((d) => d.friendlyName), ["Server Opened", "Server Opened", "Legacy Name", "", null]);
});

test("sealRename uses the account's key, or creates one when the hub has none", () => {
  const withKey = { devices: [], nameKey: v.name_key.sealed };
  const noKey = { devices: [], nameKey: null };
  const existing = sealRename(withKey, v.account_number_inputs[1], v.device_id, "Work laptop");
  assert.deepEqual(Object.keys(existing), ["deviceId", "sealedName"]);
  assert.equal(openName(K, v.device_id, existing.sealedName), "Work laptop");

  const created = sealRename(noKey, v.account_number, v.device_id, "Work laptop");
  const k = unwrapNameKey(numberWrapKey(v.account_number), created.nameKey);
  assert.equal(openName(k, v.device_id, created.sealedName), "Work laptop");

  assert.throws(() => sealRename(noKey, "lk", v.device_id, "Phone"), { message: RENAME_NEEDS_ACCOUNT_NUMBER });
  assert.throws(() => sealRename(withKey, OTHER_NUMBER, v.device_id, "Phone"), { message: NAME_KEY_UNREADABLE });
});

interface Call {
  method: string;
  route: string;
  body?: unknown;
}

function fakeHub(lists: unknown[], renameErrors: Array<Error | undefined> = []) {
  const calls: Call[] = [];
  let listed = 0;
  const call: HubCall = async (method, route, body) => {
    calls.push({ method, route, body });
    if (route === "/api/device/list") return lists[Math.min(listed++, lists.length - 1)];
    const err = renameErrors.shift();
    if (err) throw err;
    return { ok: true };
  };
  const renames = () => calls.filter((c) => c.route === "/api/device/rename").map((c) => c.body as Record<string, string>);
  return { call, calls, renames };
}

const keyExists = () => new Error('Hub API error (409): {"error":"refused","code":"NAME_KEY_EXISTS"}');

test("rename on an old hub sends today's plaintext body", async () => {
  const hub = fakeHub([{ devices: [] }]);
  await renameDevice(hub.call, "lk", "d1", "  Work Phone \n");
  assert.deepEqual(hub.calls, [
    { method: "GET", route: "/api/device/list", body: undefined },
    { method: "POST", route: "/api/device/rename", body: { deviceId: "d1", friendlyName: "Work Phone" } }
  ]);
});

test("rename on a v2 hub sends only the sealed name", async () => {
  const hub = fakeHub([{ devices: [], nameKey: v.name_key.sealed }]);
  await renameDevice(hub.call, v.account_number_inputs[0], v.device_id, " Work Phone ");
  const [body] = hub.renames();
  assert.deepEqual(Object.keys(body), ["deviceId", "sealedName"]);
  assert.equal(openName(K, v.device_id, body.sealedName), "Work Phone");
  assert.ok(!JSON.stringify(hub.calls).includes("Work Phone"));
});

test("rename creates the key when the list said none", async () => {
  const hub = fakeHub([{ devices: [], nameKey: null }]);
  await renameDevice(hub.call, v.account_number, v.device_id, "Work Phone");
  const [body] = hub.renames();
  const k = unwrapNameKey(numberWrapKey(v.account_number), body.nameKey);
  assert.equal(openName(k, v.device_id, body.sealedName), "Work Phone");
});

test("a NAME_KEY_EXISTS conflict refetches the list and reseals once", async () => {
  const hub = fakeHub([{ devices: [], nameKey: null }, { devices: [], nameKey: v.name_key.sealed }], [keyExists()]);
  await renameDevice(hub.call, v.account_number, v.device_id, "Work Phone");
  const [first, retry] = hub.renames();
  assert.equal(hub.calls.length, 4);
  assert.ok(first.nameKey);
  assert.equal(retry.nameKey, undefined);
  assert.equal(openName(K, v.device_id, retry.sealedName), "Work Phone");
});

test("a second conflict is not retried", async () => {
  const hub = fakeHub([{ devices: [], nameKey: null }], [keyExists(), keyExists()]);
  await assert.rejects(renameDevice(hub.call, v.account_number, v.device_id, "Work Phone"), /NAME_KEY_EXISTS/);
  assert.equal(hub.renames().length, 2);
});

test("other rename errors are not retried", async () => {
  const hub = fakeHub([{ devices: [], nameKey: null }], [new Error("Hub API error (404): DEVICE_NOT_FOUND")]);
  await assert.rejects(renameDevice(hub.call, v.account_number, v.device_id, "Work Phone"), /DEVICE_NOT_FOUND/);
  assert.equal(hub.renames().length, 1);
});

test("a legacy token on a v2 hub is asked for the account number", async () => {
  const hub = fakeHub([{ devices: [], nameKey: v.name_key.sealed }]);
  await assert.rejects(renameDevice(hub.call, "lk", v.device_id, "Phone"), { message: RENAME_NEEDS_ACCOUNT_NUMBER });
  assert.equal(hub.renames().length, 0);
});

test("a bad name never reaches the hub", async () => {
  const hub = fakeHub([{ devices: [], nameKey: null }]);
  await assert.rejects(renameDevice(hub.call, v.account_number, v.device_id, "a".repeat(41)), /1-40 characters/);
  assert.equal(hub.calls.length, 0);
});
