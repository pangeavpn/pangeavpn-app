import assert from "node:assert/strict";
import test from "node:test";
import { entryCandidates, resolveEntry } from "./multihop.ts";

function server(id: string, load: number | null, multihop = false): ServerInfo {
  return { id, name: id, region: id, country: "", load, ...(multihop ? { multihop } : {}) };
}

const servers = [
  server("eu-west-1", 40, true),
  server("eu-west-2", 10, true),
  server("us-east-1", 20, true),
  server("eu-central-2", 5),
  server("ap-south-1", null, true)
];

test("entryCandidates keeps entry-capable nodes outside the exit's region, lightest first", () => {
  assert.deepEqual(entryCandidates(servers, "eu-west-1").map((s) => s.id), ["us-east-1", "ap-south-1"]);
  assert.deepEqual(entryCandidates(servers, "").map((s) => s.id), ["eu-west-2", "us-east-1", "eu-west-1", "ap-south-1"]);
});

test("resolveEntry honours a usable choice and otherwise takes the lightest", () => {
  assert.equal(resolveEntry(servers, "us-east-1", "eu-west-1")?.id, "eu-west-1");
  assert.equal(resolveEntry(servers, "us-east-1", null)?.id, "eu-west-2");
  // A choice inside the exit's region is not a hop, so auto takes over.
  assert.equal(resolveEntry(servers, "eu-west-1", "eu-west-2")?.id, "us-east-1");
  // A choice that vanished from the list falls back rather than failing.
  assert.equal(resolveEntry(servers, "us-east-1", "gone-1")?.id, "eu-west-2");
  assert.equal(resolveEntry([server("x-1", 1, true)], "x-2", null), null);
});
