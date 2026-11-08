// Package securechanneltest plays the hub's side of /v2/secure for tests. It
// is imported only by tests, so it never reaches the daemon binary.
package securechanneltest

import (
	"crypto/ecdh"
	"crypto/mlkem"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/securechannel"
)

// Hub holds both static private keys of a stand-in hub.
type Hub struct {
	x25519 *ecdh.PrivateKey
	kem    *mlkem.DecapsulationKey768
}

// Request is a client request as the hub sees it once opened.
type Request struct {
	Method  string            `json:"method"`
	Route   string            `json:"route"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	TS      int64             `json:"ts"`
}

// Reply seals the hub's answer to the request it was opened from.
type Reply func(securechannel.InnerResponse) ([]byte, error)

// NewHub makes a hub with fresh keys and returns the keys a client seals for.
func NewHub() (*Hub, securechannel.Keys, error) {
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, securechannel.Keys{}, err
	}
	kem, err := mlkem.GenerateKey768()
	if err != nil {
		return nil, securechannel.Keys{}, err
	}
	keys := securechannel.Keys{
		X25519Public: x.PublicKey().Bytes(),
		KEMPublic:    base64.StdEncoding.EncodeToString(kem.EncapsulationKey().Bytes()),
	}
	return &Hub{x25519: x, kem: kem}, keys, nil
}

// Open decrypts a client envelope, as the hub's /v2/secure route does.
func (h *Hub) Open(envelope []byte) (Request, Reply, error) {
	var env struct{ Eph, Kem, IV, CT, Tag string }
	if err := json.Unmarshal(envelope, &env); err != nil {
		return Request{}, nil, fmt.Errorf("decode envelope: %w", err)
	}
	ephRaw, err := base64.StdEncoding.DecodeString(env.Eph)
	if err != nil {
		return Request{}, nil, err
	}
	eph, err := ecdh.X25519().NewPublicKey(ephRaw)
	if err != nil {
		return Request{}, nil, err
	}
	dhSecret, err := h.x25519.ECDH(eph)
	if err != nil {
		return Request{}, nil, err
	}
	kemRaw, err := base64.StdEncoding.DecodeString(env.Kem)
	if err != nil {
		return Request{}, nil, err
	}
	kemSecret, err := h.kem.Decapsulate(kemRaw)
	if err != nil {
		return Request{}, nil, err
	}
	c2s, s2c, err := securechannel.DeriveKeys(dhSecret, kemSecret)
	if err != nil {
		return Request{}, nil, err
	}
	aad := securechannel.AssociatedData(env.Eph, env.Kem)
	plaintext, err := securechannel.OpenRequest(c2s, env.IV, env.CT, env.Tag, aad)
	if err != nil {
		return Request{}, nil, err
	}
	var req Request
	if err := json.Unmarshal(plaintext, &req); err != nil {
		return Request{}, nil, err
	}
	reply := func(inner securechannel.InnerResponse) ([]byte, error) {
		return securechannel.SealReply(s2c, inner, aad)
	}
	return req, reply, nil
}
