import { test, beforeEach } from "node:test";
import assert from "node:assert/strict";
import {
  beginAttempt,
  cancelAttempt,
  commitAttempt,
  endAttempt,
  hasActiveAttempt,
  isCancelled,
  resetAttemptsForTest
} from "./connectAttempt.ts";

beforeEach(() => resetAttemptsForTest());

test("a fresh attempt is not cancelled", () => {
  const attempt = beginAttempt();
  assert.equal(isCancelled(attempt), false);
  assert.equal(hasActiveAttempt(), true);
});

test("cancelling marks the attempt and aborts its signal", () => {
  const attempt = beginAttempt();
  assert.equal(attempt.controller.signal.aborted, false);

  const cancelled = cancelAttempt();

  assert.equal(cancelled, attempt);
  assert.equal(isCancelled(attempt), true);
  // This is what stops in-flight hub requests rather than waiting on them.
  assert.equal(attempt.controller.signal.aborted, true);
});

test("an abort listener never sees a live attempt whose requests are dead", () => {
  const attempt = beginAttempt();
  let cancelledWhenAborted: boolean | null = null;
  attempt.controller.signal.addEventListener("abort", () => {
    cancelledWhenAborted = isCancelled(attempt);
  });

  cancelAttempt();

  assert.equal(cancelledWhenAborted, true);
});

test("cancelling with nothing in flight is a no-op", () => {
  // The user pressing Stop on an idle app must not tear down a live tunnel.
  assert.equal(cancelAttempt(), null);
  assert.equal(hasActiveAttempt(), false);
});

test("cancelling twice reports only the first", () => {
  const attempt = beginAttempt();
  assert.equal(cancelAttempt(), attempt);
  assert.equal(cancelAttempt(), null);
});

test("a completed attempt is not cancellable — Stop can't kill a live connection", () => {
  const attempt = beginAttempt();
  endAttempt(attempt);

  assert.equal(cancelAttempt(), null);
  assert.equal(hasActiveAttempt(), false);
});

test("a superseded attempt reports cancelled so its connect is skipped", () => {
  // Two Connect presses in a row: the first must not bring a tunnel up after
  // the second has taken over.
  const first = beginAttempt();
  const second = beginAttempt();

  assert.equal(isCancelled(first), true);
  assert.equal(first.controller.signal.aborted, true);
  assert.equal(isCancelled(second), false);
});

test("a stale attempt cannot clear the current one", () => {
  const first = beginAttempt();
  const second = beginAttempt();

  endAttempt(first); // late completion of the superseded attempt

  assert.equal(isCancelled(second), false, "the live attempt must survive");
  assert.equal(hasActiveAttempt(), true);
});

test("a stale cancel cannot kill a newer attempt", () => {
  const first = beginAttempt();
  endAttempt(first);
  const second = beginAttempt();

  // Whatever the first attempt does on its way out, the second stays live.
  endAttempt(first);

  assert.equal(isCancelled(second), false);
  assert.equal(hasActiveAttempt(), true);
});

test("a committed attempt can no longer be cancelled — Stop can't tear down a connection that just landed", () => {
  const attempt = beginAttempt();
  commitAttempt(attempt);

  assert.equal(cancelAttempt(), null);
  assert.equal(isCancelled(attempt), false);
  assert.equal(attempt.controller.signal.aborted, false);
});

test("committing a stale attempt does not affect the current one", () => {
  const first = beginAttempt();
  const second = beginAttempt();
  commitAttempt(first); // late commit of the superseded attempt

  assert.equal(cancelAttempt(), second);
  assert.equal(isCancelled(second), true);
});

test("ids are monotonic so a later attempt is always distinguishable", () => {
  const first = beginAttempt();
  endAttempt(first);
  const second = beginAttempt();

  assert.ok(second.id > first.id);
});
