import assert from "node:assert/strict";
import test from "node:test";
import { isSafeExternalUrl } from "./externalUrl.ts";

test("our own hosts and their subdomains are allowed", () => {
  assert.equal(isSafeExternalUrl("https://pangeavpn.org"), true);
  assert.equal(isSafeExternalUrl("https://pangeavpn.org/app"), true);
  assert.equal(isSafeExternalUrl("https://www.pangeavpn.it/x"), true);
  assert.equal(isSafeExternalUrl("https://github.com/org/repo/releases"), true);
});

test("a lookalike host that merely ends in our name is refused", () => {
  assert.equal(isSafeExternalUrl("https://evilpangeavpn.org"), false);
  assert.equal(isSafeExternalUrl("https://pangeavpn.org.attacker.com"), false);
});

test("anything that is not plain http(s) is refused", () => {
  assert.equal(isSafeExternalUrl("file:///C:/Windows/System32/cmd.exe"), false);
  assert.equal(isSafeExternalUrl("javascript:alert(1)"), false);
  assert.equal(isSafeExternalUrl("ms-settings:"), false);
  assert.equal(isSafeExternalUrl("\\attacker\share"), false);
});

test("junk is refused rather than thrown on", () => {
  assert.equal(isSafeExternalUrl(""), false);
  assert.equal(isSafeExternalUrl(undefined), false);
  assert.equal(isSafeExternalUrl(42), false);
  assert.equal(isSafeExternalUrl("not a url"), false);
});
