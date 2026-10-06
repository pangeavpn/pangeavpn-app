# Dead-drop bootstrap

> [!NOTE]
> **Status:** implemented on desktop on 2026-08-22. Publishing is still manual,
> and Android and the DoH-carried envelope are still to come.

## Problem

Apart from the seeds that ship with the app, every address the client uses to
reach the hub is learned *from the hub*. `cachedHubIp`, the fronted endpoint hosts and the hub proxy credentials all
arrive in a `/api/client/bootstrap` or `/api/client/regions` response. Once
every enabled carrier in `HUB_METHOD_ORDER` fails, the client has no way to
learn a new address, because learning one means reaching the hub. A user in
that state is stuck until they reinstall a newer build.

The dead drop breaks that loop. It's a signed file, published somewhere a
censor won't want to block, carrying replacement addresses that the client can
verify offline.

## What this does and doesn't solve

It solves *"every address I cached is dead"*: endpoints rotated, an IP got
blocked, a worker was retired. That's the common failure.

It doesn't solve *"the technique itself is blocked"*. Most of the carriers are
TLS on 443, and a single allowlist policy kills them all. A fresh list of
addresses is worth nothing for a carrier that can't run. That failure needs a
carrier with a different shape on the wire, which is tracked separately as the
DoH-carried envelope and is out of scope here.

The dead drop is also inbound-only. It's a read, and it never gives the client a
way to send anything.

## Trust model

Two Ed25519 public keys are compiled into the client, in
`main/deadDropKeys.ts`, alongside the pinned `SERVER_PUBLIC_KEY_B64` in
`main/secureChannel.ts`:

```
active:  d1fIudP+7WehrFOqar8LxneSKuvBSQlIHsqKgQXTJFQ=
reserve: btSYGcZsOJ+G1UkSYiowPrFnbRA3yt12QwMI7XEmpS0=
```

Both private keys were generated offline and never enter any repository, server
or CI system. The reserve key sits unused until the active key is compromised.
At that point, re-signing with the reserve moves every installed client over
without an emergency release.

Because the trust root ships in the binary, whoever controls the publishing
host can serve any bytes they like and still can't produce a file the client
will accept.

## Blob format

The file is published as `bootstrap-v1.json` in the `PangeaConfig` repository
and served from two addresses with identical content:

- `https://raw.githubusercontent.com/pangeavpn/PangeaConfig/main/bootstrap-v1.json`
- `https://cdn.jsdelivr.net/gh/pangeavpn/PangeaConfig@main/bootstrap-v1.json`

The envelope signs exact bytes, so the signer and verifier never have to agree
on a JSON canonicalization:

```json
{
  "payload": "<base64 of the payload JSON bytes>",
  "sig": "<base64 Ed25519 signature over those exact bytes>",
  "key": "active"
}
```

The payload inside:

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

`key` says which pinned key to verify against, but it's only a hint. A file that
claims `active` and verifies under neither key is rejected, and the client
never lets the field widen what it will accept.

### What the payload may carry

Reserve capacity only. Anything published here is world-readable and burned
the moment it goes out, so the file carries addresses held back from normal
rotation, kept specifically to be spent rescuing a stranded client.

No credentials. In particular, no hub Shadowsocks credentials: those are
per-node rather than per-device, so publishing them would remove the "have an
account" gate on working proxy access and hand out node identities that a
censor could confirm by probing. The Shadowsocks path recovers on the next
successful hub call instead.

Hub IPs give nothing new away, since `api.pangeavpn.org` already resolves in
public DNS.

## Acceptance rules

These are checked in order, before the payload JSON is parsed:

1. The envelope parses and `sig` verifies against the active or reserve pinned
   key.
2. `v` is 1. Anything else is ignored, so a future format can't confuse an older
   client.
3. `seq` is strictly greater than the last accepted seq on disk.
4. `expires` is in the future.

