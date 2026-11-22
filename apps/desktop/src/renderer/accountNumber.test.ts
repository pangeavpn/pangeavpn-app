import test from "node:test";
import assert from "node:assert/strict";
import { formatAccountNumberInput, formatSignInCodeInput, normalizeAccountNumber } from "./accountNumber.ts";

test("legacy digit-only tokens pass through untouched", () => {
  assert.equal(normalizeAccountNumber("0123456789012345"), "0123456789012345");
  assert.equal(formatAccountNumberInput("0123456789012345"), "0123456789012345");
  assert.equal(formatAccountNumberInput("0123"), "0123");
});

test("normalize uppercases, strips separators and folds O/I/L", () => {
  assert.equal(
    normalizeAccountNumber("abcd-efgh jkmn-pqrs-tvwx-yz01"),
    "ABCDEFGHJKMNPQRSTVWXYZ01"
  );
  assert.equal(normalizeAccountNumber("oil"), "011");
  assert.equal(normalizeAccountNumber("  ABCD-EFGH  "), "ABCDEFGH");
});

test("normalize is idempotent", () => {
  const once = normalizeAccountNumber("abcd-efgh-jkmn-pqrs-tvwx-yz01");
  assert.equal(normalizeAccountNumber(once), once);
});

test("format groups in blocks of four with no trailing dash", () => {
  assert.equal(formatAccountNumberInput("abcdefghjkmnpqrstvwxyz01"), "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01");
  assert.equal(formatAccountNumberInput("abcd"), "ABCD");
  assert.equal(formatAccountNumberInput("abcde"), "ABCD-E");
});

test("format is stable when re-applied to its own output", () => {
  const once = formatAccountNumberInput("abcdefghjkmnpqrstvwxyz01");
  assert.equal(formatAccountNumberInput(once), once);
});

test("format does not reject over-long or unexpected characters", () => {
  assert.equal(formatAccountNumberInput("abcdefghjkmnpqrstvwxyz0123456"), "ABCD-EFGH-JKMN-PQRS-TVWX-YZ01-2345-6");
  assert.equal(normalizeAccountNumber("ab!cd"), "AB!CD");
});

test("empty input stays empty", () => {
  assert.equal(normalizeAccountNumber(""), "");
  assert.equal(formatAccountNumberInput(""), "");
});

test("the browser sign-in code is shown as XXXXX-XXXXX with Crockford folding", () => {
  assert.equal(formatSignInCodeInput("7k3mq"), "7K3MQ");
  assert.equal(formatSignInCodeInput("7k3mq9xa2p"), "7K3MQ-9XA2P");
  assert.equal(formatSignInCodeInput("oil00 11111 22"), "01100-11111");
  assert.equal(formatSignInCodeInput("7K3MQ-9U!A"), "7K3MQ-9A");
});
