import assert from "node:assert/strict";
import tls from "node:tls";
import test from "node:test";
import { userTrustedRootsPem } from "./userTrustedRoots.ts";

const pem = (body: string): string => `-----BEGIN CERTIFICATE-----\n${body}\n-----END CERTIFICATE-----\n`;

test("userTrustedRootsPem keeps only the roots Node does not bundle", () => {
  const school = pem("U0NIT09M");
  const read = (store: "bundled" | "system"): string[] =>
    store === "bundled" ? [pem("UFVCTElD")] : [pem("UFVC\r\nTElD"), school];
  assert.equal(userTrustedRootsPem(read), school);
});

test("userTrustedRootsPem returns nothing when the OS store cannot be read", () => {
  assert.equal(
    userTrustedRootsPem(() => {
      throw new Error("no system store");
    }),
    ""
  );
});

test("userTrustedRootsPem reads this machine's real stores", () => {
  const bundle = userTrustedRootsPem();
  const bundled = new Set(tls.getCACertificates("bundled"));
  assert.ok(!bundle.split("\n-----BEGIN").some((p, i) => bundled.has(i === 0 ? p : `-----BEGIN${p}`)));
});
