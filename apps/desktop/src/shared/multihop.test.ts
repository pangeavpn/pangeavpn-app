import assert from "node:assert/strict";
import test from "node:test";
import { entryCandidates, normalizeEntryServer, normalizeMultihopPrefs, resolveEntry } from "./multihop.ts";

const servers = [
  { id: "eu-west-1", load: 40, multihop: true },
  { id: "eu-west-2", load: 10, multihop: true },
  { id: "us-east-1", load: 20, multihop: true },
  { id: "eu-central-2", load: 5 },
  { id: "ap-south-1", load: null, multihop: true }
];

test("entryCandidates keeps entry-capable nodes outside the exit's region, lightest first", () => {
  assert.deepEqual(entryCandidates(servers, "eu-west-1").map((s) => s.id), ["us-east-1", "ap-south-1"]);
});

test("resolveEntry honours a usable choice and otherwise takes the lightest", () => {
  assert.equal(resolveEntry(servers, "us-east-1", "eu-west-1")?.id, "eu-west-1");
  assert.equal(resolveEntry(servers, "us-east-1", null)?.id, "eu-west-2");
  // A choice inside the exit's region is not a hop, so auto takes over.
  assert.equal(resolveEntry(servers, "eu-west-1", "eu-west-2")?.id, "us-east-1");
  assert.equal(resolveEntry([{ id: "x-1", multihop: true }], "x-2", null), null);
});

test("normalizeMultihopPrefs tolerates hand-edited settings", () => {
  assert.deepEqual(normalizeMultihopPrefs(undefined), { enabled: false, entryServerId: null });
  assert.deepEqual(normalizeMultihopPrefs({ enabled: "yes", entryServerId: 3 }), { enabled: false, entryServerId: null });
  assert.deepEqual(normalizeMultihopPrefs({ enabled: true, entryServerId: " eu-west-1 " }), {
    enabled: true,
    entryServerId: "eu-west-1"
  });
});

test("normalizeEntryServer accepts absent and rejects junk", () => {
  assert.equal(normalizeEntryServer(undefined), null);
  assert.equal(normalizeEntryServer(""), null);
  assert.equal(normalizeEntryServer(" us-east-1 "), "us-east-1");
  assert.throws(() => normalizeEntryServer(42));
  assert.throws(() => normalizeEntryServer("   "));
});
