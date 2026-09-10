// Package pq runs the client half of an ML-KEM-768 exchange whose result is
// mixed into WireGuard as the peer's pre-shared key.
package pq

import (
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Algorithm names the KEM on the wire; a different KEM gets a different name.
const Algorithm = "ml-kem-768"

const (
	EncapsulationKeySize = mlkem.EncapsulationKeySize768
	CiphertextSize       = mlkem.CiphertextSize768
	PresharedKeySize     = 32
)

// presharedKeyInfo is the HKDF label every node must reproduce byte for byte.
const presharedKeyInfo = "pangea wireguard psk v1"

// OfferTTL bounds how long a decapsulation key waits for the hub's answer.
const OfferTTL = 5 * time.Minute

const maxPending = 16

var (
	ErrAlgorithm    = errors.New("pq: unsupported algorithm")
	ErrUnknownOffer = errors.New("pq: unknown or expired offer")
)

// Offer is the public half the app carries to the hub.
type Offer struct {
	ID           string `json:"id"`
	Algorithm    string `json:"algorithm"`
	KEMPublicKey string `json:"kemPublicKey"`
}

// Answer is what the node sends back through the hub.
type Answer struct {
	Algorithm     string `json:"algorithm"`
	KEMCiphertext string `json:"kemCiphertext"`
}

type pendingOffer struct {
	key       *mlkem.DecapsulationKey768
	expiresAt time.Time
}

// Exchanges holds decapsulation keys between an offer and its answer.
type Exchanges struct {
	mu      sync.Mutex
	pending map[string]pendingOffer
	now     func() time.Time
}

func NewExchanges() *Exchanges {
	return &Exchanges{pending: map[string]pendingOffer{}, now: time.Now}
}

// Offer generates a fresh key pair and returns the half the hub needs.
func (e *Exchanges) Offer() (Offer, error) {
	key, err := mlkem.GenerateKey768()
	if err != nil {
		return Offer{}, fmt.Errorf("pq: generate key: %w", err)
	}
	var idBytes [16]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return Offer{}, fmt.Errorf("pq: generate offer id: %w", err)
	}
	id := hex.EncodeToString(idBytes[:])

	e.mu.Lock()
	defer e.mu.Unlock()
	e.expireLocked()
	if len(e.pending) >= maxPending {
		e.evictOldestLocked()
	}
	e.pending[id] = pendingOffer{key: key, expiresAt: e.now().Add(OfferTTL)}

	return Offer{
		ID:           id,
		Algorithm:    Algorithm,
		KEMPublicKey: base64.StdEncoding.EncodeToString(key.EncapsulationKey().Bytes()),
	}, nil
}

// Finish derives the pre-shared key from the answer and forgets the offer
// either way: a second attempt against the same key is never legitimate.
func (e *Exchanges) Finish(id string, answer Answer) ([]byte, error) {
	e.mu.Lock()
	e.expireLocked()
	pending, ok := e.pending[id]
	delete(e.pending, id)
	e.mu.Unlock()
	if !ok {
		return nil, ErrUnknownOffer
	}
	if answer.Algorithm != Algorithm {
		return nil, fmt.Errorf("%w: %q", ErrAlgorithm, answer.Algorithm)
	}
	ciphertext, err := decodeFixed(answer.KEMCiphertext, CiphertextSize, "ciphertext")
	if err != nil {
		return nil, err
	}
	shared, err := pending.key.Decapsulate(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("pq: decapsulate: %w", err)
	}
	return DerivePresharedKey(shared)
}

// Encapsulate is the responder half: the secure channel runs it against the
// hub's static key, and tests run it in place of a node.
func Encapsulate(algorithm, kemPublicKey string) (ciphertext string, shared []byte, err error) {
	if algorithm != Algorithm {
		return "", nil, fmt.Errorf("%w: %q", ErrAlgorithm, algorithm)
	}
	raw, err := decodeFixed(kemPublicKey, EncapsulationKeySize, "encapsulation key")
	if err != nil {
		return "", nil, err
	}
	key, err := mlkem.NewEncapsulationKey768(raw)
	if err != nil {
		return "", nil, fmt.Errorf("pq: invalid encapsulation key: %w", err)
	}
	shared, ct := key.Encapsulate()
	return base64.StdEncoding.EncodeToString(ct), shared, nil
}

// DerivePresharedKey is the one derivation the nodes must match exactly.
func DerivePresharedKey(shared []byte) ([]byte, error) {
	if len(shared) != mlkem.SharedKeySize {
		return nil, fmt.Errorf("pq: shared secret is %d bytes, want %d", len(shared), mlkem.SharedKeySize)
	}
	key, err := hkdf.Key(sha256.New, shared, nil, presharedKeyInfo, PresharedKeySize)
	if err != nil {
		return nil, fmt.Errorf("pq: derive preshared key: %w", err)
	}
	return key, nil
}

func (e *Exchanges) expireLocked() {
	now := e.now()
	for id, pending := range e.pending {
		if now.After(pending.expiresAt) {
			delete(e.pending, id)
		}
	}
}

func (e *Exchanges) evictOldestLocked() {
	oldestID := ""
	var oldest time.Time
	for id, pending := range e.pending {
		if oldestID == "" || pending.expiresAt.Before(oldest) {
			oldestID, oldest = id, pending.expiresAt
		}
	}
	delete(e.pending, oldestID)
}

func decodeFixed(value string, size int, what string) ([]byte, error) {
	raw, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("pq: decode %s: %w", what, err)
	}
	if len(raw) != size {
		return nil, fmt.Errorf("pq: %s is %d bytes, want %d", what, len(raw), size)
	}
	return raw, nil
}
