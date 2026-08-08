import assert from "node:assert/strict";
import test from "node:test";
import {
  DEFAULT_HUB_METHODS,
  applyHubMethod,
  enabledHubMethods,
  isHubMethod,
  normalizeHubMethods
} from "./hubMethods.ts";

test("direct IP is the only method enabled by default", () => {
  assert.deepEqual(DEFAULT_HUB_METHODS, { directIp: true, shadowsocks: false, normal: false });
  assert.deepEqual(enabledHubMethods(DEFAULT_HUB_METHODS), ["directIp"]);
});

test("enabledHubMethods reports attempt order, not object order", () => {
  const all = { directIp: true, shadowsocks: true, normal: true };
  assert.deepEqual(enabledHubMethods(all), ["directIp", "shadowsocks", "normal"]);
});

test("turning a method on works from the default", () => {
  const { methods, applied } = applyHubMethod(DEFAULT_HUB_METHODS, "shadowsocks", true);
  assert.equal(applied, true);
  assert.deepEqual(methods, { directIp: true, shadowsocks: true, normal: false });
});

test("turning off the last enabled method is refused", () => {
  const { methods, applied } = applyHubMethod(DEFAULT_HUB_METHODS, "directIp", false);
  assert.equal(applied, false, "the app would have no way to reach the hub");
  assert.deepEqual(methods, DEFAULT_HUB_METHODS, "state must be left untouched");
});

test("any single remaining method is protected, not just direct IP", () => {
  const onlySs = { directIp: false, shadowsocks: true, normal: false };
  assert.equal(applyHubMethod(onlySs, "shadowsocks", false).applied, false);

  const onlyNormal = { directIp: false, shadowsocks: false, normal: true };
  assert.equal(applyHubMethod(onlyNormal, "normal", false).applied, false);
});

test("a method can be turned off while another is still on", () => {
  const two = { directIp: true, shadowsocks: true, normal: false };
  const { methods, applied } = applyHubMethod(two, "directIp", false);
  assert.equal(applied, true);
  assert.deepEqual(methods, { directIp: false, shadowsocks: true, normal: false });
});

test("re-applying the value a method already has is a no-op, never a refusal", () => {
  const { methods, applied } = applyHubMethod(DEFAULT_HUB_METHODS, "directIp", true);
  assert.equal(applied, true, "setting the last method to its current value must not report failure");
  assert.deepEqual(methods, DEFAULT_HUB_METHODS);

  const off = applyHubMethod(DEFAULT_HUB_METHODS, "shadowsocks", false);
  assert.equal(off.applied, true);
});

test("isHubMethod rejects anything not a known method", () => {
  assert.equal(isHubMethod("directIp"), true);
  assert.equal(isHubMethod("shadowsocks"), true);
  assert.equal(isHubMethod("normal"), true);
  assert.equal(isHubMethod("doh"), false);
  assert.equal(isHubMethod(""), false);
  assert.equal(isHubMethod(undefined), false);
  assert.equal(isHubMethod(2), false);
});

test("normalizeHubMethods falls back to the default for missing or junk input", () => {
  assert.deepEqual(normalizeHubMethods(undefined), DEFAULT_HUB_METHODS);
  assert.deepEqual(normalizeHubMethods({}), DEFAULT_HUB_METHODS);
  assert.deepEqual(normalizeHubMethods({ nonsense: 1 }), DEFAULT_HUB_METHODS);
});

test("normalizeHubMethods reads an explicit stored shape", () => {
  assert.deepEqual(normalizeHubMethods({ directIp: false, shadowsocks: true, normal: true }), {
    directIp: false,
    shadowsocks: true,
    normal: true
  });
});

test("normalizeHubMethods rescues a hand-edited all-off file", () => {
  assert.deepEqual(normalizeHubMethods({ directIp: false, shadowsocks: false, normal: false }), {
    directIp: true,
    shadowsocks: false,
    normal: false
  });
});

test("normalizeHubMethods treats non-boolean values as off, not as present", () => {
  assert.deepEqual(normalizeHubMethods({ directIp: "yes", shadowsocks: 1, normal: null }), {
    directIp: true,
    shadowsocks: false,
    normal: false
  });
});

test("migrates the old directIpOnly default (no domain) to normal off", () => {
  assert.deepEqual(normalizeHubMethods({ directIpEnabled: true, directIpOnly: true }), {
    directIp: true,
    shadowsocks: false,
    normal: false
  });
});

test("migrates a user who had allowed the normal domain path", () => {
  assert.deepEqual(normalizeHubMethods({ directIpEnabled: true, directIpOnly: false }), {
    directIp: true,
    shadowsocks: false,
    normal: true
  });
});

test("migrates a user who had turned direct IP off", () => {
  assert.deepEqual(normalizeHubMethods({ directIpEnabled: false, directIpOnly: false }), {
    directIp: false,
    shadowsocks: false,
    normal: true
  });
});

test("migration never yields an unusable all-off state", () => {
  // directIp off and directIpOnly still on would leave nothing enabled.
  assert.deepEqual(normalizeHubMethods({ directIpEnabled: false, directIpOnly: true }), {
    directIp: true,
    shadowsocks: false,
    normal: false
  });
});
