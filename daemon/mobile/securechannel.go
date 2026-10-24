package mobile

// Go port of apps/desktop/src/main/secureChannel.ts. Constants, wire format,
// and crypto primitives are byte identical to the desktop implementation.

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

const (
	hkdfInfo      = "pangea-secure-channel-v1"
	hkdfInfoV2C2S = "pangea-secure-channel-v2/c2s"
	hkdfInfoV2S2C = "pangea-secure-channel-v2/s2c"

	routeSecureV1 = "/v1/secure"
	routeSecureV2 = "/v2/secure"

	kemSharedSecretSize = 32
)

const serverPublicKeyB64 = "dCdC/tJM0oSQPUDROrrZeGR8VUgww2YPUPHlaDhqWFM="

// serverKEMPublicKeyB64 is the hub's ML-KEM-768 key; empty keeps every request
// on /v1/secure. Must match SERVER_KEM_PUBLIC_KEY_B64 on desktop.
const serverKEMPublicKeyB64 = "+VGdrHyFbqe1VCAIXhqVUKClytKNWNWF/ewL9SBBcNieILpWldUaXKyLxMxEZROHnFiDgRAfJ0cKfUh6a1Y6bIhDIZddpMc0kWHPbIUmHahUspeVHEbOICq7WZd7JIHIFCmY7gTMinDLcLLJXtJyOpk6XUAT4IQd3fNPVcuqeziGeZfPZeG1aeVYaXUR18Kv2VbHDGUp/fslJKMrv7jKYGR0CGHHNfucsrMF4GlVRYcSkXJHwao78pydQZEP+xYqDZkXEopAk0k94ORoJ1MmeUUZXFk47Lur58QxSXSeuzNrTcsdkNiomJK9m+mATsu1DluGOfyhuexzRrp8zWIPmzqpkLeG/PQOksVrOPKwd9OAEkavX9s6GVdBpad3bPsFdcxBNxt9GddxX9nAH4ybwqdoZtBcjksILlOMkMJb/MjL2ZkdjlIV+ZDNj/EJeIxl3tlqlMC2cewlZ9QBhHeOSsy47Fp50ChcifxcH/Z847grJCEeWeJ4RlTNqXMq3MY+v/bOldMWdfyacRG5+GdahZQM+fVQI8JmLiZd3kuKhPk15RGbMQADgCk9MJpUWkhxoewyBTbI3Oy659gXzxRq8IC0o1AvQroYBFI0+0pLWugF/sc7Yfl+6FOZo3TGqwm6PZbHtsEHKYdVV0OUiahC06K2lEPHGKBEQay6pIwVE5M4Twp+igzNrNjD17F9u9k2rmRvBhldPqq2Z0cnmrIsvqRhM2x+0bmH5ltXDBh3OEtOfmkNRiIHTfiqZnMS23hVArABO2MZoCMb4ppzzdR32vsOdfsBdNW9b4cmtNWtfcpf+LCKnpQJlmwe2SiVizMqUci/YtubJAtWpVA5A/isgdWbdRwWstwrLzt7+DTAkrQ7AgEbbCiwcyZL9eUjKAUfKBE5/ugD3UKx/lqDLZcP/vpLpFN+7llq/ARbFEmO9jQqUOhlB/uN5Ll7s+hb1PZCiUQXbWqpQ3UkVjWH57Ib21dkuiKbXqFefGJLU4Nbt7VQVMSsdWK5cnlY5PtYRqsO+DaTPXyf8YKAq5h3MYZ+TKl6E6AbIVkyHSh48QvP+EJ1WkGvihWRieG5EfG1qlRAsOkvScMxuXF66mt2ZiBZAO3Ax5YxvYIgjsibDSQ+v8pf9CZ4guwnBek3u8UXFGNR4mcuktkndvJzmwxatDAsTZkP1CR6MJFtSSMUaWxd0wureDy1TIU2ZGUSOhqje6KuyAJlysu959IxnqnEIURQBoFhL9yIVXcKKrmLctEhGSItPkUnRnI1AnLFEwTL/vGH82pdntfKBqRLRXdvYTWcIbUi1peaRQifvIdTJuK/u5hGxBBIVvaZ7uLCRSx34uUoMtKmfOhb7vJvcLvG96LGuLs+ICxMPhoRDBZtsORR+HgEhNItoWKm7aZBAXSM6AOujLy18EwSZVZdWuYfm+UQ7jkxv5eLeOg0ueAG6QReySF9BDErtYW3iIs/GYpExfKdMiTK2beC1RhbaLFRb6EkiiIUuQoEkDd/8sdUEelAVpZWvCwNngudPMgPzOx7G+FUUyXRUq8XMc/PUOlMmE3Y8Vp4XWxR9BF6mtpYbalmXlY="