Then each field goes through the existing helpers: every `hubIps` entry through
`isIPv4Literal`, and every `frontedEndpoints` entry through
`normalizeFrontedEndpoint`. Entries that fail are dropped one by one, and a file
whose entries all fail counts as no file.

`seq` is what makes a replay harmless. A genuine older file can be served back
at any time, but it's never accepted over a newer one. `expires` limits how long
a file that stopped being republished stays trusted.

### Enumerated authority

The blob can contribute hub IPs and fronted hostnames, and nothing else. It
can't set the hub hostname, supply or replace any key, carry a node list, or
turn a `HubMethod` on or off. Fields are read by name and never merged
generically.

That's what limits the damage if the signing key is stolen. If the file could
set the hub hostname or a channel key, key theft would go from a nuisance to a
full compromise.

## Fetch behavior

Plain HTTPS with certificate validation **on**. These are real hosts with real
certificates, unlike the empty-SNI direct-IP path in `main/hubTransport.ts`.

- Each host gets a 6-second timeout. Hosts are tried in the listed order, and
  the first file that passes every acceptance rule wins.
- The client makes at most one fetch attempt every 15 minutes, and remembers
  the last attempt on disk, so a crash-restart loop can't hammer the publishing
  hosts.
- The response body is capped (64 KiB) before parsing. The real file is a few
  hundred bytes, so anything large is thrown away unread.

## Client integration

The dead drop isn't a `HubMethod`. A static file can't carry a `/v1/secure`
request and return a response, so it isn't a carrier. It's a source of
addresses that makes the existing carriers work again. Adding it to
`HUB_METHOD_ORDER` would mean special-casing it everywhere the code treats a
method as a carrier.

There's a single trigger point: `resolveHubPath()` in `main/pangeaApiClient.ts`
coming back false with every enabled method exhausted. If the toggle is on and
the rate limit allows it, the client fetches, merges and runs the ladder once
more. If that fails too, the failure is reported exactly as it would be without
the dead drop.

Merging reuses the existing cache rules: `mergeFrontedEndpoints` for hosts and
`rememberHubIp` for the address. A merge never clears existing entries, for the
reasons already documented in `shared/frontedEndpoints.ts`.

The client caches one hub IP at a time, so a re-seed takes the first one the
blob names and leaves the rest for a later pass. Address diversity in the
payload therefore belongs in `frontedEndpoints`, which is a list all the way
through.

### Persisted state

Stored in settings alongside `hubMethods`:

| Key | Type | Default | Meaning |
| --- | --- | --- | --- |
| `deadDrop` | boolean | `true` | The user-facing toggle |
| `deadDropSeq` | number | `0` | Highest accepted `seq` |
| `deadDropLastAttempt` | number | | Epoch milliseconds of the last fetch, for the rate limit |

## Files

New:

| File | Role |
| --- | --- |
| `src/shared/deadDropBlob.ts` | Decode, verify and validate. Pure: no network and no Electron, so it's fully unit-testable |
| `src/shared/deadDropBlob.test.ts` | Tests for the above |
| `src/main/deadDrop.ts` | Host list, fetch with timeout and failover, rate limit, seq persistence |
| `src/main/deadDrop.test.ts` | Tests for the above |
| `src/main/deadDropKeys.ts` | The two pinned verify keys, kept in one small greppable module instead of buried in the client |
| `scripts/publish-deaddrop.mjs` | Builds the payload at `seq+1`, signs it with a key read from an environment variable, and writes the file for a manual commit |

Modified:

| File | Change |
| --- | --- |
| `src/main/pangeaApiClient.ts` | Retry after a re-seed at the `resolveHubPath()` exhaustion point |
| `src/renderer/index.ts`, `index.html`, `global.d.ts`, `shared/ipc.ts`, `main/preload.ts`, the eight locale files | One toggle beside the hub method switches, with its own IPC pair |
| `tsconfig.main.json` | `allowImportingTsExtensions` plus `rewriteRelativeImportExtensions` (see below) |

