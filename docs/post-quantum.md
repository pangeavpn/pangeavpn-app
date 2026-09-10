# Post-quantum protection

WireGuard agrees its session keys with X25519. A recording made today can be
decrypted once a large enough quantum computer exists, so the protection worth
having is against recording now and decrypting later. WireGuard's own answer is
the optional per-peer pre-shared key, which is mixed into the handshake so that
recovering the X25519 agreement alone no longer yields the session keys.

The pre-shared key is never sent. Both ends derive it from an ML-KEM-768
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
carries no second classical component: a broken ML-KEM leaves the tunnel exactly
as strong as it is today. ML-KEM binds its shared secret to the public key and
the ciphertext, so the derivation needs no further transcript.

## Where the pieces live

| Step | Where |
|---|---|
| Key generation, decapsulation, derivation | [`daemon/internal/pq`](../daemon/internal/pq), Go standard library `crypto/mlkem` |
| Daemon routes `POST /pq/offer`, `/pq/finish`, `/pq/encapsulate` | [`daemon/internal/api/post_quantum.go`](../daemon/internal/api/post_quantum.go) |
| Carrying the offer to the hub and the key into the config | `provision()` in [`apps/desktop/src/main/pangeaApiClient.ts`](../apps/desktop/src/main/pangeaApiClient.ts) |
| Reading the hub's answer | [`apps/desktop/src/shared/postQuantum.ts`](../apps/desktop/src/shared/postQuantum.ts) |
| `wireguard.postQuantum` in `GET /status` | `wg.HasPresharedKey` over the profile's config |

The daemon holds the private half for five minutes between offer and finish
and forgets it on the first finish, answered or not. The derived key goes back
to the app over the same loopback channel that already carries the WireGuard
private key, and the app writes it into the `[Peer]` block as `PresharedKey`.
The daemon's config converter already turns that line into the UAPI
`preshared_key`, so the device layer needed no change.

The exchange runs once per peer registration, not per packet; WireGuard's
per-packet cipher is untouched.

## Daemon wire format

`POST /pq/offer` (no body) returns

```json
{ "id": "<32 hex>", "algorithm": "ml-kem-768", "kemPublicKey": "<1184 bytes, base64>" }
```

`POST /pq/finish` takes `{ "id", "algorithm", "kemCiphertext": "<1088 bytes, base64>" }`
and returns `{ "presharedKey": "<32 bytes, base64>" }`. A wrong or spent id, a
different algorithm or a malformed ciphertext is a 400.

`POST /pq/encapsulate` takes `{ "algorithm", "kemPublicKey" }` and returns
`{ "kemCiphertext", "sharedSecret" }`; the secure channel uses it against the
hub's pinned key.

## Hub wire format

`/api/register` gains one optional object each way:

```json
{ "pq": { "algorithm": "ml-kem-768", "kemPublicKey": "<base64>" } }
{ "pq": { "algorithm": "ml-kem-768", "kemCiphertext": "<base64>" } }
```

The exit node encapsulates and keys the peer; the hub only forwards.

## Compatibility

| Daemon | Hub and node | Result |
|---|---|---|
| old | any | no offer, peer unkeyed as before |
| new | old | offer ignored, no answer, peer unkeyed |
| new | new | peer keyed on both ends |

A present but unusable answer fails the connection attempt instead of bringing
up a tunnel the node has already keyed, which would dial forever.

## Hub channel: `/v2/secure`

The same daemon primitive lets the app seal hub requests under a hybrid key:
X25519 against the hub's pinned static key, as in v1, plus ML-KEM-768 against
a second pinned hub key, with separate keys per direction and the reply bound
to its request. `SERVER_KEM_PUBLIC_KEY_B64` in
[`secureChannel.ts`](../apps/desktop/src/main/secureChannel.ts) is that second
pin; while it is empty every request stays on `/v1/secure`. Once it is set, the
app uses v2 whenever the local daemon can encapsulate and v1 otherwise. Only the
daemon decides that, so nothing on the network can force the older route.

## Tests

```
cd daemon && go test ./internal/pq ./internal/wg ./internal/api
cd apps/desktop && npm test
```

`daemon/internal/pq` also runs the node agent's exact responder against Node's
ML-KEM when a capable Node is on PATH, and both repositories pin the same
known-answer vectors for every derivation.
