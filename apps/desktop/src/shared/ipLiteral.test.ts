import assert from "node:assert/strict";
import test from "node:test";
import { isIPv4Literal, isIPv6Literal, isIpLiteral } from "./ipLiteral.ts";

test("isIPv4Literal accepts dotted quads", () => {
  for (const host of ["1.2.3.4", "0.0.0.0", "255.255.255.255", "203.0.113.4"]) {
    assert.equal(isIPv4Literal(host), true, host);
  }
});

test("isIPv4Literal rejects leading zeros", () => {
  for (const host of ["010.1.1.1", "1.02.3.4", "00.1.2.3", "1.2.3.04"]) {
    assert.equal(isIPv4Literal(host), false, host);
  }
});

test("isIPv4Literal rejects out-of-range octets", () => {
  for (const host of ["256.1.1.1", "1.999.1.1", "300.300.300.300"]) {
    assert.equal(isIPv4Literal(host), false, host);
  }
});

test("isIPv4Literal rejects things that are not dotted quads", () => {
  for (const host of ["", "example.com", "1.2.3", "1.2.3.4.5", "1.2.3.4 ", "::1", "1.2.3.-4"]) {
    assert.equal(isIPv4Literal(host), false, JSON.stringify(host));
  }
});

test("isIPv6Literal accepts literals with and without brackets", () => {
  for (const host of ["::1", "[::1]", "2001:db8::1", "fe80::1", "2001:0db8:0000:0000:0000:0000:0000:0001"]) {
    assert.equal(isIPv6Literal(host), true, host);
  }
});

test("isIPv6Literal rejects non-IPv6 input", () => {
  for (const host of ["", "1.2.3.4", "example.com", "2001::db8::1", "gggg::1"]) {
    assert.equal(isIPv6Literal(host), false, JSON.stringify(host));
  }
});

test("isIpLiteral accepts either family", () => {
  assert.equal(isIpLiteral("1.2.3.4"), true);
  assert.equal(isIpLiteral("::1"), true);
  assert.equal(isIpLiteral("example.com"), false);
});
