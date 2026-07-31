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

test("unwraps Error messages and stringifies everything else", () => {
  assert.equal(sanitizeLog(new Error("boom\nsecond line")), "boomsecond line");
  assert.equal(sanitizeLog(null), "null");
  assert.equal(sanitizeLog(undefined), "undefined");
  assert.equal(sanitizeLog(42), "42");
  assert.equal(sanitizeLog({ a: 1 }), "[object Object]");
});
