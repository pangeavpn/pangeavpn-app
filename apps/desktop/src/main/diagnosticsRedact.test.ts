import assert from "node:assert/strict";
import test from "node:test";
import { isKeptIpv4, isKeptIpv6, redactDiagnostics } from "./diagnosticsRedact.ts";

interface Case {
  readonly what: string;
  readonly line: string;
  readonly gone: readonly string[];
  readonly kept: readonly string[];
}

const ACCOUNT_DISPLAY = "K7M2-9QXD-4RTB-8WZC-3NVF-6HJP";
const ACCOUNT_NORMALIZED = "K7M29QXD4RTB8WZC3NVF6HJP";
const WG_KEY = "cGFuZ2VhdGVzdGtleXBhbmdlYXRlc3RrZXkxMjM0NTY3OD0=";
const MLKEM = "A".repeat(40) + "bQ9" + "z".repeat(60) + "8Kx" + "=";
const HEX64 = "9f2c" + "a".repeat(56) + "1b7e";

const CASES: readonly Case[] = [
  {
    what: "account number in display form",
    line: `token login rejected for ${ACCOUNT_DISPLAY} (401)`,
    gone: [ACCOUNT_DISPLAY, "K7M2-9QXD"],
    kept: ["token login rejected for", "(401)"]
  },
  {
    what: "account number in normalised form",
    line: `rememberAccountNumber wrote ${ACCOUNT_NORMALIZED} to remembered-account.dat`,
    gone: [ACCOUNT_NORMALIZED],
    kept: ["rememberAccountNumber wrote", "remembered-account.dat"]
  },
  {
    what: "legacy 16-digit token",
    line: "auth:login called with legacy token 4929100288371625",
    gone: ["4929100288371625"],
    kept: ["auth:login called with legacy token"]
  },
  {
    what: "bearer token",
    line: "Token login failed (401): Authorization: Bearer eyJhbGci.eyJzdWIiOjF9.QmFk-Sig_99",
    gone: ["eyJhbGci", "QmFk-Sig_99"],
    kept: ["Token login failed (401)", "Authorization: Bearer"]
  },
  {
    what: "license key header value",
    line: "hub rejected header X-License-Key: LIC-9f2ca7be-prod and returned 403",
    gone: ["LIC-9f2ca7be-prod"],
    kept: ["hub rejected header X-License-Key:", "returned 403"]
  },
  {
    what: "license key in a JSON body",
    line: '{"licenseKey":"LIC-9f2ca7be-prod","region":"fra"}',
    gone: ["LIC-9f2ca7be-prod"],
    kept: ['"region":"fra"']
  },
  {
    what: "WireGuard base64 key",
    line: `[wg] peer add failed for key ${WG_KEY} on utun6`,
    gone: [WG_KEY],
    kept: ["[wg] peer add failed for key", "on utun6"]
  },
  {
    what: "WireGuard config assignment",
    line: "PrivateKey = qKp0RmVyeVNlY3JldEtleVZhbHVlSGVyZTEyMzQ1Ng=",
    gone: ["qKp0RmVyeVNlY3JldEtleVZhbHVl"],
    kept: ["PrivateKey ="]
  },
  {
    what: "longer ML-KEM post-quantum material",
    line: `[pq] offer parse failed for ${MLKEM} (truncated)`,
    gone: [MLKEM],
    kept: ["[pq] offer parse failed for", "(truncated)"]
  },
  {
    what: "64-hex daemon token",
    line: `daemon token ${HEX64} rejected by /v1/status`,
    gone: [HEX64],
    kept: ["daemon token", "rejected by /v1/status"]
  },
  {
    what: "UUID",
    line: "device 8f14e45f-ceea-467a-9c3f-1b2d4e5a6c7d renamed to Work Laptop",
    gone: ["8f14e45f-ceea-467a-9c3f-1b2d4e5a6c7d"],
    kept: ["device", "renamed to Work Laptop"]
  },
  {
    what: "email address",
    line: "auth0 profile loaded for dana.reyes+vpn@example.co.uk (sub google-oauth2)",
    gone: ["dana.reyes+vpn@example.co.uk", "example.co.uk"],
    kept: ["auth0 profile loaded for", "(sub google-oauth2)"]
  },
  {
    what: "public IPv4",
    line: "[DoH] https://1.1.1.1/dns-query resolved api.pangeavpn.org to 203.0.113.44",
    gone: ["203.0.113.44", "1.1.1.1"],
    kept: ["[DoH] https://", "/dns-query resolved api.pangeavpn.org to"]
  },
  {
    what: "public IPv6",
    line: "handshake to [2606:4700:4700::1111]:51820 timed out",
    gone: ["2606:4700:4700::1111"],
    kept: ["handshake to [", "]:51820 timed out"]
  }
];

