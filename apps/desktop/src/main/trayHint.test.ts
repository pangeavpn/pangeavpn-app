import test from "node:test";
import assert from "node:assert/strict";
import { shouldShowTrayHint, trayHintBodyKey } from "./trayHint.ts";

const eligible = { alreadyShown: false, fromTrayClick: false, supported: true };

test("shows on the first hide of a fresh install", () => {
  assert.equal(shouldShowTrayHint(eligible), true);
});

test("never shows twice", () => {
  assert.equal(shouldShowTrayHint({ ...eligible, alreadyShown: true }), false);
});

test("stays quiet when the user hid the window from the tray icon", () => {
  assert.equal(shouldShowTrayHint({ ...eligible, fromTrayClick: true }), false);
});

test("stays quiet where the OS has no notifications", () => {
  assert.equal(shouldShowTrayHint({ ...eligible, supported: false }), false);
});

test("picks menu bar wording on macOS and tray wording elsewhere", () => {
  assert.equal(trayHintBodyKey("darwin"), "notify.menuBarBody");
  assert.equal(trayHintBodyKey("win32"), "notify.trayBody");
  assert.equal(trayHintBodyKey("linux"), "notify.trayBody");
});
