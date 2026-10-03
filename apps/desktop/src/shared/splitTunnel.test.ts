import assert from "node:assert/strict";
import test from "node:test";
import {
  createSplitTunnelWriter,
  DEFAULT_SPLIT_TUNNEL_CONFIG,
  normalizeSplitTunnelConfig,
  parseCidrText,
  parseSplitTunnelInvalid,
  protectRulesFor,
  sameSplitRule,
  SplitTunnelUnsupportedError,
  withAppChange,
  type SplitTunnelBackend,
  type SplitTunnelConfig,
  type SplitTunnelResult,
  type SplitTunnelWriteBody
} from "./splitTunnel.ts";

test("parseCidrText turns bare addresses into /32 and keeps prefixes as typed", () => {
  const parsed = parseCidrText("203.0.113.0/24, 198.51.100.7\n10.1.2.3/8;192.0.2.1/32");
  assert.deepEqual(parsed.cidrs, ["203.0.113.0/24", "198.51.100.7/32", "10.1.2.3/8", "192.0.2.1/32"]);
  assert.deepEqual(parsed.invalid, []);
});

test("parseCidrText reports each bad token by index and keeps the text", () => {
  const parsed = parseCidrText(
    "10.0.0.0/8 nope 2001:db8::/32 1.2.3.4/33 256.1.1.1 01.2.3.4 1.2.3.4/ 1.2.3/24 8.8.8.8 1.2.3.4/08 1.2.3.4/24/1 ::ffff:1.2.3.4"
  );
  assert.deepEqual(parsed.cidrs, ["10.0.0.0/8", "8.8.8.8/32"]);
  assert.deepEqual(
    parsed.invalid.map((item) => [item.index, item.value, item.code]),
    [
      [1, "nope", "notIPv4"],
      [2, "2001:db8::/32", "notIPv4"],
      [3, "1.2.3.4/33", "notIPv4"],
      [4, "256.1.1.1", "notIPv4"],
      [5, "01.2.3.4", "notIPv4"],
      [6, "1.2.3.4/", "notIPv4"],
      [7, "1.2.3/24", "notIPv4"],
      [9, "1.2.3.4/08", "notIPv4"],
      [10, "1.2.3.4/24/1", "notIPv4"],
      [11, "::ffff:1.2.3.4", "notIPv4"]
    ]
  );
  assert.equal(parsed.tokens.length, 12);
});

test("parseCidrText leaves prefix-length policy and masking to the daemon", () => {
  assert.deepEqual(parseCidrText("0.0.0.0/0 10.0.0.0/4 10.1.2.3/8").cidrs, ["0.0.0.0/0", "10.0.0.0/4", "10.1.2.3/8"]);
});

test("parseCidrText of an empty field clears the ranges", () => {
  assert.deepEqual(parseCidrText("  \n ,"), { cidrs: [], tokens: [], invalid: [] });
});

test("normalizeSplitTunnelConfig tolerates junk and fills gaps from the fallback", () => {
  const fallback: SplitTunnelConfig = {
    enabled: true,
    apps: ["C:\\Games\\"],
    cidrs: ["10.0.0.0/8"],
    appsSupported: true,
    unavailableReason: "",
    active: true,
    pending: false,
    cidrsDropped: false
  };
  assert.deepEqual(normalizeSplitTunnelConfig({ enabled: false, apps: ["a", 3], pending: true }, fallback), {
    ...fallback,
    enabled: false,
    apps: ["a"],
    pending: true
  });
  assert.equal(normalizeSplitTunnelConfig("nonsense").enabled, false);
  assert.deepEqual(normalizeSplitTunnelConfig(null).apps, []);
});

