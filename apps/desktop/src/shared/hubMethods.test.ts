import assert from "node:assert/strict";
import test from "node:test";
import {
  DEFAULT_HUB_METHODS,
  HUB_METHODS_REV,
  applyHubMethod,
  enabledHubMethods,
  isHubMethod,
  normalizeHubMethods,
  persistableHubMethods
} from "./hubMethods.ts";

const off = { directIp: false, reality: false, shadowsocks: false, fronted: false, normal: false };

test("every method except the cleartext-domain one is enabled by default", () => {
  assert.deepEqual(DEFAULT_HUB_METHODS, {
    directIp: true,
    reality: true,
    shadowsocks: true,
    fronted: true,
    normal: false
  });
  assert.deepEqual(enabledHubMethods(DEFAULT_HUB_METHODS), ["directIp", "reality", "shadowsocks", "fronted"]);
});

test("enabledHubMethods reports attempt order, not object order", () => {
  const all = { normal: true, fronted: true, shadowsocks: true, reality: true, directIp: true };
  assert.deepEqual(enabledHubMethods(all), ["directIp", "reality", "shadowsocks", "fronted", "normal"]);
});

test("turning a method on works from the default", () => {
  const { methods, applied } = applyHubMethod(DEFAULT_HUB_METHODS, "normal", true);
  assert.equal(applied, true);
  assert.deepEqual(methods, { directIp: true, reality: true, shadowsocks: true, fronted: true, normal: true });
});

test("turning off the last enabled method is refused", () => {
  const onlyDirect = { ...off, directIp: true };
  const { methods, applied } = applyHubMethod(onlyDirect, "directIp", false);
  assert.equal(applied, false, "the app would have no way to reach the hub");
  assert.deepEqual(methods, onlyDirect, "state must be left untouched");
});

test("any single remaining method is protected, not just direct IP", () => {
  for (const method of ["reality", "shadowsocks", "fronted", "normal"] as const) {
    assert.equal(applyHubMethod({ ...off, [method]: true }, method, false).applied, false, method);
  }
});

test("a method can be turned off while another is still on", () => {
  const { methods, applied } = applyHubMethod(DEFAULT_HUB_METHODS, "reality", false);
  assert.equal(applied, true);
  assert.deepEqual(methods, {
    directIp: true,
    reality: false,
    shadowsocks: true,
    fronted: true,
    normal: false
  });
});

test("re-applying the value a method already has is a no-op, never a refusal", () => {
  const onlyDirect = { ...off, directIp: true };
  const { methods, applied } = applyHubMethod(onlyDirect, "directIp", true);
  assert.equal(applied, true, "setting the last method to its current value must not report failure");
  assert.deepEqual(methods, onlyDirect);

  assert.equal(applyHubMethod(onlyDirect, "normal", false).applied, true);
});

test("isHubMethod rejects anything not a known method", () => {
  for (const method of ["directIp", "reality", "shadowsocks", "fronted", "normal"]) {
    assert.equal(isHubMethod(method), true, method);
  }
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

test("normalizeHubMethods reads an explicit stored shape at the current rev", () => {
  assert.deepEqual(
    normalizeHubMethods({
      directIp: false,
      reality: false,
      shadowsocks: true,
      fronted: false,
      normal: true,
      rev: HUB_METHODS_REV
    }),
    { directIp: false, reality: false, shadowsocks: true, fronted: false, normal: true }
  );
});

test("normalizeHubMethods rescues a hand-edited all-off file", () => {
  assert.deepEqual(
    normalizeHubMethods({ ...off, rev: HUB_METHODS_REV }),
    DEFAULT_HUB_METHODS,
    "directIp alone cannot produce a request without a cached IP or DoH"
  );
});

test("normalizeHubMethods treats non-boolean values as off, not as present", () => {
  assert.deepEqual(
    normalizeHubMethods({ directIp: "yes", reality: "on", shadowsocks: 1, fronted: {}, normal: null }),
    DEFAULT_HUB_METHODS,
    "nothing boolean means nothing explicit was stored, so the defaults apply"
  );
});

test("an install stored before any rev inherits every newly defaulted-on method", () => {
  // What every install written before the first bump looks like: shadowsocks
  // off because that was the old default, not because anyone chose it.
  assert.deepEqual(normalizeHubMethods({ directIp: true, shadowsocks: false, normal: false }), {
    directIp: true,
    reality: true,
    shadowsocks: true,
    fronted: true,
    normal: false
  });
});

test("the rev bump does not touch methods whose default did not change", () => {
  assert.deepEqual(normalizeHubMethods({ directIp: false, shadowsocks: false, normal: true }), {
    directIp: false,
    reality: true,
    shadowsocks: true,
    fronted: true,
    normal: true
  });
});

// A rev-1 file already applied the rev-1 defaults, so its offs were chosen.
test("a rev-1 install gains reality but keeps what it switched off at rev 1", () => {
  assert.deepEqual(
    normalizeHubMethods({ directIp: true, shadowsocks: false, fronted: false, normal: false, rev: 1 }),
    { directIp: true, reality: true, shadowsocks: false, fronted: false, normal: false }
  );
});

test("a deliberate off at the current rev survives, unlike a pre-rev one", () => {
  const chosen = { ...off, directIp: true, rev: HUB_METHODS_REV };
  assert.deepEqual(normalizeHubMethods(chosen), { ...off, directIp: true });
});

test("persistableHubMethods stamps the rev so the bump applies exactly once", () => {
  const stored = persistableHubMethods({ ...off, directIp: true });
  assert.equal(stored.rev, HUB_METHODS_REV);
  assert.deepEqual(normalizeHubMethods(stored), { ...off, directIp: true });
});

test("migrates the old directIpOnly default, adopting the newer methods' defaults", () => {
  assert.deepEqual(normalizeHubMethods({ directIpEnabled: true, directIpOnly: true }), {
    directIp: true,
    reality: true,
    shadowsocks: true,
    fronted: true,
    normal: false
  });
});

test("migrates a user who had allowed the normal domain path", () => {
  assert.deepEqual(normalizeHubMethods({ directIpEnabled: true, directIpOnly: false }), {
    directIp: true,
    reality: true,
    shadowsocks: true,
    fronted: true,
    normal: true
  });
});

test("migrates a user who had turned direct IP off", () => {
  assert.deepEqual(normalizeHubMethods({ directIpEnabled: false, directIpOnly: false }), {
    directIp: false,
    reality: true,
    shadowsocks: true,
    fronted: true,
    normal: true
  });
});

test("migration never yields an unusable all-off state", () => {
  assert.deepEqual(normalizeHubMethods({ ...off, rev: HUB_METHODS_REV }), DEFAULT_HUB_METHODS);
});
