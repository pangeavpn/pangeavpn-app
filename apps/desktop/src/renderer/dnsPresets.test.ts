import test from "node:test";
import assert from "node:assert/strict";
import { DNS_PRESETS, dnsChoiceFor, dnsServersFor } from "./dnsPresets.ts";

test("ships known IPv4 pairs for each DNS provider", () => {
  assert.deepEqual(DNS_PRESETS.adguard, ["94.140.14.14", "94.140.15.15"]);
  assert.deepEqual(DNS_PRESETS.cloudflare, ["1.1.1.1", "1.0.0.1"]);
  assert.deepEqual(DNS_PRESETS.quad9, ["9.9.9.9", "149.112.112.112"]);
  assert.deepEqual(DNS_PRESETS.cloudflareFamily, ["1.1.1.3", "1.0.0.3"]);
  assert.deepEqual(DNS_PRESETS.google, ["8.8.8.8", "8.8.4.4"]);
});

test("recognizes automatic, preset, and custom DNS values", () => {
  assert.equal(dnsChoiceFor([]), "automatic");
  assert.equal(dnsChoiceFor(DNS_PRESETS.adguard), "adguard");
  assert.equal(dnsChoiceFor(DNS_PRESETS.cloudflareFamily), "cloudflareFamily");
  assert.equal(dnsChoiceFor(["192.0.2.1"]), "custom");
});

test("returns independent address lists for selectable presets", () => {
  assert.deepEqual(dnsServersFor("automatic"), []);
  assert.deepEqual(dnsServersFor("quad9"), ["9.9.9.9", "149.112.112.112"]);
  assert.equal(dnsServersFor("custom"), null);

  const addresses = dnsServersFor("cloudflare");
  addresses?.pop();
  assert.deepEqual(DNS_PRESETS.cloudflare, ["1.1.1.1", "1.0.0.1"]);
});
