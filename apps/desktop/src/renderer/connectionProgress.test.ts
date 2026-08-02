import assert from "node:assert/strict";
import test from "node:test";
import { scheduleConnectionMessages } from "./connectionProgress.ts";

test("scheduleConnectionMessages schedules all reassurance messages from the operation start", () => {
  const scheduled: Array<{ callback: () => void; delay: number; id: number }> = [];
  const shown: string[] = [];
  const clear = scheduleConnectionMessages(
    ["three", "eight", "fifteen"],
    (message) => shown.push(message),
    (callback, delay) => {
      const id = scheduled.length + 1;
      scheduled.push({ callback, delay, id });
      return id;
    },
    () => {}
  );

  assert.deepEqual(scheduled.map(({ delay }) => delay), [3000, 8000, 15000]);
  scheduled.forEach(({ callback }) => callback());
  assert.deepEqual(shown, ["three", "eight", "fifteen"]);
  clear();
});

test("scheduleConnectionMessages clears every pending timer", () => {
  const cleared: unknown[] = [];
  const clear = scheduleConnectionMessages(
    ["three", "eight", "fifteen"],
    () => {},
    (_callback, delay) => delay,
    (timer) => cleared.push(timer)
  );

  clear();
  assert.deepEqual(cleared, [3000, 8000, 15000]);
});
