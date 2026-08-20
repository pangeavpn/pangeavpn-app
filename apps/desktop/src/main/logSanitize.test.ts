import test from "node:test";
import assert from "node:assert/strict";
import { sanitizeLog } from "./logSanitize.ts";

test("removes CR/LF so a message cannot forge extra log lines", () => {
  assert.equal(sanitizeLog("real\nforged: ok"), "realforged: ok");
  assert.equal(sanitizeLog("real\r\nforged"), "realforged");
  assert.match(sanitizeLog("a\nb\r\nc"), /^[^\r\n]*$/);
});

test("blanks other control characters, including terminal escapes", () => {
  assert.equal(sanitizeLog("clear\x1b[2Jscreen"), "clear [2Jscreen");
  assert.equal(sanitizeLog("nul\x00byte"), "nul byte");
  assert.equal(sanitizeLog("del\x7fchar"), "del char");
});

test("leaves ordinary text untouched", () => {
  assert.equal(sanitizeLog("connect failed: timeout (503)"), "connect failed: timeout (503)");
});

test("redacts bearer tokens", () => {
  assert.equal(sanitizeLog("Token login failed (401): Bearer abc123.def-GHI"), "Token login failed (401): Bearer [redacted]");
});

test("redacts X-License-Key header values", () => {
  assert.equal(sanitizeLog('rejected header X-License-Key: ABCD-1234-secret'), "rejected header X-License-Key: [redacted]");
});

test("redacts 64-hex daemon tokens", () => {
  const hex = "a".repeat(64);
  assert.equal(sanitizeLog(`daemon token ${hex} rejected`), "daemon token [redacted] rejected");
});

test("redacts WireGuard-shaped base64 keys", () => {
  const key = "A".repeat(42) + "B=";
  assert.equal(sanitizeLog(`bad key ${key}`), "bad key [redacted]");
});

test("redacts password/token/uuid fields in JSON bodies", () => {
  assert.equal(
    sanitizeLog('{"password":"hunter2","uuid":"11111111-2222-3333-4444-555555555555"}'),
    '{"password":"[redacted]","uuid":"[redacted]"}'
  );
});

test("does not throw on a symbol value", () => {
  assert.doesNotThrow(() => sanitizeLog(Symbol("x")));
});

test("unwraps Error messages and stringifies everything else", () => {
  assert.equal(sanitizeLog(new Error("boom\nsecond line")), "boomsecond line");
  assert.equal(sanitizeLog(null), "null");
  assert.equal(sanitizeLog(undefined), "undefined");
  assert.equal(sanitizeLog(42), "42");
  assert.equal(sanitizeLog({ a: 1 }), "[object Object]");
});
