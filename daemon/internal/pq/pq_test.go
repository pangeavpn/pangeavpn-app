package pq

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOfferAndFinishAgreeWithResponder(t *testing.T) {
	exchanges := NewExchanges()
	offer, err := exchanges.Offer()
	if err != nil {
		t.Fatal(err)
	}
	if offer.Algorithm != Algorithm || offer.ID == "" {
		t.Fatalf("offer = %+v", offer)
	}
	if raw, _ := base64.StdEncoding.DecodeString(offer.KEMPublicKey); len(raw) != EncapsulationKeySize {
		t.Fatalf("encapsulation key is %d bytes, want %d", len(raw), EncapsulationKeySize)
	}

	ciphertext, shared, err := Encapsulate(offer.Algorithm, offer.KEMPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	want, err := DerivePresharedKey(shared)
	if err != nil {
		t.Fatal(err)
	}

	got, err := exchanges.Finish(offer.ID, Answer{Algorithm: Algorithm, KEMCiphertext: ciphertext})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) || len(got) != PresharedKeySize {
		t.Fatalf("preshared key mismatch: got %x want %x", got, want)
	}
}

func TestFinishIsSingleUse(t *testing.T) {
	exchanges := NewExchanges()
	offer, _ := exchanges.Offer()
	ciphertext, _, _ := Encapsulate(Algorithm, offer.KEMPublicKey)
	answer := Answer{Algorithm: Algorithm, KEMCiphertext: ciphertext}

	if _, err := exchanges.Finish(offer.ID, answer); err != nil {
		t.Fatal(err)
	}
	if _, err := exchanges.Finish(offer.ID, answer); !errors.Is(err, ErrUnknownOffer) {
		t.Fatalf("second finish err = %v, want ErrUnknownOffer", err)
	}
}

func TestFinishRejectsBadAnswersAndBurnsTheOffer(t *testing.T) {
	exchanges := NewExchanges()
	offer, _ := exchanges.Offer()
	ciphertext, _, _ := Encapsulate(Algorithm, offer.KEMPublicKey)

	_, err := exchanges.Finish(offer.ID, Answer{Algorithm: "rsa", KEMCiphertext: ciphertext})
	if !errors.Is(err, ErrAlgorithm) {
		t.Fatalf("wrong algorithm err = %v, want ErrAlgorithm", err)
	}
	_, err = exchanges.Finish(offer.ID, Answer{Algorithm: Algorithm, KEMCiphertext: ciphertext})
	if !errors.Is(err, ErrUnknownOffer) {
		t.Fatalf("offer survived a bad answer: err = %v", err)
	}

	offer, _ = exchanges.Offer()
	short := base64.StdEncoding.EncodeToString(make([]byte, CiphertextSize-1))
	if _, err := exchanges.Finish(offer.ID, Answer{Algorithm: Algorithm, KEMCiphertext: short}); err == nil {
		t.Fatal("short ciphertext accepted")
	}
	if _, err := exchanges.Finish("nope", Answer{}); !errors.Is(err, ErrUnknownOffer) {
		t.Fatalf("unknown id err = %v", err)
	}
}

func TestTamperedCiphertextYieldsDifferentKey(t *testing.T) {
	exchanges := NewExchanges()
	offer, _ := exchanges.Offer()
	ciphertext, shared, _ := Encapsulate(Algorithm, offer.KEMPublicKey)
	want, _ := DerivePresharedKey(shared)

	raw, _ := base64.StdEncoding.DecodeString(ciphertext)
	raw[0] ^= 0x01
	got, err := exchanges.Finish(offer.ID, Answer{Algorithm: Algorithm, KEMCiphertext: base64.StdEncoding.EncodeToString(raw)})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(got, want) {
		t.Fatal("tampered ciphertext produced the responder's key")
	}
}

