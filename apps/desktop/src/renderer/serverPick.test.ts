import test from "node:test";
import assert from "node:assert/strict";
import { PREFERRED_MAX_LOAD, pickRandomServer, resolveSelection } from "./serverPick.ts";

type Candidate = { id: string; load?: number | null };

const s = (id: string, load?: number | null): Candidate => ({ id, load });

// Picks element `i` of whatever pool the function settles on.
const at = (i: number, poolSize: number) => (): number => i / poolSize;

test("returns null for an empty list", () => {
  assert.equal(pickRandomServer([]), null);
});

test("returns the only server when there is one", () => {
  assert.equal(pickRandomServer([s("a", 10)], at(0, 1))?.id, "a");
});

test("picks only from servers at or below the load threshold", () => {
  const servers = [s("busy", 90), s("light", 10), s("alsoBusy", 99)];
  // Whatever the roll, a congested server can never come back.
  for (let i = 0; i < 10; i++) {
    assert.equal(pickRandomServer(servers, () => i / 10)?.id, "light");
  }
});

test("treats the threshold as inclusive", () => {
  const picked = pickRandomServer([s("busy", 76), s("edge", PREFERRED_MAX_LOAD)], () => 0);
  assert.equal(picked?.id, "edge");
});

test("falls back to the full list when every server is congested", () => {
  const servers = [s("a", 90), s("b", 99)];
  assert.equal(pickRandomServer(servers, at(0, 2))?.id, "a");
  assert.equal(pickRandomServer(servers, at(1, 2))?.id, "b");
});

test("falls back to the full list when no server reports load (older hub)", () => {
  const servers = [s("a"), s("b")];
  assert.equal(pickRandomServer(servers, at(0, 2))?.id, "a");
  assert.equal(pickRandomServer(servers, at(1, 2))?.id, "b");
});

test("treats null, undefined and non-finite load as unknown rather than zero", () => {
  // If unknown load were coerced to 0 these would all be 'eligible' and would
  // beat the genuinely light server; the fallback must not fire at all here.
  const servers = [s("nul", null), s("undef"), s("nan", Number.NaN), s("inf", Number.POSITIVE_INFINITY), s("light", 5)];
  for (let i = 0; i < 10; i++) {
    assert.equal(pickRandomServer(servers, () => i / 10)?.id, "light");
  }
});

test("every eligible server is reachable and congested ones never are", () => {
  const servers = [s("a", 10), s("busy", 80), s("b", 20), s("c", 30)];
  const seen = new Set<string>();
  for (let i = 0; i < 3; i++) seen.add(pickRandomServer(servers, at(i, 3))!.id);
  assert.deepEqual([...seen].sort(), ["a", "b", "c"]);
});

test("distributes uniformly across the eligible pool", () => {
  const servers = [s("a", 10), s("b", 20), s("c", 30)];
  const counts: Record<string, number> = { a: 0, b: 0, c: 0 };
  for (let i = 0; i < 300; i++) {
    counts[pickRandomServer(servers, () => (i % 300) / 300)!.id] += 1;
  }
  assert.deepEqual(counts, { a: 100, b: 100, c: 100 });
});

test("never returns undefined when random returns values at the top of its range", () => {
  // Math.random() is [0,1) but guard the boundary anyway — a rounding slip here
  // would hand the caller undefined and crash the connect path.
  const servers = [s("a", 10), s("b", 20)];
  assert.notEqual(pickRandomServer(servers, () => 0.999999999999), null);
  assert.notEqual(pickRandomServer(servers, () => 1), null);
});

test("does not mutate the input list", () => {
  const servers = [s("a", 90), s("b", 10)];
  const before = servers.map((x) => x.id);
  pickRandomServer(servers, () => 0);
  assert.deepEqual(servers.map((x) => x.id), before);
});

const listed = [{ id: "a" }, { id: "b" }, { id: "c" }];

test("keeps the in-session pick when it is still listed", () => {
  assert.equal(resolveSelection(listed, "b", "c"), "b");
});

test("restores the last connected server when there is no in-session pick", () => {
  assert.equal(resolveSelection(listed, "", "c"), "c");
});

test("restores the last connected server when the in-session pick has gone", () => {
  assert.equal(resolveSelection(listed, "removed", "a"), "a");
});

test("selects nothing when neither the in-session pick nor the last server is listed", () => {
  assert.equal(resolveSelection(listed, "gone", "alsoGone"), "");
});

test("selects nothing on a fresh install with no last server", () => {
  assert.equal(resolveSelection(listed, "", null), "");
});

test("selects nothing when the list is empty", () => {
  assert.equal(resolveSelection([], "a", "b"), "");
});

test("selects nothing when the last server is filtered out by transport", () => {
  // User's last server doesn't support the chosen transport, so it isn't visible.
  assert.equal(resolveSelection([{ id: "a" }], "", "needsOtherTransport"), "");
});
