// Package securechannel seals a hub request for /v2/secure: an ephemeral
// X25519 agreement with the hub's pinned key plus an ML-KEM-768 encapsulation
// to its second pinned key, HKDF-SHA256 into one AES-256-GCM key per
// direction, and the reply bound to its request.
//
// The wire format and constants are byte identical to
// apps/desktop/src/main/secureChannel.ts and daemon/mobile/securechannel.go.
// There is no v1 here: a request that cannot be sealed for v2 is not sent.
package securechannel

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
)

// Route is the hub path every sealed envelope is posted to.
const Route = "/v2/secure"

const (
	hkdfInfoC2S         = "pangea-secure-channel-v2/c2s"
	hkdfInfoS2C         = "pangea-secure-channel-v2/s2c"
	kemSharedSecretSize = 32
)

// The hub's pinned keys. They must match SERVER_PUBLIC_KEY_B64 and
// SERVER_KEM_PUBLIC_KEY_B64 on desktop and the mobile core's constants.
const (
	hubX25519PublicB64 = "dCdC/tJM0oSQPUDROrrZeGR8VUgww2YPUPHlaDhqWFM="
	hubKEMPublicB64    = "+VGdrHyFbqe1VCAIXhqVUKClytKNWNWF/ewL9SBBcNieILpWldUaXKyLxMxEZROHnFiDgRAfJ0cKfUh6a1Y6bIhDIZddpMc0kWHPbIUmHahUspeVHEbOICq7WZd7JIHIFCmY7gTMinDLcLLJXtJyOpk6XUAT4IQd3fNPVcuqeziGeZfPZeG1aeVYaXUR18Kv2VbHDGUp/fslJKMrv7jKYGR0CGHHNfucsrMF4GlVRYcSkXJHwao78pydQZEP+xYqDZkXEopAk0k94ORoJ1MmeUUZXFk47Lur58QxSXSeuzNrTcsdkNiomJK9m+mATsu1DluGOfyhuexzRrp8zWIPmzqpkLeG/PQOksVrOPKwd9OAEkavX9s6GVdBpad3bPsFdcxBNxt9GddxX9nAH4ybwqdoZtBcjksILlOMkMJb/MjL2ZkdjlIV+ZDNj/EJeIxl3tlqlMC2cewlZ9QBhHeOSsy47Fp50ChcifxcH/Z847grJCEeWeJ4RlTNqXMq3MY+v/bOldMWdfyacRG5+GdahZQM+fVQI8JmLiZd3kuKhPk15RGbMQADgCk9MJpUWkhxoewyBTbI3Oy659gXzxRq8IC0o1AvQroYBFI0+0pLWugF/sc7Yfl+6FOZo3TGqwm6PZbHtsEHKYdVV0OUiahC06K2lEPHGKBEQay6pIwVE5M4Twp+igzNrNjD17F9u9k2rmRvBhldPqq2Z0cnmrIsvqRhM2x+0bmH5ltXDBh3OEtOfmkNRiIHTfiqZnMS23hVArABO2MZoCMb4ppzzdR32vsOdfsBdNW9b4cmtNWtfcpf+LCKnpQJlmwe2SiVizMqUci/YtubJAtWpVA5A/isgdWbdRwWstwrLzt7+DTAkrQ7AgEbbCiwcyZL9eUjKAUfKBE5/ugD3UKx/lqDLZcP/vpLpFN+7llq/ARbFEmO9jQqUOhlB/uN5Ll7s+hb1PZCiUQXbWqpQ3UkVjWH57Ib21dkuiKbXqFefGJLU4Nbt7VQVMSsdWK5cnlY5PtYRqsO+DaTPXyf8YKAq5h3MYZ+TKl6E6AbIVkyHSh48QvP+EJ1WkGvihWRieG5EfG1qlRAsOkvScMxuXF66mt2ZiBZAO3Ax5YxvYIgjsibDSQ+v8pf9CZ4guwnBek3u8UXFGNR4mcuktkndvJzmwxatDAsTZkP1CR6MJFtSSMUaWxd0wureDy1TIU2ZGUSOhqje6KuyAJlysu959IxnqnEIURQBoFhL9yIVXcKKrmLctEhGSItPkUnRnI1AnLFEwTL/vGH82pdntfKBqRLRXdvYTWcIbUi1peaRQifvIdTJuK/u5hGxBBIVvaZ7uLCRSx34uUoMtKmfOhb7vJvcLvG96LGuLs+ICxMPhoRDBZtsORR+HgEhNItoWKm7aZBAXSM6AOujLy18EwSZVZdWuYfm+UQ7jkxv5eLeOg0ueAG6QReySF9BDErtYW3iIs/GYpExfKdMiTK2beC1RhbaLFRb6EkiiIUuQoEkDd/8sdUEelAVpZWvCwNngudPMgPzOx7G+FUUyXRUq8XMc/PUOlMmE3Y8Vp4XWxR9BF6mtpYbalmXlY="
)