var hkdfSalt = mustDecodeHex("b9a288d01062a270368f67495ebafcec7eb910bee52855df69b22025cd205ae2")

// nowMillis stamps the send time the hub checks against its replay window.
var nowMillis = func() int64 { return time.Now().UnixMilli() }

// channelKeys are the hub's public halves; tests swap in keys they hold.
type channelKeys struct {
	x25519Public []byte
	kemPublic    string
}

func productionKeys() channelKeys {
	raw, err := base64.StdEncoding.DecodeString(serverPublicKeyB64)
	if err != nil {
		panic("mobile: invalid server public key constant: " + err.Error())
	}
	return channelKeys{x25519Public: raw, kemPublic: serverKEMPublicKeyB64}
}

func mustDecodeHex(value string) []byte {
	raw, err := hex.DecodeString(value)
	if err != nil {
		panic("mobile: invalid hex constant: " + err.Error())
	}
	return raw
}

type encryptedResponse struct {
	IV  string `json:"iv"`
	CT  string `json:"ct"`
	Tag string `json:"tag"`
}

type innerResponse struct {
	Status int             `json:"status"`
	Body   json.RawMessage `json:"body"`
}

type innerRequest struct {
	Method  string            `json:"method"`
	Route   string            `json:"route"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body,omitempty"`
	TS      int64             `json:"ts"`
}

// sealedRequest is one request sealed for a secure route, and the only key
// that can read the hub's answer to it.
type sealedRequest struct {
	route    string
	envelope []byte
	open     func(encryptedResponse) (innerResponse, error)
}

// sealRequest seals for the production hub: v2 while a KEM key is pinned.
func sealRequest(method, route string, headers map[string]string, body []byte) (sealedRequest, error) {
	return sealRequestWith(productionKeys(), method, route, headers, body)
}

// sealRequestWith falls back to v1 only when the pinned key is unusable
// locally, so nothing on the network can push the client down to v1.
func sealRequestWith(keys channelKeys, method, route string, headers map[string]string, body []byte) (sealedRequest, error) {
	plaintext, err := innerRequestJSON(method, route, headers, body)
	if err != nil {
		return sealedRequest{}, err
	}
	if keys.kemPublic != "" {
		if sealed, err := sealV2(keys, plaintext); err == nil {
			return sealed, nil
		}
	}
	return sealV1(keys, plaintext)
}

func innerRequestJSON(method, route string, headers map[string]string, body []byte) ([]byte, error) {
	if headers == nil {
		headers = map[string]string{}
	}
	return json.Marshal(innerRequest{Method: method, Route: route, Headers: headers, Body: body, TS: nowMillis()})
}

// ephemeralAgreement is a fresh X25519 key per request against the pinned hub key.
func ephemeralAgreement(serverPublic []byte) (string, []byte, error) {
	curve := ecdh.X25519()
	serverPub, err := curve.NewPublicKey(serverPublic)
	if err != nil {
		return "", nil, fmt.Errorf("parse server public key: %w", err)
	}
	ephPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return "", nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	shared, err := ephPriv.ECDH(serverPub)
	if err != nil {
		return "", nil, fmt.Errorf("ecdh: %w", err)
	}
	return base64.StdEncoding.EncodeToString(ephPriv.PublicKey().Bytes()), shared, nil
}

func sealV1(keys channelKeys, plaintext []byte) (sealedRequest, error) {
	ephB64, shared, err := ephemeralAgreement(keys.x25519Public)
	if err != nil {
		return sealedRequest{}, err
	}
	key, err := hkdf.Key(sha256.New, shared, hkdfSalt, hkdfInfo, 32)
	if err != nil {
		return sealedRequest{}, fmt.Errorf("hkdf: %w", err)
	}
	sealed, err := gcmSeal(key, plaintext, nil)
	if err != nil {
		return sealedRequest{}, err
	}
	envelope, err := json.Marshal(map[string]string{"eph": ephB64, "iv": sealed.IV, "ct": sealed.CT, "tag": sealed.Tag})
	if err != nil {
		return sealedRequest{}, err
	}
	return sealedRequest{
		route:    routeSecureV1,
		envelope: envelope,
		open:     func(resp encryptedResponse) (innerResponse, error) { return gcmOpenInner(key, resp, nil) },
	}, nil
}

