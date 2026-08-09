package reality

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// KeyPair is an X25519 REALITY key pair. PrivateKey configures the server
// side (kept secret); PublicKey is distributed to clients. Both are
// base64.RawURLEncoding, matching sing-box's option.*RealityOptions field
// encoding (see common/tls/reality_{client,server}.go).
type KeyPair struct {
	PrivateKey string
	PublicKey  string
}

// GenerateKeyPair creates a fresh REALITY X25519 key pair.
func GenerateKeyPair() (KeyPair, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return KeyPair{}, fmt.Errorf("reality: generate key pair: %w", err)
	}
	return KeyPair{
		PrivateKey: base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		PublicKey:  base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
	}, nil
}

// GenerateShortID returns a random REALITY short ID, hex-encoded. REALITY
// short IDs are at most 8 bytes (16 hex chars).
func GenerateShortID(byteLen int) (string, error) {
	if byteLen <= 0 || byteLen > 8 {
		byteLen = 8
	}
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("reality: generate short id: %w", err)
	}
	return fmt.Sprintf("%x", buf), nil
}