test("normalizeSplitTunnelConfig takes cidrsDropped from the reply alone, never a stale fallback", () => {
  const stale: SplitTunnelConfig = { ...DEFAULT_SPLIT_TUNNEL_CONFIG, enabled: true, cidrs: ["10.0.0.0/8"], cidrsDropped: true };
  assert.equal(normalizeSplitTunnelConfig({ cidrsDropped: true }).cidrsDropped, true);
  assert.equal(normalizeSplitTunnelConfig({ enabled: true, cidrs: ["10.0.0.0/8"] }, stale).cidrsDropped, false);
  assert.equal(normalizeSplitTunnelConfig({ cidrsDropped: "true" }, stale).cidrsDropped, false);
  assert.equal(DEFAULT_SPLIT_TUNNEL_CONFIG.cidrsDropped, false);
});

test("parseSplitTunnelInvalid reads the 400 shape and maps unknown codes", () => {
  const body = JSON.stringify({
    ok: false,
    error: "invalid_split_tunnel",
    invalid: [
      { field: "apps", index: 1, code: "systemProcess" },
      { field: "cidrs", index: 0, code: "prefixTooShort" },
      { field: "cidrs", index: 2, code: "somethingNew" },
      { field: "other", index: 0, code: "nul" },
      { field: "apps", index: -1, code: "nul" }
    ]
  });
  assert.deepEqual(parseSplitTunnelInvalid(body), [
    { field: "apps", index: 1, code: "systemProcess" },
    { field: "cidrs", index: 0, code: "prefixTooShort" },
    { field: "cidrs", index: 2, code: "invalid" }
  ]);
  assert.equal(parseSplitTunnelInvalid('{"ok":false,"error":"invalid json"}'), null);
  assert.equal(parseSplitTunnelInvalid("not json"), null);
});

test("rule comparison follows each OS's path case rules", () => {
  assert.ok(sameSplitRule("C:\\Games\\Foo\\", "c:/games/foo/", "win32"));
  assert.ok(sameSplitRule("/Applications/Foo.app", "/applications/foo.app/", "darwin"));
  assert.ok(!sameSplitRule("/opt/Foo/", "/opt/foo/", "linux"));
});

test("withAppChange adds once and removes every case variant", () => {
  const apps = ["C:\\Games\\Foo\\", "C:\\Tools\\bar.exe"];
  assert.deepEqual(withAppChange(apps, "c:\\games\\foo\\", true, "win32"), apps);
  assert.deepEqual(withAppChange(apps, "C:\\New\\x.exe", true, "win32"), [...apps, "C:\\New\\x.exe"]);
  assert.deepEqual(withAppChange(apps, "C:\\TOOLS\\BAR.EXE", false, "win32"), ["C:\\Games\\Foo\\"]);
});

test("protectRulesFor names the bundle on macOS and the exe elsewhere", () => {
  assert.deepEqual(protectRulesFor("/Applications/PangeaVPN.app/Contents/MacOS/PangeaVPN", "darwin"), [
    "/Applications/PangeaVPN.app"
  ]);
  assert.deepEqual(protectRulesFor("C:\\Program Files\\PangeaVPN\\PangeaVPN.exe", "win32"), [
    "C:\\Program Files\\PangeaVPN\\PangeaVPN.exe"
  ]);
  assert.deepEqual(protectRulesFor("/opt/PangeaVPN/pangeavpn", "linux"), ["/opt/PangeaVPN/pangeavpn"]);
});

function fakeBackend(initial: SplitTunnelConfig) {
  let stored = initial;
  const posts: SplitTunnelWriteBody[] = [];
  let failGet = false;
  let reject: SplitTunnelResult | null = null;
  const backend: SplitTunnelBackend = {
    async get() {
      await new Promise((resolve) => setTimeout(resolve, 1));
      if (failGet) throw new Error("daemon request timeout (GET /split-tunnel)");
      return { ...stored, apps: [...stored.apps], cidrs: [...stored.cidrs] };
    },
    async set(body, current) {
      posts.push(body);
      await new Promise((resolve) => setTimeout(resolve, 1));
      if (reject) return reject;
      stored = { ...current, enabled: body.enabled, apps: body.apps, cidrs: body.cidrs };
      return { ok: true, config: stored };
    }
  };
  return {
    backend,
    posts,
    failGets: () => (failGet = true),
    rejectWith: (result: SplitTunnelResult) => (reject = result)
  };
}

