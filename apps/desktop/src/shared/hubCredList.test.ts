import assert from "node:assert/strict";
import test from "node:test";
import { fastestWorking } from "./hubCredList.ts";

const after = <T>(ms: number, value: T): Promise<T> => new Promise((resolve) => setTimeout(() => resolve(value), ms));

test("fastestWorking starts every attempt before any settles", async () => {
  const started: string[] = [];
  const won = await fastestWorking(["slow", "fast"], (name) => {
    started.push(name);
    return after(name === "slow" ? 40 : 5, name);
  });
  assert.deepEqual(started, ["slow", "fast"]);
  assert.deepEqual(won, { value: "fast", index: 1 });
});

test("fastestWorking waits past early failures for a later answer", async () => {
  const won = await fastestWorking([1, 2, 3], async (n) => {
    if (n === 1) throw new Error("dropped");
    if (n === 2) return after(5, null);
    return after(20, n * 10);
  });
  assert.deepEqual(won, { value: 30, index: 2 });
});

test("fastestWorking settles null once every attempt fails", async () => {
  const won = await fastestWorking(["a", "b"], (name) => {
    if (name === "a") throw new Error("sync throw");
    return after(5, null);
  });
  assert.equal(won, null);
});

test("fastestWorking settles null for an empty list", async () => {
  assert.equal(await fastestWorking([], async () => 1), null);
});

test("fastestWorking never starts a staggered attempt once an earlier one answers", async () => {
  const started: string[] = [];
  const won = await fastestWorking(
    ["first", "second", "third"],
    (name) => {
      started.push(name);
      return after(5, name);
    },
    50
  );
  assert.deepEqual(won, { value: "first", index: 0 });
  await after(120, null);
  assert.deepEqual(started, ["first"]);
});

test("fastestWorking overlaps a staggered attempt with one that hangs", async () => {
  const startedAt: number[] = [];
  const t0 = Date.now();
  const won = await fastestWorking(
    [0, 1],
    (n) => {
      startedAt.push(Date.now() - t0);
      return n === 0 ? new Promise<number | null>(() => undefined) : after(5, 1);
    },
    30
  );
  assert.deepEqual(won, { value: 1, index: 1 });
  assert.ok(startedAt[1] >= 25 && startedAt[1] < 200, `second start at ${startedAt[1]}ms`);
});

test("fastestWorking does not wait for a hung attempt once one answers", async () => {
  const hung = new Promise<string | null>(() => undefined);
  const won = await fastestWorking(["hung", "ok"], (name) => (name === "hung" ? hung : after(5, "ok")));
  assert.deepEqual(won, { value: "ok", index: 1 });
});
