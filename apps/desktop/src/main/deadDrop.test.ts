import assert from "node:assert/strict";
import test from "node:test";
import { generateKeyPairSync, sign } from "node:crypto";
import {
  DEAD_DROP_MIN_INTERVAL_MS,
  DEAD_DROP_URLS,
  deadDropDue,
  fetchDeadDropPayload
} from "./deadDrop.ts";
import type { DeadDropKeys } from "../shared/deadDropBlob.ts";

const { publicKey, privateKey } = generateKeyPairSync("ed25519");
const PUB = (publicKey.export({ type: "spki", format: "der" }) as Buffer).subarray(12).toString("base64");
const KEYS: DeadDropKeys = { active: PUB, reserve: PUB };
const NOW = Date.parse("2026-08-21T00:00:00Z");

function blob(seq: number, hubIp = "203.0.113.4"): string {
  const bytes = Buffer.from(
    JSON.stringify({
      v: 1,
      seq,
      issued: "2026-08-01T00:00:00Z",
      expires: "2026-11-19T00:00:00Z",
      hubIps: [hubIp],
      frontedEndpoints: []
    }),
    "utf8"
  );
  return JSON.stringify({
    payload: bytes.toString("base64"),
    sig: sign(null, bytes, privateKey).toString("base64"),
    key: "active"
  });
}

const base = { keys: KEYS, minSeq: 0, nowMs: NOW };

test("a client that has never fetched is due", () => {
  assert.equal(deadDropDue(0, NOW), true);
});

test("a fetch inside the window is not due", () => {
  assert.equal(deadDropDue(NOW - 1000, NOW), false);
  assert.equal(deadDropDue(NOW - DEAD_DROP_MIN_INTERVAL_MS + 1, NOW), false);
});

test("a fetch older than the window is due again", () => {
  assert.equal(deadDropDue(NOW - DEAD_DROP_MIN_INTERVAL_MS, NOW), true);
  assert.equal(deadDropDue(NOW - DEAD_DROP_MIN_INTERVAL_MS * 5, NOW), true);
});

test("a clock that jumped backwards does not lock out fetching forever", () => {
  assert.equal(deadDropDue(NOW + DEAD_DROP_MIN_INTERVAL_MS * 100, NOW), true);
});

test("the shipped url list is https and non-empty", () => {
  assert.ok(DEAD_DROP_URLS.length >= 2);
  for (const url of DEAD_DROP_URLS) {
    assert.ok(url.startsWith("https://"), url);
  }
});

test("returns the payload from the first host that answers", async () => {
  const seen: string[] = [];
  const result = await fetchDeadDropPayload({
    ...base,
    urls: ["https://a.example.com/b.json", "https://b.example.com/b.json"],
    fetchText: async (url) => {
      seen.push(url);
      return blob(7);
    }
  });
  assert.equal(result?.payload.seq, 7);
  assert.equal(result?.url, "https://a.example.com/b.json");
  assert.deepEqual(seen, ["https://a.example.com/b.json"]);
});

test("falls over to the next host when one cannot be fetched", async () => {
  const seen: string[] = [];
  const result = await fetchDeadDropPayload({
    ...base,
    urls: ["https://dead.example.com/b.json", "https://live.example.com/b.json"],
    fetchText: async (url) => {
      seen.push(url);
      return url.includes("dead") ? null : blob(3);
    }
  });
  assert.equal(result?.payload.seq, 3);
  assert.equal(result?.url, "https://live.example.com/b.json");
  assert.equal(seen.length, 2);
});

test("falls over when a host serves a blob that fails verification", async () => {
  const result = await fetchDeadDropPayload({
    ...base,
    urls: ["https://hostile.example.com/b.json", "https://live.example.com/b.json"],
    fetchText: async (url) => (url.includes("hostile") ? "{\"payload\":\"AAAA\",\"sig\":\"AAAA\"}" : blob(4))
  });
  assert.equal(result?.payload.seq, 4);
});

test("a host that throws does not end the search", async () => {
  const result = await fetchDeadDropPayload({
    ...base,
    urls: ["https://boom.example.com/b.json", "https://live.example.com/b.json"],
    fetchText: async (url) => {
      if (url.includes("boom")) throw new Error("connection reset");
      return blob(5);
    }
  });
  assert.equal(result?.payload.seq, 5);
});

test("returns null when no host yields an acceptable blob", async () => {
  const result = await fetchDeadDropPayload({
    ...base,
    urls: ["https://a.example.com/b.json", "https://b.example.com/b.json"],
    fetchText: async () => null
  });
  assert.equal(result, null);
});

test("a replayed blob is rejected even when the host serves it happily", async () => {
  const result = await fetchDeadDropPayload({
    ...base,
    minSeq: 9,
    urls: ["https://a.example.com/b.json"],
    fetchText: async () => blob(9)
  });
  assert.equal(result, null);
});

test("non-https urls are never fetched", async () => {
  let called = false;
  const result = await fetchDeadDropPayload({
    ...base,
    urls: ["http://plain.example.com/b.json"],
    fetchText: async () => {
      called = true;
      return blob(7);
    }
  });
  assert.equal(called, false);
  assert.equal(result, null);
});