const EMPTY: SplitTunnelConfig = {
  enabled: false,
  apps: [],
  cidrs: [],
  appsSupported: true,
  unavailableReason: "",
  active: false,
  pending: false,
  cidrsDropped: false
};

test("writer: a failed GET writes nothing and rejects", async () => {
  const fake = fakeBackend({ ...EMPTY, apps: ["C:\\Keep\\me.exe"] });
  fake.failGets();
  const writer = createSplitTunnelWriter(fake.backend, { platform: "win32", protect: () => ["C:\\P\\PangeaVPN.exe"] });
  await assert.rejects(writer.setEnabled(true), /timeout/);
  assert.equal(fake.posts.length, 0);
});

test("writer: overlapping edits are applied in order, none lost", async () => {
  const fake = fakeBackend(EMPTY);
  const writer = createSplitTunnelWriter(fake.backend, { platform: "win32", protect: () => ["C:\\P\\PangeaVPN.exe"] });
  const results = await Promise.all([
    writer.setApp("C:\\A\\a.exe", true),
    writer.setApp("C:\\B\\", true),
    writer.setEnabled(true),
    writer.setApp("c:\\a\\A.EXE", false)
  ]);
  assert.ok(results.every((result) => result.ok));
  const last = results[3];
  assert.ok(last.ok);
  assert.deepEqual(last.config.apps, ["C:\\B\\"]);
  assert.equal(last.config.enabled, true);
  assert.deepEqual(fake.posts.map((post) => post.protect), Array(4).fill(["C:\\P\\PangeaVPN.exe"]));
  assert.ok(fake.posts.every((post) => Object.keys(post).sort().join() === "apps,cidrs,enabled,protect"));
});

test("writer: invalid range text never reaches the daemon", async () => {
  const fake = fakeBackend(EMPTY);
  const writer = createSplitTunnelWriter(fake.backend, { platform: "linux", protect: () => [] });
  const result = await writer.setCidrs("10.0.0.0/8, bogus");
  assert.deepEqual(result, { ok: false, invalid: [{ field: "cidrs", index: 1, code: "notIPv4", value: "bogus" }] });
  assert.equal(fake.posts.length, 0);
});

test("writer: daemon rejections carry the offending entry back", async () => {
  const fake = fakeBackend({ ...EMPTY, apps: ["/usr/bin/"] });
  fake.rejectWith({
    ok: false,
    invalid: [
      { field: "apps", index: 0, code: "tooBroad" },
      { field: "cidrs", index: 1, code: "prefixTooShort" }
    ]
  });
  const writer = createSplitTunnelWriter(fake.backend, { platform: "linux", protect: () => [] });
  const result = await writer.setCidrs("10.0.0.1 ,  1.0.0.0/4");
  assert.deepEqual(result, {
    ok: false,
    invalid: [
      { field: "apps", index: 0, code: "tooBroad", value: "/usr/bin/" },
      { field: "cidrs", index: 1, code: "prefixTooShort", value: "1.0.0.0/4" }
    ]
  });
  assert.deepEqual(fake.posts[0].cidrs, ["10.0.0.1/32", "1.0.0.0/4"]);
});

test("writer: an old daemon reads as unsupported, and the chain survives a failure", async () => {
  const backend: SplitTunnelBackend = { get: async () => null, set: async () => null };
  const writer = createSplitTunnelWriter(backend, { platform: "darwin", protect: () => [] });
  await assert.rejects(writer.setEnabled(true), SplitTunnelUnsupportedError);
  await assert.rejects(writer.setApp("/Applications/X.app", true), SplitTunnelUnsupportedError);
});
