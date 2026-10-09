import assert from "node:assert/strict";
import test from "node:test";
import { isCachedServer, restoreCachedServers } from "./cachedServers.ts";

function server(overrides: Record<string, unknown> = {}) {
  return {
    id: "fra-1",
    name: "Frankfurt",
    region: "eu-central",
    country: "DE",
    cloak: { remoteHost: "1.2.3.4", uid: "uid-1", publicKey: "pk-1" },
    ...overrides
  };
}

test("a well-formed server is accepted", () => {
  assert.equal(isCachedServer(server()), true);
});

test("every field provision() cannot build a profile without is required", () => {
  for (const missing of ["id", "name", "region", "country"]) {
    const s = server();
    delete (s as Record<string, unknown>)[missing];
    assert.equal(isCachedServer(s), false, `${missing} must be required`);
  }
});

test("blank strings do not pass as present", () => {
  assert.equal(isCachedServer(server({ id: "   " })), false);
  assert.equal(isCachedServer(server({ name: "" })), false);
});

test("the cloak block and each of its fields are required", () => {
  assert.equal(isCachedServer(server({ cloak: undefined })), false);
  assert.equal(isCachedServer(server({ cloak: null })), false);
  assert.equal(isCachedServer(server({ cloak: "nope" })), false);
  assert.equal(isCachedServer(server({ cloak: { uid: "u", publicKey: "p" } })), false);
  assert.equal(isCachedServer(server({ cloak: { remoteHost: "h", publicKey: "p" } })), false);
  assert.equal(isCachedServer(server({ cloak: { remoteHost: "h", uid: "u" } })), false);
});

test("optional transport blocks pass through, but only as objects", () => {
  assert.equal(isCachedServer(server({ reality: { remotePort: 443 } })), true);
  assert.equal(isCachedServer(server({ shadowsocks: {} })), true);
  assert.equal(isCachedServer(server({ hysteria2: undefined })), true);
  assert.equal(isCachedServer(server({ naive: "nope" })), false);
  assert.equal(isCachedServer(server({ snowflake: [] })), false);
});

test("a null optional block passes through like an absent one", () => {
  assert.equal(isCachedServer(server({ naive: null })), true);
  assert.equal(isCachedServer(server({ reality: null, hysteria2: null })), true);
});

test("non-objects are rejected", () => {
  assert.equal(isCachedServer(undefined), false);
  assert.equal(isCachedServer(null), false);
  assert.equal(isCachedServer("fra-1"), false);
  assert.equal(isCachedServer([server()]), false);
});

test("restore keeps order, drops junk and deduplicates by id", () => {
  const restored = restoreCachedServers([
    server({ id: "b" }),
    "junk",
    server({ id: "a" }),
    server({ id: "b", name: "Duplicate" }),
    server({ cloak: null })
  ]);
  assert.deepEqual(restored.map((s) => s.id), ["b", "a"]);
  assert.equal(restored[0].name, "Frankfurt", "the first entry for an id wins");
});

test("restore tolerates an absent or non-array value", () => {
  assert.deepEqual(restoreCachedServers(undefined), []);
  assert.deepEqual(restoreCachedServers(null), []);
  assert.deepEqual(restoreCachedServers({}), []);
  assert.deepEqual(restoreCachedServers("nope"), []);
  assert.deepEqual(restoreCachedServers([]), []);
});
