import assert from "node:assert/strict";
import test from "node:test";
import { buildServerRetryOrder, replaceManagedProfile, runServerFallback } from "./serverFallback.ts";

class RetryableError extends Error {}

test("runServerFallback returns immediately when the first server succeeds", async () => {
  const attempted: string[] = [];
  const result = await runServerFallback(
    ["a", "b"],
    async (serverId) => {
      attempted.push(serverId);
      return `${serverId}-profile`;
    },
    (error) => error instanceof RetryableError
  );

  assert.deepEqual(result, { serverId: "a", value: "a-profile" });
  assert.deepEqual(attempted, ["a"]);
});

test("runServerFallback advances only after a retryable failure", async () => {
  const attempted: string[] = [];
  const result = await runServerFallback(
    ["a", "b", "c"],
    async (serverId, index) => {
      attempted.push(serverId);
      if (index < 2) throw new RetryableError(serverId);
      return serverId;
    },
    (error) => error instanceof RetryableError
  );

  assert.deepEqual(result, { serverId: "c", value: "c" });
  assert.deepEqual(attempted, ["a", "b", "c"]);
});

test("runServerFallback does not hide non-retryable failures", async () => {
  const failure = new Error("provision failed");
  const attempted: string[] = [];

  await assert.rejects(
    runServerFallback(
      ["a", "b"],
      async (serverId) => {
        attempted.push(serverId);
        throw failure;
      },
      (error) => error instanceof RetryableError
    ),
    (error) => error === failure
  );
  assert.deepEqual(attempted, ["a"]);
});

test("runServerFallback reports the final retryable failure after exhausting the plan", async () => {
  const last = new RetryableError("last");

  await assert.rejects(
    runServerFallback(
      ["a", "b"],
      async (_serverId, index) => {
        throw index === 0 ? new RetryableError("first") : last;
      },
      (error) => error instanceof RetryableError
    ),
    (error) => error === last
  );
});

test("runServerFallback rejects an empty retry plan", async () => {
  await assert.rejects(
    runServerFallback([], async () => "unused", () => true),
    /at least one server/i
  );
});

test("buildServerRetryOrder keeps siblings together before cycling regions", () => {
  const servers = [
    { id: "eu-west-1", load: 20 },
    { id: "us-east-1", load: 80 },
    { id: "us-east-2", load: 10 },
    { id: "ap-south-1", load: 30 }
  ];

  assert.deepEqual(buildServerRetryOrder(servers, "us-east-1"), [
    "us-east-1",
    "us-east-2",
    "ap-south-1",
    "eu-west-1"
  ]);
});

test("buildServerRetryOrder falls back to the full healthy set when the persisted server's region is gone", () => {
  const servers = [
    { id: "eu-west-1", load: 20 },
    { id: "us-east-1", load: 80 },
    { id: "us-east-2", load: 10 }
  ];

  assert.deepEqual(buildServerRetryOrder(servers, "ap-south-1"), ["eu-west-1", "us-east-2", "us-east-1"]);
});

test("buildServerRetryOrder dedupes a hub response that repeats a node id", () => {
  const servers = [
    { id: "us-east-1", load: 80 },
    { id: "us-east-2", load: 10 },
    { id: "us-east-1", load: 80 }
  ];

  assert.deepEqual(buildServerRetryOrder(servers, "us-east-1"), ["us-east-1", "us-east-2"]);
});

test("replaceManagedProfile preserves unrelated profiles and installs only the winner", () => {
  const previous = { id: "auto-old" };
  const unrelated = { id: "manual" };
  const winner = { id: "auto-new" };

  assert.deepEqual(
    replaceManagedProfile([previous, unrelated, { id: "auto-new" }], previous.id, winner),
    [unrelated, winner]
  );
});