var hkdfSalt = mustDecodeHex("b9a288d01062a270368f67495ebafcec7eb910bee52855df69b22025cd205ae2")

// Keys are the public halves of a hub's two static keys.
type Keys struct {
	X25519Public []byte
	KEMPublic    string
}

// HubKeys returns the production hub's pinned keys.
func HubKeys() Keys {
	raw, err := base64.StdEncoding.DecodeString(hubX25519PublicB64)
	if err != nil {
		panic("securechannel: invalid hub public key constant: " + err.Error())
	}
	return Keys{X25519Public: raw, KEMPublic: hubKEMPublicB64}
}

// InnerResponse is the hub's answer once opened.
type InnerResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

type innerRequest struct {
	Method  string            `json:"method"`
	Route   string            `json:"route"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body,omitempty"`
	// The send time rides inside the ciphertext: the hub refuses envelopes
	// sealed outside its window, so a captured request cannot be replayed.
	TS int64 `json:"ts"`
}

type encryptedReply struct {
	IV  string `json:"iv"`
	CT  string `json:"ct"`
	Tag string `json:"tag"`
}

// Sealed is one request sealed for Route, and the only key that opens its reply.
type Sealed struct {
	// Envelope is the JSON body to POST to Route.
	Envelope []byte
	s2c      []byte
	aad      []byte
}

// Seal seals method and route (query included) for keys. body may be nil.
func Seal(keys Keys, method, route string, headers map[string]string, body []byte) (Sealed, error) {
	if headers == nil {
		headers = map[string]string{}
	}
	plaintext, err := json.Marshal(innerRequest{
		Method: method, Route: route, Headers: headers, Body: body, TS: time.Now().UnixMilli(),
	})
	if err != nil {
		return Sealed{}, err
	}
	kemCiphertext, kemSecret, err := pq.Encapsulate(pq.Algorithm, keys.KEMPublic)
	if err != nil {
		return Sealed{}, err
	}
	if len(kemSecret) != kemSharedSecretSize {
		return Sealed{}, fmt.Errorf("securechannel: KEM shared secret is %d bytes, want %d", len(kemSecret), kemSharedSecretSize)
	}
	ephB64, dhSecret, err := ephemeralAgreement(keys.X25519Public)
	if err != nil {
		return Sealed{}, err
	}
	c2s, s2c, err := DeriveKeys(dhSecret, kemSecret)
	if err != nil {
		return Sealed{}, err
	}
	aad := AssociatedData(ephB64, kemCiphertext)
	iv, ct, tag, err := gcmSeal(c2s, plaintext, aad)
	if err != nil {
		return Sealed{}, err
	}
	envelope, err := json.Marshal(map[string]string{"eph": ephB64, "kem": kemCiphertext, "iv": iv, "ct": ct, "tag": tag})
	if err != nil {
		return Sealed{}, err
	}
	return Sealed{Envelope: envelope, s2c: s2c, aad: aad}, nil
}

// Open reads the hub's reply body. It fails for anything the hub did not seal
// for this very request.
func (s Sealed) Open(reply []byte) (InnerResponse, error) {
	var enc encryptedReply
	if err := json.Unmarshal(reply, &enc); err != nil {
		return InnerResponse{}, fmt.Errorf("securechannel: decode reply: %w", err)
	}
	plaintext, err := gcmOpen(s.s2c, enc, s.aad)
	if err != nil {
		return InnerResponse{}, err
	}
	var inner InnerResponse
	if err := json.Unmarshal(plaintext, &inner); err != nil {
		return InnerResponse{}, fmt.Errorf("securechannel: decode inner response: %w", err)
	}
	return inner, nil
}

