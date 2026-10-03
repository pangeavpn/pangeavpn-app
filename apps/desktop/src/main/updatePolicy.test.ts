import assert from "node:assert/strict";
import test from "node:test";
import { compareVersions, isPrerelease, shouldOfferUpdate } from "./updatePolicy.ts";

test("a release candidate sorts below its final release and above the previous one", () => {
  assert.ok(compareVersions("0.8.0", "0.8.0-rc.1") > 0);
  assert.ok(compareVersions("0.8.0-rc.1", "0.8.0") < 0);
  assert.ok(compareVersions("0.8.0-rc.1", "0.7.5") > 0);
  assert.equal(compareVersions("v0.8.0", "0.8.0"), 0);
  assert.equal(compareVersions("0.8.0-rc.1", "0.8.0-rc.1"), 0);
});

test("numeric prerelease parts compare as numbers", () => {
  assert.ok(compareVersions("0.8.0-rc.10", "0.8.0-rc.9") > 0);
  assert.ok(compareVersions("0.8.0-rc.2", "0.8.0-rc.1") > 0);
  assert.ok(compareVersions("0.8.0-rc.1.1", "0.8.0-rc.1") > 0);
  assert.ok(compareVersions("0.8.0-rc.1", "0.8.0-beta.3") > 0);
  assert.ok(compareVersions("0.8.0-1", "0.8.0-alpha") < 0);
});

test("core versions still compare numerically", () => {
  assert.ok(compareVersions("0.10.0", "0.9.9") > 0);
  assert.ok(compareVersions("1.0", "0.99.99") > 0);
  assert.equal(compareVersions("0.8", "0.8.0"), 0);
});

test("prerelease detection", () => {
  assert.equal(isPrerelease("0.8.0-rc.1"), true);
  assert.equal(isPrerelease("v0.8.0-rc.1"), true);
  assert.equal(isPrerelease("0.8.0"), false);
  assert.equal(isPrerelease("0.8.0+build.5"), false);
});

test("a candidate install is offered its own final release", () => {
  assert.equal(shouldOfferUpdate({ version: "0.8.0" }, "0.8.0-rc.1"), true);
  assert.equal(shouldOfferUpdate({ version: "0.8.1" }, "0.8.0-rc.1"), true);
});

test("a candidate install is not offered an older stable release", () => {
  assert.equal(shouldOfferUpdate({ version: "0.7.5" }, "0.8.0-rc.1"), false);
});

test("a candidate install may move to a newer candidate", () => {
  assert.equal(shouldOfferUpdate({ version: "0.8.0-rc.2", prerelease: true }, "0.8.0-rc.1"), true);
  assert.equal(shouldOfferUpdate({ version: "0.8.0-rc.1", prerelease: true }, "0.8.0-rc.2"), false);
});

test("a stable install is never offered a release candidate", () => {
  assert.equal(shouldOfferUpdate({ version: "0.8.0-rc.1" }, "0.7.5"), false);
  assert.equal(shouldOfferUpdate({ version: "0.8.0-rc.1", prerelease: true }, "0.7.5"), false);
  assert.equal(shouldOfferUpdate({ version: "0.9.0", prerelease: true }, "0.8.0"), false);
});

test("a stable install is offered newer stable releases only", () => {
  assert.equal(shouldOfferUpdate({ version: "0.8.0" }, "0.7.5"), true);
  assert.equal(shouldOfferUpdate({ version: "0.7.5" }, "0.7.5"), false);
  assert.equal(shouldOfferUpdate({ version: "0.7.4" }, "0.7.5"), false);
});
