//go:build android

package mobile

// X25519 device identity keypair, persisted via SecretStore under the
// "identityKey" key as {"priv":"b64","pub":"b64"} (mirrors desktop auth.ts).

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type identityKeyPair struct {
	priv   *ecdh.PrivateKey
	pubB64 string
}

type storedIdentity struct {
	Priv string `json:"priv"`
	Pub  string `json:"pub"`
}

// loadIdentity returns the previously persisted identity, or nil if none is
// stored yet. A corrupt stored value is treated the same as absent: Login
// will generate and persist a fresh identity.
func loadIdentity(s SecretStore) *identityKeyPair {
	raw := s.Get(keyIdentity)
	if raw == "" {
		return nil
	}
	var stored storedIdentity
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil
	}
	privRaw, err := base64.StdEncoding.DecodeString(stored.Priv)
	if err != nil {
		return nil
	}
	priv, err := ecdh.X25519().NewPrivateKey(privRaw)
	if err != nil {
		return nil
	}
	return &identityKeyPair{priv: priv, pubB64: stored.Pub}
}

func generateIdentity() (*identityKeyPair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate identity key: %w", err)
	}
	pubB64 := base64.StdEncoding.EncodeToString(priv.PublicKey().Bytes())
	return &identityKeyPair{priv: priv, pubB64: pubB64}, nil
}

func (k *identityKeyPair) marshal() string {
	privB64 := base64.StdEncoding.EncodeToString(k.priv.Bytes())
	b, _ := json.Marshal(storedIdentity{Priv: privB64, Pub: k.pubB64})
	return string(b)
}
