package mobile

// The client half of the post-quantum WireGuard pre-shared key, run in-process
// around /api/register. Ports apps/desktop/src/shared/postQuantum.ts.

import (
	"bytes"
	"crypto/mlkem"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
)

// pqOffer is the register body's "pq" block; the exit node encapsulates to it.
type pqOffer struct {
	Algorithm    string `json:"algorithm"`
	KEMPublicKey string `json:"kemPublicKey"`
}

// pqAnswer is the node's half, carried back in the register response.
type pqAnswer struct {
	Algorithm     string `json:"algorithm"`
	KEMCiphertext string `json:"kemCiphertext"`
}

// newPQOffer uses StdEncoding: the hub re-encodes the key and rejects any
// non-canonical form.
func newPQOffer(dk *mlkem.DecapsulationKey768) pqOffer {
	return pqOffer{
		Algorithm:    pq.Algorithm,
		KEMPublicKey: base64.StdEncoding.EncodeToString(dk.EncapsulationKey().Bytes()),
	}
}

// parsePQAnswer is nil when nothing was offered or nobody answered. A present
// but unusable answer errors: the node has keyed its side of the peer.
func parsePQAnswer(offered bool, raw json.RawMessage) (*pqAnswer, error) {
	trimmed := bytes.TrimSpace(raw)
	if !offered || len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil, nil
	}
	var answer pqAnswer
	if trimmed[0] != '{' || json.Unmarshal(trimmed, &answer) != nil {
		return nil, errors.New("server answered the post-quantum offer with a malformed block")
	}
	if answer.KEMCiphertext == "" {
		return nil, errors.New("server answered the post-quantum offer without a ciphertext")
	}
	if answer.Algorithm != pq.Algorithm {
		return nil, fmt.Errorf("server answered the post-quantum offer with %q, not %s", answer.Algorithm, pq.Algorithm)
	}
	return &answer, nil
}

// finishPQ decapsulates the node's ciphertext and derives the key it installed.
func finishPQ(dk *mlkem.DecapsulationKey768, answer *pqAnswer) ([]byte, error) {
	ciphertext, err := base64.StdEncoding.DecodeString(answer.KEMCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decode post-quantum ciphertext: %w", err)
	}
	if len(ciphertext) != pq.CiphertextSize {
		return nil, fmt.Errorf("post-quantum ciphertext is %d bytes, want %d", len(ciphertext), pq.CiphertextSize)
	}
	shared, err := dk.Decapsulate(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("decapsulate: %w", err)
	}
	return pq.DerivePresharedKey(shared)
}

// presharedKeyFrom is nil, nil for an unkeyed peer: no offer (dk nil) or no answer.
func presharedKeyFrom(dk *mlkem.DecapsulationKey768, raw json.RawMessage) ([]byte, error) {
	answer, err := parsePQAnswer(dk != nil, raw)
	if err != nil || answer == nil {
		return nil, err
	}
	return finishPQ(dk, answer)
}
