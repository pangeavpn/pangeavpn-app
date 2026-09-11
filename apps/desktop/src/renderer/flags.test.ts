import assert from "node:assert/strict";
import test from "node:test";
import { hasFlag } from "./flags.ts";

test("every country the hub serves or is about to serve has a drawn flag", () => {
  for (const code of ["GB", "US", "NL", "PL", "CH"]) {
    assert.ok(hasFlag(code), `${code} falls back to the globe`);
  }
});

test("hasFlag normalises case and whitespace and rejects unknown codes", () => {
  assert.ok(hasFlag(" pl "));
  assert.equal(hasFlag(""), false);
  assert.equal(hasFlag("ZZ"), false);
});
