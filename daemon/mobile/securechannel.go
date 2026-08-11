//go:build android

package mobile

// Go port of apps/desktop/src/main/secureChannel.ts. Constants, wire format,
// and crypto primitives (X25519 ECDH -> HKDF-SHA256 -> AES-256-GCM) are byte
// identical to the desktop implementation.

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
)

const hkdfInfo = "pangea-secure-channel-v1"

var (
	serverPublicKeyRaw []byte
	hkdfSalt           []byte
)

func init() {
	var err error
	serverPublicKeyRaw, err = base64.StdEncoding.DecodeString("dCdC/tJM0oSQPUDROrrZeGR8VUgww2YPUPHlaDhqWFM=")
	if err != nil {
		panic("mobile: invalid server public key constant: " + err.Error())
	}
	hkdfSalt, err = hex.DecodeString("b9a288d01062a270368f67495ebafcec7eb910bee52855df69b22025cd205ae2")
	if err != nil {
		panic("mobile: invalid hkdf salt constant: " + err.Error())
	}
}

type envelope struct {
	Eph string `json:"eph"`
	IV  string `json:"iv"`
	CT  string `json:"ct"`
	Tag string `json:"tag"`
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
}

// encryptRequest builds the {eph,iv,ct,tag} envelope for one /v1/secure call
// and returns the derived AES key needed to decrypt the matching response.
func encryptRequest(method, route string, headers map[string]string, body []byte) (envelope, []byte, error) {
	curve := ecdh.X25519()
	ephPriv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return envelope{}, nil, fmt.Errorf("generate ephemeral key: %w", err)
	}
	serverPub, err := curve.NewPublicKey(serverPublicKeyRaw)
	if err != nil {
		return envelope{}, nil, fmt.Errorf("parse server public key: %w", err)
	}
	shared, err := ephPriv.ECDH(serverPub)
	if err != nil {
		return envelope{}, nil, fmt.Errorf("ecdh: %w", err)
	}
	aesKey, err := hkdf.Key(sha256.New, shared, hkdfSalt, hkdfInfo, 32)
	if err != nil {
		return envelope{}, nil, fmt.Errorf("hkdf: %w", err)
	}

	if headers == nil {
		headers = map[string]string{}
	}
	plaintext, err := json.Marshal(innerRequest{Method: method, Route: route, Headers: headers, Body: body})
	if err != nil {
		return envelope{}, nil, err
	}

	gcm, err := newGCM(aesKey)
	if err != nil {
		return envelope{}, nil, err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(iv); err != nil {
		return envelope{}, nil, err
	}
	sealed := gcm.Seal(nil, iv, plaintext, nil)
	ct, tag := sealed[:len(sealed)-gcm.Overhead()], sealed[len(sealed)-gcm.Overhead():]

	env := envelope{
		Eph: base64.StdEncoding.EncodeToString(ephPriv.PublicKey().Bytes()),
		IV:  base64.StdEncoding.EncodeToString(iv),
		CT:  base64.StdEncoding.EncodeToString(ct),
		Tag: base64.StdEncoding.EncodeToString(tag),
	}
	return env, aesKey, nil
}

// decryptResponse reverses encryptRequest's AES-256-GCM envelope using the
// AES key derived for the matching request.
func decryptResponse(aesKey []byte, resp encryptedResponse) (innerResponse, error) {
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

	gcm, err := newGCM(aesKey)
	if err != nil {
		return innerResponse{}, err
	}
	if len(iv) != gcm.NonceSize() {
		return innerResponse{}, errors.New("secure channel: invalid nonce size")
	}

	combined := append(append(make([]byte, 0, len(ct)+len(tag)), ct...), tag...)
	plaintext, err := gcm.Open(nil, iv, combined, nil)
	if err != nil {
		return innerResponse{}, fmt.Errorf("secure channel: decrypt failed: %w", err)
	}

	var inner innerResponse
	if err := json.Unmarshal(plaintext, &inner); err != nil {
		return innerResponse{}, fmt.Errorf("decode inner response: %w", err)
	}
	return inner, nil
}

func newGCM(aesKey []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return nil, fmt.Errorf("aes cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm: %w", err)
	}
	return gcm, nil
}