// DeriveKeys is the v2 key schedule; exported so a test can play the hub.
func DeriveKeys(dhSecret, kemSecret []byte) (c2s, s2c []byte, err error) {
	ikm := append(append(make([]byte, 0, len(dhSecret)+len(kemSecret)), dhSecret...), kemSecret...)
	if c2s, err = hkdf.Key(sha256.New, ikm, hkdfSalt, hkdfInfoC2S, 32); err != nil {
		return nil, nil, fmt.Errorf("securechannel: hkdf c2s: %w", err)
	}
	if s2c, err = hkdf.Key(sha256.New, ikm, hkdfSalt, hkdfInfoS2C, 32); err != nil {
		return nil, nil, fmt.Errorf("securechannel: hkdf s2c: %w", err)
	}
	return c2s, s2c, nil
}

// AssociatedData is what both directions authenticate, tying a reply to its request.
func AssociatedData(ephB64, kemB64 string) []byte {
	return []byte(ephB64 + "." + kemB64)
}

// SealReply seals a hub answer under key; exported so a test can play the hub.
func SealReply(key []byte, inner InnerResponse, aad []byte) ([]byte, error) {
	plaintext, err := json.Marshal(inner)
	if err != nil {
		return nil, err
	}
	iv, ct, tag, err := gcmSeal(key, plaintext, aad)
	if err != nil {
		return nil, err
	}
	return json.Marshal(encryptedReply{IV: iv, CT: ct, Tag: tag})
}

// OpenRequest opens a client envelope's ciphertext under key; exported so a
// test can play the hub.
func OpenRequest(key []byte, iv, ct, tag string, aad []byte) ([]byte, error) {
	return gcmOpen(key, encryptedReply{IV: iv, CT: ct, Tag: tag}, aad)
}

func ephemeralAgreement(serverPublic []byte) (string, []byte, error) {
	curve := ecdh.X25519()
	serverPub, err := curve.NewPublicKey(serverPublic)
	if err != nil {
		return "", nil, fmt.Errorf("securechannel: parse hub public key: %w", err)
	}
	ephPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("securechannel: generate ephemeral key: %w", err)
	}
	shared, err := ephPriv.ECDH(serverPub)
	if err != nil {
		return "", nil, fmt.Errorf("securechannel: ecdh: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ephPriv.PublicKey().Bytes()), shared, nil
}

func gcmSeal(key, plaintext, aad []byte) (iv, ct, tag string, err error) {
	gcm, err := newGCM(key)
	if err != nil {
		return "", "", "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", "", "", err
	}
	sealed := gcm.Seal(nil, nonce, plaintext, aad)
	split := len(sealed) - gcm.Overhead()
	enc := base64.StdEncoding.EncodeToString
	return enc(nonce), enc(sealed[:split]), enc(sealed[split:]), nil
}

func gcmOpen(key []byte, enc encryptedReply, aad []byte) ([]byte, error) {
	dec := base64.StdEncoding.DecodeString
	iv, err := dec(enc.IV)
	if err != nil {
		return nil, fmt.Errorf("securechannel: decode iv: %w", err)
	}
	ct, err := dec(enc.CT)
	if err != nil {
		return nil, fmt.Errorf("securechannel: decode ct: %w", err)
	}
	tag, err := dec(enc.Tag)
	if err != nil {
		return nil, fmt.Errorf("securechannel: decode tag: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != gcm.NonceSize() {
		return nil, errors.New("securechannel: invalid nonce size")
	}
	combined := append(append(make([]byte, 0, len(ct)+len(tag)), ct...), tag...)
	plaintext, err := gcm.Open(nil, iv, combined, aad)
	if err != nil {
		return nil, fmt.Errorf("securechannel: decrypt failed: %w", err)
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("securechannel: aes: %w", err)
	}
	return cipher.NewGCM(block)
}

func mustDecodeHex(value string) []byte {
	raw, err := hex.DecodeString(value)
	if err != nil {
		panic("securechannel: invalid hex constant: " + err.Error())
	}
	return raw
}
