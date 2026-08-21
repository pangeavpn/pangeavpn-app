# Dead-drop bootstrap

Status: implemented 2026-08-22 (desktop). Publishing is manual; Android and the
DoH-carried envelope are still to come.

## Problem

Every address the client uses to reach the hub is learned *from the hub*:
`cachedHubIp`, the fronted endpoint hosts, and the hub Shadowsocks credentials
all arrive on a `/api/client/bootstrap` or `/api/client/regions` response. When
every enabled carrier in `HUB_METHOD_ORDER` fails, the client has no way to
learn a new address, because learning one requires reaching the hub. A user in
that state is stranded until they reinstall with a newer build.

The dead drop breaks the cycle with a signed file published somewhere a censor
is unwilling to block, carrying replacement addresses the client can verify
offline.

## What this does and does not solve

Solves: *"every address I cached is dead."* Endpoints rotated, an IP was
blocked, a worker was retired. This is the common failure.

Does not solve: *"the technique itself is blocked."* Three of the four carriers
are TLS on 443 and die to a single allowlist policy. A fresh list of addresses
for a carrier that cannot run is worth nothing. That failure needs a carrier
with a different shape on the wire, tracked separately as the DoH-carried
envelope and out of scope here.

The dead drop is also inbound-only. It is a read. It never gives the client a
way to send anything.

## Trust model

Two Ed25519 public keys are compiled into the client, alongside the existing
pinned `SERVER_PUBLIC_KEY_B64` in `main/secureChannel.ts`:

```
active:  d1fIudP+7WehrFOqar8LxneSKuvBSQlIHsqKgQXTJFQ=
reserve: btSYGcZsOJ+G1UkSYiowPrFnbRA3yt12QwMI7XEmpS0=
```

Both private keys are generated offline and never enter any repository, server,
or CI system. The reserve key is unused until the active key is compromised, at
which point re-signing with the reserve moves every already-installed client
without an emergency release.

The trust root ships in the binary, so a party who controls the publishing host
can serve any bytes they like and still cannot produce an accepted file.

## Blob format

Published as `bootstrap-v1.json` in the `PangeaConfig` repository, served from
two addresses with identical content:

- `https://raw.githubusercontent.com/pangeavpn/PangeaConfig/main/bootstrap-v1.json`
- `https://cdn.jsdelivr.net/gh/pangeavpn/PangeaConfig@main/bootstrap-v1.json`

The envelope signs exact bytes, so signer and verifier never have to agree on a
JSON canonicalisation:

```json
{
  "payload": "<base64 of the payload JSON bytes>",
  "sig": "<base64 Ed25519 signature over those exact bytes>",
  "key": "active"
}
```

Payload:

```json
{
  "v": 1,
  "seq": 7,
  "issued": "2026-08-21T19:00:00Z",
  "expires": "2026-11-19T00:00:00Z",
  "hubIps": ["203.0.113.4"],
  "frontedEndpoints": ["reserve-a.example.workers.dev"]
}
```

`key` selects which pinned key to verify against. It is a hint only: a file
claiming `active` that verifies under neither key is rejected, and the client
never trusts the field to widen what it will accept.

### What the payload may carry

Reserve capacity only. Everything published here is world-readable and is burned
on publication, so the file carries addresses held back from normal rotation,
existing to be spent recovering a stranded client.

No credentials. In particular no hub Shadowsocks credentials: those are per-node
rather than per-device, so publishing them would remove the "have an account"
gate on working proxy access and hand out probe-confirmable node identities. The
Shadowsocks path recovers on the next successful hub call instead.

Hub IPs leak nothing new, since `api.pangeavpn.org` already resolves in public
DNS.

## Acceptance rules

Checked in this order, before the payload JSON is parsed:

1. Envelope parses and `sig` verifies against the active or reserve pinned key.
2. `v` equals 1. Anything else is ignored, so a future format cannot confuse an
   older client.
3. `seq` is strictly greater than the last accepted seq on disk.
4. `expires` is in the future.

Then each field is validated with the existing helpers: every entry of `hubIps`
through `isIPv4Literal`, every entry of `frontedEndpoints` through
`normalizeFrontedEndpoint`. Entries that fail are dropped individually; a file
whose entries all fail is treated as no file.

`seq` is what makes a replay harmless. A genuine older file can be served back
at any time but is never accepted over a newer one. `expires` bounds how long a
file that stopped being republished stays trusted.

### Enumerated authority

The blob may contribute hub IPs and fronted hostnames. Nothing else. It may not
set the hub hostname, supply or replace any key, carry a node list, or enable or
disable a `HubMethod`. Fields are read by name, never merged generically.

This is what bounds the damage from signing-key theft. If the file could set the
hub hostname or a channel key, key theft would escalate from nuisance to full
compromise.

## Fetch behaviour

Ordinary HTTPS with certificate validation **on** — these are real hosts with
real certificates, unlike the empty-SNI direct-IP path in `main/hubTransport.ts`.

- 6 second timeout per host, hosts tried in listed order, first file that passes
  every acceptance rule wins.
- At most one fetch attempt per 15 minutes, persisted to disk, so a
  crash-restart loop cannot hammer the publishing hosts.
- Response body capped before parsing; the file is a few hundred bytes and
  anything large is discarded unread.

## Client integration

The dead drop is not a `HubMethod`. A static file cannot carry a `/v1/secure`
request and return a response, so it is not a carrier; it is a source of
addresses that makes the existing carriers work again. Adding it to
`HUB_METHOD_ORDER` would require special-casing it at every point that treats a
method as a carrier.