for (const item of CASES) {
  test(`redacts ${item.what} and keeps the rest of the line`, () => {
    const out = redactDiagnostics(item.line);
    for (const secret of item.gone) {
      assert.ok(!out.includes(secret), `leaked ${secret} in: ${out}`);
    }
    for (const context of item.kept) {
      assert.ok(out.includes(context), `lost context ${context} in: ${out}`);
    }
    assert.match(out, /\[redacted:[a-zA-Z0-9]+\]/);
  });
}

test("loopback and RFC1918 addresses are kept on purpose: they describe the tunnel, not the user", () => {
  const line = "route 10.8.0.2/32 via 192.168.1.1, daemon listening on 127.0.0.1:7890, link-local 169.254.3.9";
  assert.equal(redactDiagnostics(line), line);
  assert.equal(redactDiagnostics("hub health probe to 172.16.4.7 ok"), "hub health probe to 172.16.4.7 ok");
  assert.ok(isKeptIpv4("10.0.0.1"));
  assert.ok(isKeptIpv4("172.31.255.254"));
  assert.ok(!isKeptIpv4("172.32.0.1"));
  assert.ok(!isKeptIpv4("8.8.8.8"));
});

test("loopback, link-local and ULA IPv6 are kept; global unicast is not", () => {
  assert.equal(redactDiagnostics("bound ::1 and fe80::1%utun6 and fd00::5"), "bound ::1 and fe80::1%utun6 and fd00::5");
  assert.ok(isKeptIpv6("::1"));
  assert.ok(isKeptIpv6("fe80::abcd"));
  assert.ok(isKeptIpv6("fd12:3456::1"));
  assert.ok(!isKeptIpv6("2001:db8::1"));
});

test("timestamps and versions are not mistaken for addresses", () => {
  const line = "2026-09-13T10:22:31.123Z [warn] PangeaVPN 0.7.1 daemon exited code=1 signal=null";
  assert.equal(redactDiagnostics(line), line);
});

test("file paths are not mistaken for keys", () => {
  const line = "could not open C:\\ProgramData\\PangeaVPN\\daemon.log (EACCES)";
  assert.equal(redactDiagnostics(line), line);
  const unix = "reading /Library/Application Support/PangeaVPN/daemon-elevated.log";
  assert.equal(redactDiagnostics(unix), unix);
});

test("a multi-line block is redacted line by line without collapsing", () => {
  const block = [
    "2026-09-13T10:22:31.000Z [log] connecting to fra-01",
    `2026-09-13T10:22:31.400Z [log] peer key ${WG_KEY}`,
    "2026-09-13T10:22:33.900Z [log] handshake ok via 10.8.0.1"
  ].join("\n");
  const out = redactDiagnostics(block);
  assert.equal(out.split("\n").length, 3);
  assert.ok(!out.includes(WG_KEY));
  assert.ok(out.includes("connecting to fra-01"));
  assert.ok(out.includes("handshake ok via 10.8.0.1"));
});

test("ordinary prose survives untouched", () => {
  const line = "Hub unreachable; retrying in 20s (method=normal, attempt 2)";
  assert.equal(redactDiagnostics(line), line);
});
