/**
 * Edge relay for the hub's secure channel.
 *
 * The client's four other paths to the hub all terminate on address space we
 * own, so one enumeration sweep that blackholes it takes out every one of them
 * at once. This relay exists to have an address the censor cannot cheaply
 * blackhole: a CDN anycast IP shared with enough of the web that dropping it
 * costs them far more than it costs us.
 *
 * It is a dumb forwarder and is trusted with nothing. Every request it carries
 * is an envelope sealed against the hub's pinned X25519 key (see
 * secureChannel.ts), so it moves ciphertext it cannot read and cannot forge a
 * reply to. That is the whole reason this is safe to run on someone else's
 * infrastructure.
 *
 * Deploy: see README.md in this directory.
 */

const HUB_ORIGIN = "https://api.pangeavpn.org";

// The only route worth relaying. Hardcoded rather than proxying whatever path
// arrives: a relay that forwards arbitrary paths to arbitrary hosts is an open
// proxy, and it would be found and abused within days of going up.
const RELAY_PATH = "/v1/secure";

// Envelopes are small — a few KB at most. Anything larger is not our client.
const MAX_BODY_BYTES = 64 * 1024;

const UPSTREAM_TIMEOUT_MS = 20_000;

export default {
  async fetch(request) {
    const url = new URL(request.url);

    if (url.pathname !== RELAY_PATH) {
      return new Response("Not found", { status: 404 });
    }
    if (request.method !== "POST") {
      return new Response("Method not allowed", { status: 405, headers: { Allow: "POST" } });
    }

    const body = await request.arrayBuffer();
    if (body.byteLength === 0 || body.byteLength > MAX_BODY_BYTES) {
      return new Response("Bad request", { status: 400 });
    }

    // Only what the hub needs. Nothing is copied from the incoming request:
    // headers a CDN adds on the way in (CF-Connecting-IP, CF-IPCountry, the
    // trace headers) would tell the hub things this path exists precisely to
    // avoid putting on the wire. See README.md if per-IP rate limiting on the
    // hub needs the client address back.
    let upstream;
    try {
      upstream = await fetch(`${HUB_ORIGIN}${RELAY_PATH}`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body,
        signal: AbortSignal.timeout(UPSTREAM_TIMEOUT_MS)
      });
    } catch {
      // Deliberately vague: the client only needs to know this path failed so
      // it can fall through to the next one.
      return new Response("Bad gateway", { status: 502 });
    }

    // Streamed straight back. The status matters — the client treats a non-2xx
    // as "this path is dead, try the next" — but the body is opaque to us.
    return new Response(upstream.body, {
      status: upstream.status,
      headers: {
        "Content-Type": "application/json",
        "Cache-Control": "no-store"
      }
    });
  }
};
