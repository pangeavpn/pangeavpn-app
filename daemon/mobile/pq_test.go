package mobile

import (
	"bytes"
	"crypto/mlkem"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os/exec"
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

// nodePQCiphertext and nodePQKey are what the node agent's responder (Node's
// ML-KEM and HKDF) answered to the encapsulation key of nodePQSeed.
var (
	nodePQCiphertext = "" +
		"1nmGE13XrshQSgez5sUTLdTBkDdk7+UgyuJxZXfCVfFJTx+Yudi+nxH6EZXUnpESh0KJ63fx0N3MQup5jjbpeIIbvwJyFgge" +
		"oKkQWXbpDltvjl3emxfWIhpZp0FY2ZNhDcikAfxpjSdmI7vi4prT/Q9jnQ7Cxpjatx0yVhmdQloiZ2vWxkCxrymzdkCPdLOV" +
		"wjttnWch5SI5t++wZvSVVVcLkItzIYiHQc+ruRuXH3qP31EhzCme53IFSS+T5IoNHlMJ2w9O9sZHhZ+nWD+oBfuvHaGwnQ3E" +
		"vtltARMvBKri2fEnDLLbsomgRNTH5jYibZXMH/eccRnK9vF4Pxldclk4OOPCeWtXTJcc6Zb4RpjiXfsK13B1fyPsfLdsplBj" +
		"urpEXCqPl8qRxsV/sMRQTkGqJlnhzhnnZBEGSZEf+tssexQbqjuGNsmv6TDLTszNYperFL2Srpwt+cYTmyys+7Aaqc4CdhHP" +
		"vRq0hPIYtRZGAXgJpf4l0rTGTxYEBYkyqopRUaWef3zoRw1DRAUiuuy6HC5i0hMvhQfcuRMuTg1Jokj+9+sXb3q7p6WL0WxI" +
		"1/KHucOAM5jVXmJBs7LpUa55pxRtZsrtcNndNA9P/QiJhOJHDvUAoGCGKx9XRGmGL/9efuxg3NfpqqFIH4xvLGykvYTmO75S" +
		"yue3sv0XPDXItJ6R/PxuFyLSkIQJAHLSyvuWm7Wt7ymke8eljkzK3ES2e1E85FS8faCgcfkLUmt8DEb2U7Z00PMyFcXHK5Vu" +
		"2PQJwO60KsinPX/jctdytxJe+GUx+zuJHc9MzU7+/xl2oTQkmP4ou0aJRESAymb3E1d7vq4tzynUsmcvmYEybt4Y7Cax6EuS" +
		"odCaTEsL/hhWMyCfvQcG2XOm4u+oE72CrYTqTUnlAnYgvu+NC7l8iG9vR0mvtF7SWSa+hZKDxc8Qz66tqIB7MimKkI2q/DE2" +
		"42WWFe+8kZgggNJGTSzKDzJZ0fkroIDisgMjOcr5SET9TpS2iACcYpSvUftX5KnJzWVJdh28Y+3tdj+XR+e+iOHOyYTer3lW" +
		"kcrjAoG8fbc3KT+R2W2IW49rvqcluASPivo/ekQkIRSbVkK7nDKWJBleNW6+cL6fyZQInx4bTLIqwrcizp6FmlfZDV1DDrzq" +
		"TCbewUBo5MVzzPht+K6nmIoyPk+GcDYeZT34zOMteMxo9zCgkQwd0U2ywq9gIud8kaMCldHmJvy/2VQmVayjHNUh4yokhOfv" +
		"2hriK8b1X7NI79pd4NYVCXTVPvn4+wT8M39cCS9gIUcbnHBsk+D07RJTgXTRtNFmVDfGSxJbxUMEf+0f2WncykHkBYlQ6vzP" +
		"kuglAYkHvrZcqBVFWDwwZJyEZOYgXVW6hB7LXS35AXJm8opHNdqY4e8eOKTCcUy8WDbVYsICl8eATnpJ/vwqD9OOU9vL1876" +
		"UP0nUSb/+q0="
	nodePQKey = "995d6f48d5a4252e05cc74ac9b644ad0a00e84c75cf3ca7bd8169be7485821c4"
)

func nodePQSeed() []byte {
	seed := make([]byte, mlkem.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	return seed
}

func TestFinishPQAgreesWithTheNode(t *testing.T) {
	dk, err := mlkem.NewDecapsulationKey768(nodePQSeed())
	if err != nil {
		t.Fatalf("NewDecapsulationKey768: %v", err)
	}
	answer, err := parsePQAnswer(true, json.RawMessage(`{"algorithm":"ml-kem-768","kemCiphertext":"`+nodePQCiphertext+`"}`))
	if err != nil {
		t.Fatalf("parsePQAnswer: %v", err)
	}

	got, err := finishPQ(dk, answer)
	if err != nil {
		t.Fatalf("finishPQ: %v", err)
	}
	if hex.EncodeToString(got) != nodePQKey {
		t.Fatalf("got %x, want the node's %s", got, nodePQKey)
	}
}

// Runs the node agent's responder live when a Node with ML-KEM is on PATH,
// the same script daemon/internal/pq checks its own half against.
func TestFinishPQAgreesWithALiveNode(t *testing.T) {
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
	dk := mustDecapsulationKey(t)
	out, err := exec.Command("node", "-e", script, newPQOffer(dk).KEMPublicKey).Output()
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
	got, err := finishPQ(dk, &pqAnswer{Algorithm: pq.Algorithm, KEMCiphertext: reply.CT})
	if err != nil {
		t.Fatalf("finishPQ: %v", err)
	}
	if hex.EncodeToString(got) != reply.PSK {
		t.Fatalf("mobile derived %x, Node derived %s", got, reply.PSK)
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
