import assert from "node:assert/strict";
import test from "node:test";
import {
  buildServerRetryOrder,
  groupRegions,
  loadOf,
  orderByRecent,
  pickNode,
  promoteRecent,
  regionKeyOf,
  regionLoad,
  regionOfServer
} from "./regions.ts";

const server = (id: string, name: string, load: number | null = 0, country = ""): ServerInfo => ({
  id,
  name,
  region: name,
  country,
  load,
  cloak: { remoteHost: `${id}.example`, uid: "uid", publicKey: "pk" }
});

test("regionKeyOf strips a trailing numeric suffix", () => {
  assert.equal(regionKeyOf(server("eu-central-1", "Amsterdam")), "eu-central");
  assert.equal(regionKeyOf(server("eu-central-12", "Amsterdam")), "eu-central");
});

test("regionKeyOf leaves ids without a numeric suffix alone", () => {
  assert.equal(regionKeyOf(server("amsterdam", "Amsterdam")), "amsterdam");
  assert.equal(regionKeyOf(server("eu-west-a", "London")), "eu-west-a");
});

test("groupRegions folds sibling nodes into one region, in hub order", () => {
  const regions = groupRegions([
    server("eu-central-1", "Amsterdam"),
    server("eu-west-1", "London"),
    server("eu-central-2", "Amsterdam")
  ]);

  assert.deepEqual(regions.map((r) => r.key), ["eu-central", "eu-west"]);
  assert.deepEqual(regions[0].nodes.map((n) => n.id), ["eu-central-1", "eu-central-2"]);
  assert.equal(regions[0].name, "Amsterdam");
  assert.equal(regions[1].nodes.length, 1);
});

test("groupRegions falls back to the key when a node has no name", () => {
  const [region] = groupRegions([server("eu-central-1", "")]);
  assert.equal(region.name, "eu-central");
});

test("pickNode returns the lowest-load node", () => {
  const [region] = groupRegions([
    server("eu-central-1", "Amsterdam", 61),
    server("eu-central-2", "Amsterdam", 22),
    server("eu-central-3", "Amsterdam", 47)
  ]);
  assert.equal(pickNode(region).id, "eu-central-2");
  assert.equal(regionLoad(region), 22);
});

test("a node reporting load beats one reporting none", () => {
  const [region] = groupRegions([
    server("eu-central-1", "Amsterdam", null),
    server("eu-central-2", "Amsterdam", 90)
  ]);
  assert.equal(pickNode(region).id, "eu-central-2");
});

test("regionLoad is null when no node in the region reports one", () => {
  const [region] = groupRegions([server("eu-west-1", "London", null)]);
  assert.equal(regionLoad(region), null);
  assert.equal(loadOf(region.nodes[0]), 100);
});

test("regionOfServer finds the region holding a node id", () => {
  const regions = groupRegions([
    server("eu-central-1", "Amsterdam"),
    server("us-east-1", "New York")
  ]);
  assert.equal(regionOfServer(regions, "us-east-1")?.key, "us-east");
  assert.equal(regionOfServer(regions, "nope-1"), undefined);
});

test("orderByRecent puts recents first and keeps the rest in hub order", () => {
  const regions = groupRegions([
    server("eu-central-1", "Amsterdam"),
    server("eu-west-1", "London"),
    server("us-east-1", "New York")
  ]);
  const ordered = orderByRecent(regions, ["us-east", "eu-west"]);
  assert.deepEqual(ordered.map((r) => r.key), ["us-east", "eu-west", "eu-central"]);
});

test("orderByRecent ignores recents that no longer exist", () => {
  const regions = groupRegions([server("eu-west-1", "London")]);
  assert.deepEqual(orderByRecent(regions, ["gone", "eu-west"]).map((r) => r.key), ["eu-west"]);
});

test("promoteRecent moves a key to the front without duplicating it", () => {
  assert.deepEqual(promoteRecent(["a", "b", "c"], "c"), ["c", "a", "b"]);
  assert.deepEqual(promoteRecent(["a", "b"], "z"), ["z", "a", "b"]);
});

test("promoteRecent caps the list", () => {
  assert.deepEqual(promoteRecent(["a", "b", "c"], "z", 2), ["z", "a"]);
});

test("buildServerRetryOrder exhausts the selected region before later regions", () => {
  const servers = [
    server("eu-central-1", "Amsterdam", 70),
    server("us-east-1", "New York", 5),
    server("eu-central-2", "Amsterdam", 20),
    server("eu-central-3", "Amsterdam", null),
    server("us-east-2", "New York", 10),
    server("ap-south-1", "Mumbai", 1)
  ];

  assert.deepEqual(buildServerRetryOrder(servers, "eu-central-1"), [
    "eu-central-1",
    "eu-central-2",
    "eu-central-3",
    "us-east-1",
    "us-east-2",
    "ap-south-1"
  ]);
  assert.deepEqual(servers.map((item) => item.id), [
    "eu-central-1",
    "us-east-1",
    "eu-central-2",
    "eu-central-3",
    "us-east-2",
    "ap-south-1"
  ]);
});

test("buildServerRetryOrder keeps the requested server when it is not in the snapshot", () => {
  assert.deepEqual(buildServerRetryOrder([server("eu-west-1", "London")], "gone-1"), ["gone-1"]);
});

test("buildServerRetryOrder continues with the next region and wraps around", () => {
  const servers = [
    server("eu-west-1", "London", 20),
    server("us-east-1", "New York", 30),
    server("ap-south-1", "Mumbai", 40)
  ];

  assert.deepEqual(buildServerRetryOrder(servers, "us-east-1"), [
    "us-east-1",
    "ap-south-1",
    "eu-west-1"
  ]);
});
