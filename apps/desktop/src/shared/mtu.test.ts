import test from "node:test";
import assert from "node:assert/strict";
import { MTU_DEFAULT, MTU_MAX, MTU_MIN, normalizeMtu, normalizeMtuOrDefault } from "./mtu.ts";

test("the shipped default sits inside the accepted range", () => {
  assert.ok(MTU_MIN <= MTU_DEFAULT && MTU_DEFAULT <= MTU_MAX);
});

test("accepts numbers across the range", () => {
  assert.equal(normalizeMtu(MTU_MIN), MTU_MIN);
  assert.equal(normalizeMtu(MTU_DEFAULT), MTU_DEFAULT);
  assert.equal(normalizeMtu(MTU_MAX), MTU_MAX);
  assert.equal(normalizeMtu(1350), 1350);
});

test("rejects values outside the range", () => {
  assert.equal(normalizeMtu(MTU_MIN - 1), null);
  assert.equal(normalizeMtu(MTU_MAX + 1), null);
  assert.equal(normalizeMtu(0), null);
  assert.equal(normalizeMtu(-1380), null);
  assert.equal(normalizeMtu(9000), null);
});

test("rejects non-integers", () => {
  assert.equal(normalizeMtu(1380.5), null);
  assert.equal(normalizeMtu("1380.5"), null);
});

test("accepts numeric strings, trimming whitespace", () => {
  assert.equal(normalizeMtu("1380"), 1380);
  assert.equal(normalizeMtu("  1400  "), 1400);
});

test("rejects empty and non-numeric strings", () => {
  assert.equal(normalizeMtu(""), null);
  assert.equal(normalizeMtu("   "), null);
  assert.equal(normalizeMtu("abc"), null);
  assert.equal(normalizeMtu("1380abc"), null);
});

test("rejects non-finite numbers", () => {
  assert.equal(normalizeMtu(Number.NaN), null);
  assert.equal(normalizeMtu(Number.POSITIVE_INFINITY), null);
  assert.equal(normalizeMtu(Number.NEGATIVE_INFINITY), null);
});

test("rejects types that are not numbers or strings", () => {
  // settings.json is user-writable, and IPC input is untrusted.
  assert.equal(normalizeMtu(null), null);
  assert.equal(normalizeMtu(undefined), null);
  assert.equal(normalizeMtu(true), null);
  assert.equal(normalizeMtu({}), null);
  assert.equal(normalizeMtu([]), null);
  assert.equal(normalizeMtu([1380]), null);
});

test("falls back to the default for anything unusable", () => {
  assert.equal(normalizeMtuOrDefault(undefined), MTU_DEFAULT);
  assert.equal(normalizeMtuOrDefault("nonsense"), MTU_DEFAULT);
  assert.equal(normalizeMtuOrDefault(9000), MTU_DEFAULT);
  assert.equal(normalizeMtuOrDefault(1400), 1400);
});
