// Edge relay for the hub's secure channel: a CDN anycast address a censor
// can't cheaply block. Forwards sealed ciphertext only — see README.md.

const HUB_ORIGIN = "https://api.pangeavpn.org";

// The only route relayed — hardcoded rather than proxied, so this can't
// become an open proxy for arbitrary paths/hosts.
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

    // Only what the hub needs — no CDN-added client-identifying headers are
    // copied through. See README.md if the hub ever needs the client address.
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