// sealV2 is the hybrid route: each direction has its own key, and both bind
// eph.kem as associated data so a reply only opens for its own request.
func sealV2(keys channelKeys, plaintext []byte) (sealedRequest, error) {
	kemCiphertext, kemSecret, err := pq.Encapsulate(pq.Algorithm, keys.kemPublic)
	if err != nil {
		return sealedRequest{}, err
	}
	if len(kemSecret) != kemSharedSecretSize {
		return sealedRequest{}, fmt.Errorf("KEM shared secret is %d bytes, want %d", len(kemSecret), kemSharedSecretSize)
	}
	ephB64, shared, err := ephemeralAgreement(keys.x25519Public)
	if err != nil {
		return sealedRequest{}, err
	}
	c2s, s2c, err := deriveV2Keys(shared, kemSecret)
	if err != nil {
		return sealedRequest{}, err
	}
	aad := []byte(ephB64 + "." + kemCiphertext)
	sealed, err := gcmSeal(c2s, plaintext, aad)
	if err != nil {
		return sealedRequest{}, err
	}
	envelope, err := json.Marshal(map[string]string{
		"eph": ephB64, "kem": kemCiphertext, "iv": sealed.IV, "ct": sealed.CT, "tag": sealed.Tag,
	})
	if err != nil {
		return sealedRequest{}, err
	}
	return sealedRequest{
		route:    routeSecureV2,
		envelope: envelope,
		open:     func(resp encryptedResponse) (innerResponse, error) { return gcmOpenInner(s2c, resp, aad) },
	}, nil
}

func deriveV2Keys(dhSecret, kemSecret []byte) ([]byte, []byte, error) {
	ikm := append(append(make([]byte, 0, len(dhSecret)+len(kemSecret)), dhSecret...), kemSecret...)
	c2s, err := hkdf.Key(sha256.New, ikm, hkdfSalt, hkdfInfoV2C2S, 32)
	if err != nil {
		return nil, nil, fmt.Errorf("hkdf c2s: %w", err)
	}
	s2c, err := hkdf.Key(sha256.New, ikm, hkdfSalt, hkdfInfoV2S2C, 32)
	if err != nil {
		return nil, nil, fmt.Errorf("hkdf s2c: %w", err)
	}
	return c2s, s2c, nil
}

func gcmSeal(key, plaintext, aad []byte) (encryptedResponse, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return encryptedResponse{}, err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return encryptedResponse{}, err
	}
	sealed := gcm.Seal(nil, iv, plaintext, aad)
	split := len(sealed) - gcm.Overhead()
	return encryptedResponse{
		IV:  base64.StdEncoding.EncodeToString(iv),
		CT:  base64.StdEncoding.EncodeToString(sealed[:split]),
		Tag: base64.StdEncoding.EncodeToString(sealed[split:]),
	}, nil
}

func gcmOpenInner(key []byte, resp encryptedResponse, aad []byte) (innerResponse, error) {
	iv, err := base64.StdEncoding.DecodeString(resp.IV)
	if err != nil {
		return innerResponse{}, fmt.Errorf("decode iv: %w", err)
	}
	ct, err := base64.StdEncoding.DecodeString(resp.CT)
	if err != nil {
		return innerResponse{}, fmt.Errorf("decode ct: %w", err)
	}
	tag, err := base64.StdEncoding.DecodeString(resp.Tag)
	if err != nil {
		return innerResponse{}, fmt.Errorf("decode tag: %w", err)
	}
	gcm, err := newGCM(key)
	if err != nil {
		return innerResponse{}, err
	}
	if len(iv) != gcm.NonceSize() {
		return innerResponse{}, errors.New("secure channel: invalid nonce size")
	}
	combined := append(append(make([]byte, 0, len(ct)+len(tag)), ct...), tag...)
	plaintext, err := gcm.Open(nil, iv, combined, aad)
	if err != nil {
		return innerResponse{}, fmt.Errorf("secure channel: decrypt failed: %w", err)
	}
	var inner innerResponse
	if err := json.Unmarshal(plaintext, &inner); err != nil {
		return innerResponse{}, fmt.Errorf("decode inner response: %w", err)
	}
	return inner, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return gcm, nil
}