func TestOffersExpireAndAreCapped(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	exchanges := NewExchanges()
	exchanges.now = func() time.Time { return now }

	offer, _ := exchanges.Offer()
	ciphertext, _, _ := Encapsulate(Algorithm, offer.KEMPublicKey)
	now = now.Add(OfferTTL + time.Second)
	if _, err := exchanges.Finish(offer.ID, Answer{Algorithm: Algorithm, KEMCiphertext: ciphertext}); !errors.Is(err, ErrUnknownOffer) {
		t.Fatalf("expired offer err = %v, want ErrUnknownOffer", err)
	}

	first, _ := exchanges.Offer()
	for i := 0; i < maxPending; i++ {
		now = now.Add(time.Millisecond)
		if _, err := exchanges.Offer(); err != nil {
			t.Fatal(err)
		}
	}
	if len(exchanges.pending) != maxPending {
		t.Fatalf("pending = %d, want %d", len(exchanges.pending), maxPending)
	}
	if _, ok := exchanges.pending[first.ID]; ok {
		t.Fatal("oldest offer survived eviction")
	}
}

func TestEncapsulateRejectsBadInput(t *testing.T) {
	if _, _, err := Encapsulate("ml-kem-1024", ""); !errors.Is(err, ErrAlgorithm) {
		t.Fatalf("err = %v, want ErrAlgorithm", err)
	}
	short := base64.StdEncoding.EncodeToString(make([]byte, 10))
	if _, _, err := Encapsulate(Algorithm, short); err == nil {
		t.Fatal("short key accepted")
	}
	if _, _, err := Encapsulate(Algorithm, "not base64!"); err == nil {
		t.Fatal("bad base64 accepted")
	}
}

// The node agents derive the key with Node's HKDF; this vector pins both ends.
func TestDerivePresharedKeyVector(t *testing.T) {
	got, err := DerivePresharedKey(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	const want = "dfc93885e6d13ebf994246d5581400d69658ee607440c53eaa15c46a8329267c"
	if hex.EncodeToString(got) != want {
		t.Fatalf("vector = %x, want %s", got, want)
	}
	if _, err := DerivePresharedKey(make([]byte, 31)); err == nil {
		t.Fatal("short shared secret accepted")
	}
}

// Runs the node agent's exact responder code against Node's ML-KEM when a
// capable Node is on PATH, so a Go/OpenSSL disagreement fails here first.
func TestFinishAgreesWithNodeResponder(t *testing.T) {
	const script = `
const c = require("node:crypto");
if (typeof c.encapsulate !== "function") { process.stdout.write("unsupported"); process.exit(0); }
const raw = Buffer.from(process.argv[1], "base64");
const spki = Buffer.concat([Buffer.from("308204b2300b0609608648016503040402038204a100", "hex"), raw]);
const key = c.createPublicKey({ key: spki, format: "der", type: "spki" });
const { sharedKey, ciphertext } = c.encapsulate(key);
const psk = c.hkdfSync("sha256", sharedKey, "", "pangea wireguard psk v1", 32);
process.stdout.write(JSON.stringify({ ct: ciphertext.toString("base64"), psk: Buffer.from(psk).toString("hex") }));
`
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not on PATH")
	}
	exchanges := NewExchanges()
	offer, _ := exchanges.Offer()

	out, err := exec.Command("node", "-e", script, offer.KEMPublicKey).Output()
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	if strings.TrimSpace(string(out)) == "unsupported" {
		t.Skip("node on PATH has no ML-KEM support")
	}
	var reply struct {
		CT  string `json:"ct"`
		PSK string `json:"psk"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		t.Fatalf("node output %q: %v", out, err)
	}

	got, err := exchanges.Finish(offer.ID, Answer{Algorithm: Algorithm, KEMCiphertext: reply.CT})
	if err != nil {
		t.Fatal(err)
	}
	if hex.EncodeToString(got) != reply.PSK {
		t.Fatalf("Go derived %x, Node derived %s", got, reply.PSK)
	}
}
