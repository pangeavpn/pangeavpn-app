import assert from "node:assert/strict";
import path from "node:path";
import test from "node:test";
import { daemonTokenCandidatePaths } from "./daemonTokenPaths.ts";

const APPDATA = "/Users/someone/Library/Application Support";

test("macOS looks where a root daemon writes its token by default", () => {
  const candidates = daemonTokenCandidatePaths(
    "darwin",
    path.join("/Library/Application Support/PangeaVPN", "daemon-token.txt"),
    APPDATA
  );

  assert.ok(
    candidates.includes(path.normalize("/Library/Application Support/pangeavpn-desktop/daemon-token.txt")),
    `system default dir missing from candidates: ${candidates.join(", ")}`
  );
});

test("Linux looks in the root daemon's state dir", () => {
  const candidates = daemonTokenCandidatePaths("linux", "/etc/pangeavpn/daemon-token.txt", APPDATA);
  assert.ok(
    candidates.includes(path.normalize("/var/lib/pangeavpn-desktop/daemon-token.txt")),
    `linux state dir missing from candidates: ${candidates.join(", ")}`
  );
});

test("the configured token path is tried first and never duplicated", () => {
  const tokenPath = path.normalize("/Library/Application Support/PangeaVPN/daemon-token.txt");
  const candidates = daemonTokenCandidatePaths("darwin", tokenPath, APPDATA);

  assert.equal(candidates[0], tokenPath);
  assert.equal(new Set(candidates).size, candidates.length);
});
