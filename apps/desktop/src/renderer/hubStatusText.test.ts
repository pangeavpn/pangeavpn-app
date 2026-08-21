import assert from "node:assert/strict";
import test from "node:test";
import { hubActiveText, hubTestText, type Translate } from "./hubStatusText.ts";

// Echoes the key and params so the assertions pin both, not the English copy.
const t: Translate = (key, params) => (params ? `${key} ${JSON.stringify(params)}` : key);

test("no active method says so rather than naming one", () => {
  assert.equal(
    hubActiveText({ methods: FLAGS, active: null, detail: null }, t),
    "settings.provisioning.active.none"
  );
});

const FLAGS = { directIp: true, shadowsocks: true, fronted: true, normal: false };

test("an active method is named, with its address when there is one", () => {
  assert.equal(
    hubActiveText({ methods: FLAGS, active: "fronted", detail: "edge.example" }, t),
    'settings.provisioning.active.detail {"method":"settings.provisioning.hubFronted.title","detail":"edge.example"}'
  );
});

test("an active method with no address drops the address clause", () => {
  assert.equal(
    hubActiveText({ methods: FLAGS, active: "directIp", detail: null }, t),
    'settings.provisioning.active.using {"method":"settings.provisioning.directIp.title"}'
  );
});

test("a working method reports the round trip", () => {
  assert.equal(
    hubTestText({ method: "normal", ok: true, ms: 91 }, t),
    'settings.provisioning.result.ok {"ms":91}'
  );
  assert.equal(
    hubTestText({ method: "directIp", ok: true, detail: "203.0.113.7", ms: 42 }, t),
    'settings.provisioning.result.okDetail {"detail":"203.0.113.7","ms":42}'
  );
});

test("a failure reads as a failure", () => {
  assert.equal(
    hubTestText({ method: "normal", ok: false, ms: 5000 }, t),
    "settings.provisioning.result.fail"
  );
});

test("a method with nothing to try says why instead of blaming the network", () => {
  assert.equal(
    hubTestText({ method: "fronted", ok: false, unavailable: "noRelay", ms: 0 }, t),
    "settings.provisioning.result.noRelay"
  );
  assert.equal(
    hubTestText({ method: "shadowsocks", ok: false, unavailable: "noCredentials", ms: 0 }, t),
    "settings.provisioning.result.noCredentials"
  );
  assert.equal(
    hubTestText({ method: "directIp", ok: false, unavailable: "noAddress", ms: 0 }, t),
    "settings.provisioning.result.noAddress"
  );
  assert.equal(
    hubTestText({ method: "normal", ok: false, unavailable: "busy", ms: 0 }, t),
    "settings.provisioning.result.busy"
  );
});
