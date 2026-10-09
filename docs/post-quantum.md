# Post-quantum protection

WireGuard agrees its session keys with X25519. Traffic recorded today could be
decrypted once a large enough quantum computer exists, so the attack worth
defending against is "record now, decrypt later". WireGuard already has a hook
for this: an optional per-peer pre-shared key, mixed into the handshake, so that
recovering the X25519 agreement alone no longer gives up the session keys.

PangeaVPN never sends that key. Both ends derive it from an ML-KEM-768
(FIPS 203) exchange in which only public values cross the wire.

## The exchange

```
daemon                                  exit node (through the hub)
  dk, ek <- ML-KEM-768 keygen
                       -- ek -->
                                        ss, ct <- Encapsulate(ek)
                       <-- ct --
  ss <- Decapsulate(dk, ct)

psk = HKDF-SHA256(ikm = ss, salt = empty, info = "pangea wireguard psk v1", 32 bytes)
```

WireGuard's own X25519 handshake is the classical half, so the pre-shared key
doesn't need a second classical component. If ML-KEM were ever broken, the
tunnel would be exactly as strong as it is without it. ML-KEM already binds its
shared secret to the public key and the ciphertext, so the derivation needs no
extra transcript.

## Where the pieces live

| Step | Where |
|---|---|
| Key generation, decapsulation, derivation | [`daemon/internal/pq`](../daemon/internal/pq), using the Go standard library's `crypto/mlkem` |
| Daemon routes `POST /pq/offer`, `/pq/finish`, `/pq/encapsulate` | [`daemon/internal/api/post_quantum.go`](../daemon/internal/api/post_quantum.go) |
| Carrying the offer to the hub and the key into the config | `provision()` in [`apps/desktop/src/main/pangeaApiClient.ts`](../apps/desktop/src/main/pangeaApiClient.ts) |
| Reading the hub's answer | [`apps/desktop/src/shared/postQuantum.ts`](../apps/desktop/src/shared/postQuantum.ts) |
| `wireguard.postQuantum` in `GET /status` | `wg.HasPresharedKey` over the profile's config |

The daemon keeps the private half for up to five minutes between offer and
finish, and forgets it on the first finish, whether or not the hub answered.
The derived key goes back to the app over the same loopback channel that
already carries the WireGuard private key, and the app writes it into the
`[Peer]` block as `PresharedKey`. The daemon's config converter already turns
that line into the UAPI `preshared_key`, so the device layer didn't need to
change.

The exchange runs once per peer registration, not per packet. WireGuard's
per-packet cipher is untouched.

## Daemon wire format

`POST /pq/offer` takes no body and returns:

```json
{ "id": "<32 hex>", "algorithm": "ml-kem-768", "kemPublicKey": "<1184 bytes, base64>" }
```

`POST /pq/finish` takes `{ "id", "algorithm", "kemCiphertext": "<1088 bytes, base64>" }`
and returns `{ "presharedKey": "<32 bytes, base64>" }`. A wrong or already-spent
id, a different algorithm, or a malformed ciphertext gets a 400.

`POST /pq/encapsulate` takes `{ "algorithm", "kemPublicKey" }` and returns
`{ "kemCiphertext", "sharedSecret" }`. The secure channel uses it against the
hub's pinned key.

## Hub wire format

`/api/register` gains one optional object in each direction. The request
carries the public key, and the response carries the ciphertext:

```json
{ "pq": { "algorithm": "ml-kem-768", "kemPublicKey": "<base64>" } }
{ "pq": { "algorithm": "ml-kem-768", "kemCiphertext": "<base64>" } }
```

The exit node encapsulates and keys the peer. The hub only passes the values
along.

## Compatibility

| Daemon | Hub and node | Result |
|---|---|---|
| old | any | No offer; the peer is unkeyed, as before |
| new | old | Offer ignored, no answer; the peer is unkeyed |
| new | new | The peer is keyed on both ends |

> [!NOTE]
> An answer that's present but unusable fails the connection attempt. The
> alternative would be bringing up a tunnel the node has already keyed, and
> that tunnel would dial forever.

## Hub channel: `/v2/secure`

The same daemon primitive lets the app seal hub requests under a hybrid key:
X25519 against the hub's pinned static key, as in v1, plus ML-KEM-768 against a
second pinned hub key. v2 also uses a separate key for each direction and binds
each reply to its request.

`SERVER_KEM_PUBLIC_KEY_B64` in
[`secureChannel.ts`](../apps/desktop/src/main/secureChannel.ts) is that second
pin, and it's set. The app uses v2 whenever the local daemon can encapsulate,
and v1 otherwise. Only the daemon decides that, so nothing on the network can
force the older route. Emptying the pin would move every request back to
`/v1/secure`.

[Architecture](architecture.md#secure-request-envelope) walks through both
envelope versions step by step.

## Tests

```
cd daemon && go test ./internal/pq ./internal/wg ./internal/api
cd apps/desktop && npm test
```

When a Node with ML-KEM support is on `PATH`, `daemon/internal/pq` also runs the
node agent's exact responder against Node's ML-KEM. Both repositories pin the
same known-answer vectors for every derivation.
