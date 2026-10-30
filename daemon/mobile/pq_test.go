package mobile

import (
	"bytes"
	"crypto/mlkem"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/pq"
)

func mustDecapsulationKey(t *testing.T) *mlkem.DecapsulationKey768 {
	t.Helper()
	dk, err := mlkem.GenerateKey768()
	if err != nil {
		t.Fatalf("GenerateKey768: %v", err)
	}
	return dk
}

// nodeAnswer plays the exit node: encapsulate to the offer and derive the PSK.
func nodeAnswer(t *testing.T, offer pqOffer) (json.RawMessage, []byte) {
	t.Helper()
	ciphertext, shared, err := pq.Encapsulate(offer.Algorithm, offer.KEMPublicKey)
	if err != nil {
		t.Fatalf("Encapsulate: %v", err)
	}
	psk, err := pq.DerivePresharedKey(shared)
	if err != nil {
		t.Fatalf("DerivePresharedKey: %v", err)
	}
	raw, err := json.Marshal(pqAnswer{Algorithm: pq.Algorithm, KEMCiphertext: ciphertext})
	if err != nil {
		t.Fatalf("marshal answer: %v", err)
	}
	return raw, psk
}

func TestParsePQAnswer(t *testing.T) {
	valid := `{"algorithm":"ml-kem-768","kemCiphertext":"AA=="}`
	tests := []struct {
		name    string
		offered bool
		raw     string
		want    *pqAnswer
		wantErr string
	}{
		{"no offer", false, valid, nil, ""},
		{"absent", true, "", nil, ""},
		{"null", true, "null", nil, ""},
		{"not an object", true, `"x"`, nil, "malformed"},
		{"an array", true, `[1]`, nil, "malformed"},
		{"no ciphertext", true, `{}`, nil, "without a ciphertext"},
		{"empty ciphertext", true, `{"algorithm":"ml-kem-768","kemCiphertext":""}`, nil, "without a ciphertext"},
		{"another algorithm", true, `{"algorithm":"ml-kem-1024","kemCiphertext":"AA=="}`, nil, "ml-kem-1024"},
		{"valid", true, valid, &pqAnswer{Algorithm: pq.Algorithm, KEMCiphertext: "AA=="}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePQAnswer(tt.offered, json.RawMessage(tt.raw))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want an error containing %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("got %v, want no error", err)
			}
			if (got == nil) != (tt.want == nil) || (got != nil && *got != *tt.want) {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestFinishPQAgreesWithTheNode(t *testing.T) {
	dk := mustDecapsulationKey(t)
	raw, nodePSK := nodeAnswer(t, newPQOffer(dk))
	answer, err := parsePQAnswer(true, raw)
	if err != nil {
		t.Fatalf("parsePQAnswer: %v", err)
	}

	got, err := finishPQ(dk, answer)
	if err != nil {
		t.Fatalf("finishPQ: %v", err)
	}
	if len(got) != pq.PresharedKeySize || !bytes.Equal(got, nodePSK) {
		t.Fatalf("got %x, want the node's %x", got, nodePSK)
	}
}

func TestFinishPQRejectsAShortCiphertext(t *testing.T) {
	dk := mustDecapsulationKey(t)
	short := base64.StdEncoding.EncodeToString(make([]byte, pq.CiphertextSize-1))
	for name, ciphertext := range map[string]string{"short": short, "not base64": "!!"} {
		t.Run(name, func(t *testing.T) {
			if psk, err := finishPQ(dk, &pqAnswer{Algorithm: pq.Algorithm, KEMCiphertext: ciphertext}); err == nil {
				t.Fatalf("got %x, want an error", psk)
			}
		})
	}
}

func TestPresharedKeyFromAnAnswer(t *testing.T) {
	dk := mustDecapsulationKey(t)
	raw, nodePSK := nodeAnswer(t, newPQOffer(dk))

	if psk, err := presharedKeyFrom(nil, raw); psk != nil || err != nil {
		t.Fatalf("without an offer got %x, %v, want nil, nil", psk, err)
	}
	if psk, err := presharedKeyFrom(dk, nil); psk != nil || err != nil {
		t.Fatalf("unanswered got %x, %v, want nil, nil", psk, err)
	}
	if _, err := presharedKeyFrom(dk, json.RawMessage(`{}`)); err == nil {
		t.Fatal("a broken answer was accepted")
	}
	psk, err := presharedKeyFrom(dk, raw)
	if err != nil || !bytes.Equal(psk, nodePSK) {
		t.Fatalf("got %x, %v, want the node's %x", psk, err, nodePSK)
	}
}

// The hub re-encodes the key and refuses anything that does not match byte for byte.
func TestPQOfferIsCanonical(t *testing.T) {
	offer := newPQOffer(mustDecapsulationKey(t))
	if offer.Algorithm != pq.Algorithm {
		t.Fatalf("got algorithm %q, want %q", offer.Algorithm, pq.Algorithm)
	}
	raw, err := base64.StdEncoding.Strict().DecodeString(offer.KEMPublicKey)
	if err != nil {
		t.Fatalf("decode kemPublicKey: %v", err)
	}
	if len(raw) != pq.EncapsulationKeySize {
		t.Fatalf("got %d bytes, want %d", len(raw), pq.EncapsulationKeySize)
	}
	if again := base64.StdEncoding.EncodeToString(raw); again != offer.KEMPublicKey {
		t.Fatal("kemPublicKey does not re-encode to itself")
	}
}

func TestDecodeConfigDefaultsPostQuantumOff(t *testing.T) {
	if decodeConfig("").PostQuantum {
		t.Fatal("post-quantum should default off")
	}
	if decodeConfig(`{"mtu":1380}`).PostQuantum {
		t.Fatal("a blob written before the setting existed should get the default")
	}
	if !decodeConfig(`{"postQuantum":true}`).PostQuantum {
		t.Fatal("an explicit on must survive")
	}
}

func TestEncodeConfigRoundTripsPostQuantum(t *testing.T) {
	original := defaultConfig()
	original.PostQuantum = true
	encoded, err := encodeConfig(original)
	if err != nil {
		t.Fatalf("encodeConfig: %v", err)
	}
	if !strings.Contains(encoded, `"postQuantum":true`) {
		t.Fatalf("encoded blob missing postQuantum: %s", encoded)
	}
	if !decodeConfig(encoded).PostQuantum {
		t.Fatalf("round trip lost postQuantum: %s", encoded)
	}
}
