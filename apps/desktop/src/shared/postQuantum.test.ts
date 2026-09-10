import assert from "node:assert/strict";
import test from "node:test";
import { PQ_ALGORITHM, parsePostQuantumAnswer } from "./postQuantum.ts";

const offer = { id: "abc", algorithm: PQ_ALGORITHM, kemPublicKey: "ek" };

test("parsePostQuantumAnswer is null without an offer or without an answer", () => {
  assert.equal(parsePostQuantumAnswer(null, { algorithm: PQ_ALGORITHM, kemCiphertext: "ct" }), null);
  assert.equal(parsePostQuantumAnswer(offer, undefined), null);
  assert.equal(parsePostQuantumAnswer(offer, null), null);
});

test("parsePostQuantumAnswer returns the answer the node gave", () => {
  assert.deepEqual(parsePostQuantumAnswer(offer, { algorithm: PQ_ALGORITHM, kemCiphertext: "ct", extra: 1 }), {
    algorithm: PQ_ALGORITHM,
    kemCiphertext: "ct"
  });
});

test("parsePostQuantumAnswer refuses a block it cannot finish", () => {
  assert.throws(() => parsePostQuantumAnswer(offer, "yes"), /malformed/);
  assert.throws(() => parsePostQuantumAnswer(offer, {}), /without a ciphertext/);
  assert.throws(() => parsePostQuantumAnswer(offer, { algorithm: PQ_ALGORITHM, kemCiphertext: "" }), /without a ciphertext/);
  assert.throws(() => parsePostQuantumAnswer(offer, { algorithm: "ml-kem-1024", kemCiphertext: "ct" }), /ml-kem-1024/);
  assert.throws(() => parsePostQuantumAnswer(offer, { kemCiphertext: "ct" }), /undefined, not ml-kem-768/);
});