`node --test` runs the TypeScript sources directly and needs explicit `.ts`
specifiers, while the emitted CommonJS needs `.js`. The rewrite flag satisfies
both from one source form, and the emitted `require("./ipLiteral.js")` is proof
that it works.

### Prerequisite refactor

`isIPv4Literal` was defined privately in both `main/pangeaApiClient.ts` and
`shared/naiveEndpoint.ts`, and neither exported it. The pure
`shared/deadDropBlob.ts` couldn't import either copy, and a third copy would be
the wrong answer for a validator that guards what the client will dial.

The two copies also disagreed. The one in `naiveEndpoint` rejected leading
zeros and the one in `pangeaApiClient` accepted them. `010.1.1.1` is the
classic octal-parsing ambiguity, and Go's `net.ParseIP` rejects it, so the
strict version is the right one. Both now live in `src/shared/ipLiteral.ts`
next to `isIPv6Literal`, and all three call sites import from there.

> [!WARNING]
> This deliberately changes behavior on the `pangeaApiClient` side. A DoH answer
> or cached hub IP written with a leading zero is now rejected instead of being
> dialed. Well-formed resolvers and the hub never produce that form.

## Publishing

Publishing is manual for this phase. The operator runs `publish-deaddrop.mjs`
with the private key available, commits the regenerated `bootstrap-v1.json` to
`PangeaConfig`, and pushes. Automating it from the hub can wait until the format
has settled.

## Testing

Everything was built test-first, and no test touches the network.

| Area | Cases |
| --- | --- |
| Verification | A valid signature is accepted; a tampered payload is rejected; a signature from an unrelated key is rejected; the reserve key is accepted; a `key` field that lies about which key signed doesn't change the outcome |
| Acceptance | Equal and lower `seq` are rejected; an expired file is rejected; an unknown `v` is ignored; malformed base64, a truncated envelope and a non-JSON payload are all rejected without throwing |
| Validation | Invalid IPs and hostnames are dropped one by one; a file of nothing but invalid entries counts as no file |
| Fetch | Failover from a dead first host; the rate limit blocks a second attempt inside the window and allows one after it; an oversized body is discarded |
| Integration | A client with every method exhausted re-seeds and retries exactly once; a client that still fails reports the same error as before |

## Security analysis

### A hostile file is served

This covers a compromised publishing account or a hostile CDN edge. The
signature fails, the file is discarded, and the client is exactly as reachable
as it was before, never worse. Certificate validation means a network attacker
doesn't even get as far as this check.

### An older genuine file is replayed

`seq` rejects it, and `expires` bounds how long it could matter.

### The signing key is stolen

The attacker can point the client at hub IPs and fronted hosts they control, but
they can't read the request. It's encrypted with AES-256-GCM under a key derived
by X25519 ECDH against the pinned hub key, so the bearer token, `licenseKey` and
`identityPubkey` stay ciphertext. They can't forge a response either, because
the reply is authenticated under the same derived key. What they're left with is
denial of service, plus the knowledge that an address belongs to a Pangea user.
Recovery means re-signing with the reserve key.

### A censor reads the file

That's expected, since it's public. They learn the reserve fronted hostnames and
hub IPs. That's the accepted cost, and it's why the payload is reserve capacity
only.

> [!NOTE]
> When this was written, `secureChannel` derived one key for both directions
> instead of separate request and response keys. Fixing that needed a
> coordinated hub deploy, so it was deferred. `/v2/secure` has since split the
> keys (see [Post-quantum protection](post-quantum.md)), and v1 remains only as
> a fallback. The caveat never weakened the analysis above: an attacker holding
> the signing key still has no shared secret.

## Out of scope

- **Android and the Go daemon.** Desktop comes first. Settling the format here
  gives a second implementation something fixed to agree with.
- **Automated publishing from the hub.**
- **The DoH-carried envelope.** That's the real answer to a technique-level
  block, and it's a separate project. The payload is versioned so the
  envelope's parameters (authoritative domain, resolver hints) can arrive later
  as `v: 2`, which older clients will ignore.
