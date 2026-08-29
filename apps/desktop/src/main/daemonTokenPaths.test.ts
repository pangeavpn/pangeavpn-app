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

// A managed install writes no token of its own, but an older build may have
// left one behind; trying it first costs a doomed 401 on every daemon call.
test("Linux tries the managed daemon's token before a stale user-dir one", () => {
  const candidates = daemonTokenCandidatePaths("linux", "/etc/pangeavpn/daemon-token.txt", APPDATA);
  const userToken = path.normalize(path.join(APPDATA, "pangeavpn-desktop", "daemon-token.txt"));

  assert.equal(candidates[0], path.normalize("/etc/pangeavpn/daemon-token.txt"));
  assert.ok(candidates.includes(userToken), `user-dir fallback missing: ${candidates.join(", ")}`);
  assert.ok(candidates.indexOf(userToken) > 0, "user-dir token must not be tried first");
});

// An unmanaged/dev install still reads its own directory first.
test("Linux without a managed service keeps the user-dir token first", () => {
  const userToken = path.normalize(path.join(APPDATA, "pangeavpn-desktop", "daemon-token.txt"));
  const candidates = daemonTokenCandidatePaths("linux", userToken, APPDATA);

  assert.equal(candidates[0], userToken);
  assert.equal(new Set(candidates).size, candidates.length);
});

test("the configured token path is tried first and never duplicated", () => {
  const tokenPath = path.normalize("/Library/Application Support/PangeaVPN/daemon-token.txt");
  const candidates = daemonTokenCandidatePaths("darwin", tokenPath, APPDATA);

  assert.equal(candidates[0], tokenPath);
  assert.equal(new Set(candidates).size, candidates.length);
});
