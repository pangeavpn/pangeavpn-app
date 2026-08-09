# Edge relay

A Cloudflare Worker that forwards the hub's `/v1/secure` envelope to
`api.pangeavpn.org`. It backs the client's `fronted` hub method.

## Why it exists

The client has four other ways to reach the hub (see `ensureHub` in
[`pangeaApiClient.ts`](../../apps/desktop/src/main/pangeaApiClient.ts)): a cached
IP, a DoH-resolved IP with no SNI, the daemon's Shadowsocks proxy, and plain
HTTPS. Between them they defeat DNS poisoning, SNI blocking, and a blackholed
hub IP.

What they do not defeat is enumeration. All four terminate on address space we
own, so a censor who sweeps our IPs and null-routes them takes out every path at
once, and the client is left unable to provision even though nothing about the
protocol was detected. The relay's address is the CDN's, shared with a large
fraction of the web — blocking it is expensive in a way blocking our own /24 is
not.

## Why it is safe to run on someone else's infrastructure

Every request is an envelope sealed against the hub's pinned X25519 key
(`SERVER_PUBLIC_KEY_B64` in
[`secureChannel.ts`](../../apps/desktop/src/main/secureChannel.ts)), with a fresh
ephemeral client key per request. TLS is only the carrier — which is also why
the no-SNI direct-IP path can set `rejectUnauthorized: false` without weakening
anything.

So Cloudflare terminating TLS in the middle buys it nothing: it relays
ciphertext it cannot read and cannot forge a reply to. What it *can* see is
traffic timing and volume, and that a given client IP talks to this relay. That
is why `fronted` is attempted after our own paths rather than before them.

## Deploy

```sh
cd infra/edge-relay
npx wrangler deploy
```

Note the hostname it prints (`pangea-edge-relay.<subdomain>.workers.dev`).

Deploy **more than one**, on separate accounts and ideally separate CDNs — the
client rotates through the list and promotes whichever answers, so a burned
relay costs one failed probe instead of the whole method. Give each its own
`name` in `wrangler.toml`.

If you put a relay on a custom domain, do not use a name that resembles ours.
The keyword rule that blocks `pangeavpn.org` will block `pangea-relay.dev` just
as readily, and then you have paid for a relay that fails in exactly the
conditions it exists for.

## Telling clients about it

Two ways, and you want both:

1. **Hub-advertised.** Return the hostnames as `frontedEndpoints` on
   `/api/client/bootstrap` and the token-login response. The client validates
   them (host only — no scheme, port or path), caches them to `settings.json`,
   and refreshes on every login. This is how rotation reaches existing installs
   without shipping a release.

2. **Shipped or hand-set**, for the cold-start case. A client that has never
   reached the hub has no cached relay, so a brand-new install behind a block
   still has nothing. Until the hub advertises them, the list can be seeded by
   hand in `settings.json`:

   ```json
   { "frontedEndpoints": ["pangea-edge-relay.example.workers.dev"] }
   ```

Until one of those happens the method is inert: `tryFrontedPath` logs
`no relay configured` and `ensureHub` falls straight through to the next
method. Nothing breaks; the improvement simply is not there yet.

## Operational notes

**The hub sees Cloudflare's IPs, not the client's.** The Worker deliberately
forwards no `CF-Connecting-IP` or `X-Forwarded-For`, so the relay tells the hub
nothing about who is calling.

This costs nothing today: the hub mounts `/v1/secure` ahead of `clientRateLimit`
in `app.js`, and the inner request it re-dispatches carries a `_secureChannel`
marker the limiter skips — so nothing on this path is bucketed by source address
and there is no shared-bucket throttling to design around. Worth re-checking if
that middleware order ever changes; if per-IP limiting does start applying here,
exempt the relay hub-side rather than forwarding the client address, which is
the thing this path exists to keep off the wire.

The flip side is that `/v1/secure` has no rate limit at all, and a relay is one
more public door to it. The Worker's POST-only, single-path, 64 KB ceiling is
the only brake it adds. That door was already open at `api.pangeavpn.org`, so
this is not new exposure — but a limiter keyed on something other than source IP
would be worth having before this sees real traffic.

**Cost.** This carries control-plane traffic only — login, regions, key
registration. No tunnel data ever crosses it. The free tier is far more than
enough; if this relay ever shows meaningful volume, something is misconfigured.