Single trigger point: `resolveHubPath()` in `main/pangeaApiClient.ts` returning
false with every enabled method exhausted. If the toggle is on and the rate
limit allows, fetch, merge, and run the ladder once more. If it still fails, the
failure is reported exactly as it is today.

Merging reuses the existing cache rules: `mergeFrontedEndpoints` for hosts, and
`rememberHubIp` for the address. A merge never clears existing entries, matching
the reasoning already documented in `shared/frontedEndpoints.ts`.

The client caches one hub IP at a time, so a re-seed takes the first the blob
names and leaves the rest for a later pass. Address diversity in the payload
therefore belongs in `frontedEndpoints`, which is a list end to end.

### Persisted state

Added to settings alongside `hubMethods`:

- `deadDrop: boolean` — default on
- `deadDropSeq: number` — highest accepted seq, default 0
- `deadDropLastAttempt: number` — epoch ms, for the rate limit

## Files

New:

- `src/shared/deadDropBlob.ts` — decode, verify, validate. Pure; no network, no
  electron, fully unit-testable.
- `src/shared/deadDropBlob.test.ts`
- `src/main/deadDrop.ts` — host list, fetch with timeout and failover, rate
  limit, seq persistence.
- `src/main/deadDrop.test.ts`
- `scripts/publish-deaddrop.mjs` — builds the payload at `seq+1`, signs with the
  key read from an environment variable, writes the file for manual commit.
- `src/main/deadDropKeys.ts` — the two pinned verify keys, kept in one small
  greppable module rather than buried in the client.

Modified:

- `src/main/pangeaApiClient.ts` — retry after re-seed at the `resolveHubPath()`
  exhaustion point.
- `src/renderer/index.ts`, `index.html`, `global.d.ts`, `shared/ipc.ts`,
  `main/preload.ts` and the eight locale files — one toggle beside the hub
  method switches, with its own IPC pair.
- `tsconfig.main.json` — `allowImportingTsExtensions` plus
  `rewriteRelativeImportExtensions`. `node --test` runs the TypeScript sources
  directly and needs explicit `.ts` specifiers; the emitted CommonJS needs `.js`.
  The rewrite flag satisfies both from one source form, and the emitted
  `require("./ipLiteral.js")` is the check that it works.

### Prerequisite refactor

`isIPv4Literal` was defined privately in both `main/pangeaApiClient.ts` and
`shared/naiveEndpoint.ts`, exported from neither. A pure `shared/deadDropBlob.ts`
cannot import either copy, and a third copy would be the wrong answer for a
validator that guards what the client will dial.

The two copies were not identical. `naiveEndpoint`'s rejected leading zeros;
`pangeaApiClient`'s accepted them. `010.1.1.1` is the classic octal-parsing
ambiguity and Go's `net.ParseIP` rejects it, so the strict version is the
correct one. Both now live in `src/shared/ipLiteral.ts` alongside
`isIPv6Literal`, and all three call sites import from there.

This is a deliberate behaviour change on the `pangeaApiClient` side: a DoH
answer or cached hub IP written with a leading zero is now rejected rather than
dialed. Well-formed resolvers and the hub never emit that form.

## Publishing

Manual for this phase. The operator runs `publish-deaddrop.mjs` with the private
key available, commits the regenerated `bootstrap-v1.json` to `PangeaConfig`,
and pushes. Automating this from the hub is deferred until the format has
settled.

## Testing

TDD throughout. No test touches the network.

Verification: valid signature accepted; tampered payload rejected; signature
from an unrelated key rejected; reserve key accepted; a `key` field lying about
which key signed does not change the outcome.

Acceptance: equal and lower `seq` rejected; expired file rejected; unknown `v`
ignored; malformed base64, truncated envelope, and non-JSON payload all rejected
without throwing.

Validation: invalid IPs and hostnames dropped individually; a file of entirely
invalid entries treated as no file.

Fetch: failover from a dead first host; rate limit blocks a second attempt
inside the window and allows one after it; oversized body discarded.

Integration: a client with every method exhausted re-seeds and retries exactly
once; a client that still fails reports the same error it does today.

## Security analysis

**Hostile file served** (publishing account compromised, hostile CDN edge).
Signature fails, file discarded, client exactly as reachable as before. Never
worse than current behaviour. Certificate validation means a network attacker
does not reach this check at all.

**Replay of a genuine older file.** Rejected by `seq`, bounded by `expires`.

**Signing key theft.** The attacker can aim the client at hub IPs and fronted
hosts they control. They cannot read the request: it is AES-256-GCM under a key
derived by X25519 ECDH against the pinned hub key, so the bearer token,
`licenseKey` and `identityPubkey` stay ciphertext. They cannot forge a response:
the reply is authenticated under the same derived key. What remains is denial of
service plus the metadata that an address belongs to a Pangea user. Recovery is
re-signing with the reserve key.

**Censor reads the file.** Expected; it is public. They gain the reserve fronted
hostnames and hub IPs. This is the accepted cost and the reason the payload is
reserve capacity only.

Known unrelated caveat: `secureChannel` derives one key used in both directions
rather than separating request and response keys. That finding is deliberately
deferred because fixing it requires a coordinated hub deploy. It does not weaken
the analysis above — an attacker holding the signing key still has no shared
secret — but it is noted because that envelope is cited as the safety net.

## Out of scope

- Android and the Go daemon. Desktop first; the format is settled here so a
  second implementation has something fixed to agree with.
- Automated publishing from the hub.
- The DoH-carried envelope. It is the actual answer to a technique-level block
  and is a separate project. The payload is versioned so its parameters —
  authoritative domain, resolver hints — can arrive later as `v: 2`, which older
  clients ignore.
