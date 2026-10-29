import assert from "node:assert/strict";
import test from "node:test";
import { expectedDigest, macPackageAsset } from "./macUpdatePackage.ts";

test("the asset is the release's pkg for this Mac's architecture", () => {
  assert.deepEqual(macPackageAsset("0.8.2", "arm64"), {
    name: "PangeaVPN-0.8.2-arm64.pkg",
    url: "https://github.com/pangeavpn/pangeavpn-app/releases/download/v0.8.2/PangeaVPN-0.8.2-arm64.pkg",
    checksumsUrl: "https://github.com/pangeavpn/pangeavpn-app/releases/download/v0.8.2/SHA256SUMS.txt"
  });
  assert.equal(macPackageAsset("v0.8.2-rc.1", "x64")?.url,
    "https://github.com/pangeavpn/pangeavpn-app/releases/download/v0.8.2-rc.1/PangeaVPN-0.8.2-rc.1-x64.pkg");
});

test("a version that isn't plain semver never reaches a URL", () => {
  for (const version of ["", "latest", "0.8", "0.8.2/../../evil", "0.8.2?x=1", "0.8.2-rc.1/../x", "0.8.2#frag", " 0.8.2-", "0.8.2-rc..1"]) {
    assert.equal(macPackageAsset(version, "arm64"), null, version);
  }
});

test("only the two architectures CI builds get an asset", () => {
  assert.equal(macPackageAsset("0.8.2", "ia32"), null);
  assert.equal(macPackageAsset("0.8.2", "arm"), null);
});

test("the expected digest comes from the matching SHA256SUMS line", () => {
  const arm = "a".repeat(64);
  const x64 = "B".repeat(64);
  const sums = [
    `${"c".repeat(64)}  PangeaVPN-Setup-0.8.2-x64.exe`,
    `${arm}  PangeaVPN-0.8.2-arm64.pkg`,
    `${x64} *PangeaVPN-0.8.2-x64.pkg`,
    ""
  ].join("\n");
  assert.equal(expectedDigest(sums, "PangeaVPN-0.8.2-arm64.pkg"), arm);
  assert.equal(expectedDigest(sums, "PangeaVPN-0.8.2-x64.pkg"), x64.toLowerCase());
  assert.equal(expectedDigest(sums, "PangeaVPN-0.8.2-arm64.pkg.evil"), null);
  assert.equal(expectedDigest(`${arm.slice(1)}  PangeaVPN-0.8.2-arm64.pkg`, "PangeaVPN-0.8.2-arm64.pkg"), null);
  assert.equal(expectedDigest("<html>blocked</html>", "PangeaVPN-0.8.2-arm64.pkg"), null);
});
