import assert from "node:assert/strict";
import test from "node:test";
import { planRotation, recordRotation } from "./serverRotation.ts";

// The retry order as regions.ts builds it: the current server, its region's
// siblings, then later regions in hub order.
const order = ["eu-west-2", "eu-west-1", "us-east-1", "ap-southeast-1"];

test("planRotation follows the retry order, so the current region's sibling comes first", () => {
  const { plan } = planRotation(order, "eu-west-2", []);
  assert.deepEqual(plan, ["eu-west-1", "us-east-1", "ap-southeast-1"]);
});

test("planRotation never includes the server being left", () => {
  const { plan } = planRotation(order, "eu-west-2", []);
  assert.ok(!plan.includes("eu-west-2"));
});

test("planRotation moves servers already visited this cycle behind the fresh ones", () => {
  const { plan } = planRotation(order, "eu-west-2", ["eu-west-1"]);
  assert.deepEqual(plan, ["us-east-1", "ap-southeast-1", "eu-west-1"]);
});

test("planRotation starts the cycle over once every alternative has been visited", () => {
  const { plan, visited } = planRotation(order, "eu-west-2", ["eu-west-1", "us-east-1", "ap-southeast-1"]);
  assert.deepEqual(visited, []);
  assert.deepEqual(plan, ["eu-west-1", "us-east-1", "ap-southeast-1"]);
});

test("planRotation drops ids that are no longer listed so a gone server cannot block the reset", () => {
  const { plan, visited } = planRotation(order, "eu-west-2", ["gone-1", "eu-west-1"]);
  assert.deepEqual(visited, ["eu-west-1"]);
  assert.deepEqual(plan, ["us-east-1", "ap-southeast-1", "eu-west-1"]);
});

test("planRotation returns an empty plan when there is nowhere else to go", () => {
  const { plan } = planRotation(["eu-west-1"], "eu-west-1", []);
  assert.deepEqual(plan, []);
});

test("recordRotation remembers the server left, the failed attempts and the landing", () => {
  const visited = recordRotation([], "eu-west-2", ["eu-west-1", "us-east-1", "ap-southeast-1"], "us-east-1");
  assert.deepEqual(visited, ["eu-west-2", "eu-west-1", "us-east-1"]);
});

test("recordRotation remembers every attempt when none of them connected", () => {
  const visited = recordRotation([], "eu-west-2", ["eu-west-1", "us-east-1"], null);
  assert.deepEqual(visited, ["eu-west-2", "eu-west-1", "us-east-1"]);
});

test("recordRotation keeps earlier history and does not repeat ids", () => {
  const visited = recordRotation(["us-east-1"], "eu-west-2", ["eu-west-1", "us-east-1"], "us-east-1");
  assert.deepEqual(visited, ["us-east-1", "eu-west-2", "eu-west-1"]);
});
