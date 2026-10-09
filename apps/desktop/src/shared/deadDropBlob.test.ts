import assert from "node:assert/strict";
import test from "node:test";
import { generateKeyPairSync, sign } from "node:crypto";
import { verifyDeadDropBlob, type DeadDropKeys } from "./deadDropBlob.ts";

function makeKey(): { pub: string; priv: ReturnType<typeof generateKeyPairSync>["privateKey"] } {
  const { publicKey, privateKey } = generateKeyPairSync("ed25519");
  const pub = (publicKey.export({ type: "spki", format: "der" }) as Buffer).subarray(12).toString("base64");
  return { pub, priv: privateKey };
}

const ACTIVE = makeKey();
const RESERVE = makeKey();
const STRANGER = makeKey();

const KEYS: DeadDropKeys = { active: ACTIVE.pub, reserve: RESERVE.pub };
const NOW = Date.parse("2026-08-21T00:00:00Z");

function payloadJson(overrides: Record<string, unknown> = {}): string {
  return JSON.stringify({
    v: 1,
    seq: 7,
    issued: "2026-08-01T00:00:00Z",
    expires: "2026-11-19T00:00:00Z",
    hubIps: ["203.0.113.4"],
    frontedEndpoints: ["reserve-a.example.com"],
    ...overrides
  });
}

function blob(
  json: string,
  signer: typeof ACTIVE = ACTIVE,
  keyName = "active"
): string {
  const bytes = Buffer.from(json, "utf8");
  return JSON.stringify({
    payload: bytes.toString("base64"),
    sig: sign(null, bytes, signer.priv).toString("base64"),
    key: keyName
  });
}

const opts = { keys: KEYS, minSeq: 0, nowMs: NOW };

test("accepts a well-formed blob signed by the active key", () => {
  const result = verifyDeadDropBlob(blob(payloadJson()), opts);
  assert.deepEqual(result, {
    seq: 7,
    hubIps: ["203.0.113.4"],
    frontedEndpoints: ["reserve-a.example.com"]
  });
});

test("accepts a blob signed by the reserve key", () => {
  const result = verifyDeadDropBlob(blob(payloadJson(), RESERVE, "reserve"), opts);
  assert.equal(result?.seq, 7);
});

test("the key field is a hint, not an instruction", () => {
  // Signed by reserve but labelled active: the signature is what decides.
  assert.equal(verifyDeadDropBlob(blob(payloadJson(), RESERVE, "active"), opts)?.seq, 7);
  assert.equal(verifyDeadDropBlob(blob(payloadJson(), ACTIVE, "nonsense"), opts)?.seq, 7);
});

test("rejects a signature from an unrelated key", () => {
  assert.equal(verifyDeadDropBlob(blob(payloadJson(), STRANGER), opts), null);
});

test("rejects a tampered payload", () => {
  const good = JSON.parse(blob(payloadJson())) as Record<string, string>;
  const tampered = JSON.parse(payloadJson()) as Record<string, unknown>;
  tampered.hubIps = ["198.51.100.9"];
  good.payload = Buffer.from(JSON.stringify(tampered), "utf8").toString("base64");
  assert.equal(verifyDeadDropBlob(JSON.stringify(good), opts), null);
});

test("rejects a replayed or equal seq", () => {
  assert.equal(verifyDeadDropBlob(blob(payloadJson({ seq: 7 })), { ...opts, minSeq: 7 }), null);
  assert.equal(verifyDeadDropBlob(blob(payloadJson({ seq: 6 })), { ...opts, minSeq: 7 }), null);
  assert.equal(verifyDeadDropBlob(blob(payloadJson({ seq: 8 })), { ...opts, minSeq: 7 })?.seq, 8);
});

test("rejects a non-integer or negative seq", () => {
  for (const seq of [1.5, -1, "7", null]) {
    assert.equal(verifyDeadDropBlob(blob(payloadJson({ seq })), opts), null, String(seq));
  }
});

test("rejects an expired blob", () => {
  const expired = blob(payloadJson({ expires: "2026-08-20T23:59:59Z" }));
  assert.equal(verifyDeadDropBlob(expired, opts), null);
});

test("rejects a missing or unparseable expires", () => {
  for (const expires of [undefined, "", "not a date", 12345]) {
    assert.equal(verifyDeadDropBlob(blob(payloadJson({ expires })), opts), null, String(expires));
  }
});

test("ignores an unknown version", () => {
  assert.equal(verifyDeadDropBlob(blob(payloadJson({ v: 2 })), opts), null);
  assert.equal(verifyDeadDropBlob(blob(payloadJson({ v: "1" })), opts), null);
});

test("drops invalid entries individually", () => {
  const result = verifyDeadDropBlob(
    blob(
      payloadJson({
        hubIps: ["203.0.113.4", "010.1.1.1", "256.1.1.1", "example.com", 42],
        frontedEndpoints: ["reserve-a.example.com", "no-dot", "", "UPPER.example.com"]
      })
    ),
    opts
  );
  assert.deepEqual(result?.hubIps, ["203.0.113.4"]);
  assert.deepEqual(result?.frontedEndpoints, ["reserve-a.example.com", "upper.example.com"]);
});

test("deduplicates entries", () => {
  const result = verifyDeadDropBlob(
    blob(
      payloadJson({
        hubIps: ["203.0.113.4", "203.0.113.4"],
        frontedEndpoints: ["a.example.com", "A.example.com"]
      })
    ),
    opts
  );
  assert.deepEqual(result?.hubIps, ["203.0.113.4"]);
  assert.deepEqual(result?.frontedEndpoints, ["a.example.com"]);
});

test("a blob with nothing usable is treated as no blob", () => {
  const empty = blob(payloadJson({ hubIps: [], frontedEndpoints: [] }));
  assert.equal(verifyDeadDropBlob(empty, opts), null);
  const allJunk = blob(payloadJson({ hubIps: ["999.1.1.1"], frontedEndpoints: ["no-dot"] }));
  assert.equal(verifyDeadDropBlob(allJunk, opts), null);
});

test("tolerates unknown extra fields in a v1 payload", () => {
  assert.equal(verifyDeadDropBlob(blob(payloadJson({ somethingNew: { a: 1 } })), opts)?.seq, 7);
});

test("rejects malformed input without throwing", () => {
  const cases = [
    "",
    "not json",
    "[]",
    "null",
    "42",
    JSON.stringify({}),
    JSON.stringify({ payload: "***", sig: "***", key: "active" }),
    JSON.stringify({ payload: Buffer.from("{}").toString("base64"), key: "active" }),
    JSON.stringify({ payload: 5, sig: 5, key: 5 }),
    blob("not json at all"),
    blob("[]"),
    blob("null")
  ];
  for (const raw of cases) {
    assert.equal(verifyDeadDropBlob(raw, opts), null, JSON.stringify(raw).slice(0, 40));
  }
});

test("rejects non-canonical base64 rather than silently accepting it", () => {
  const parsed = JSON.parse(blob(payloadJson())) as Record<string, string>;
  parsed.payload = parsed.payload.replace(/=+$/, "") + "===";
  assert.equal(verifyDeadDropBlob(JSON.stringify(parsed), opts), null);
});

test("rejects an oversized blob before parsing it", () => {
  const huge = JSON.stringify({ payload: "A".repeat(200_000), sig: "A", key: "active" });
  assert.equal(verifyDeadDropBlob(huge, opts), null);
});

test("accepts a Buffer as well as a string", () => {
  const raw = Buffer.from(blob(payloadJson()), "utf8");
  assert.equal(verifyDeadDropBlob(raw, opts)?.seq, 7);
});
