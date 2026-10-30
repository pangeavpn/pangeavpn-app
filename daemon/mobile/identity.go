//go:build android || ios

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

// parseIdentity decodes a stored identity, nil when none is stored. Callers
// treat a corrupt one as absent, so Login generates a fresh identity.
func parseIdentity(raw string) (*identityKeyPair, error) {
	if raw == "" {
		return nil, nil
	}
	var stored storedIdentity
	if err := json.Unmarshal([]byte(raw), &stored); err != nil {
		return nil, fmt.Errorf("decode stored identity: %w", err)
	}
	privRaw, err := base64.StdEncoding.DecodeString(stored.Priv)
	if err != nil {
		return nil, fmt.Errorf("decode stored identity key: %w", err)
	}
	priv, err := ecdh.X25519().NewPrivateKey(privRaw)
	if err != nil {
		return nil, fmt.Errorf("decode stored identity key: %w", err)
	}
	return &identityKeyPair{priv: priv, pubB64: stored.Pub}, nil
}

// publicKey is "" for a missing identity, so two loads compare without nil checks.
func (k *identityKeyPair) publicKey() string {
	if k == nil {
		return ""
	}
	return k.pubB64
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

// forgetIdentity drops the identity only while it is still pub, in memory and
// in the store, so a Login that raced ahead keeps its fresh key.
func forgetIdentity(pub string) {
	if pub == "" {
		return
	}
	mu.Lock()
	st := store
	if identity.publicKey() == pub {
		identity = nil
	}
	mu.Unlock()
	if st == nil {
		return
	}
	if stored, _ := parseIdentity(st.Get(keyIdentity)); stored.publicKey() == pub {
		st.Set(keyIdentity, "")
	}
}
