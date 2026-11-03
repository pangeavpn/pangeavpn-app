package mobile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/hkdf"
	"crypto/mlkem"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// testHub holds the private halves the real hub keeps, so a test can open what
// the client seals and answer it the way the hub does.
type testHub struct {
	x25519 *ecdh.PrivateKey
	kem    *mlkem.DecapsulationKey768
}

func newTestHub(t *testing.T) (testHub, channelKeys) {
	t.Helper()
	x, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	kem, err := mlkem.GenerateKey768()
	if err != nil {
		t.Fatal(err)
	}
	keys := channelKeys{
		x25519Public: x.PublicKey().Bytes(),
		kemPublic:    base64.StdEncoding.EncodeToString(kem.EncapsulationKey().Bytes()),
	}
	return testHub{x25519: x, kem: kem}, keys
}

type wireEnvelope struct {
	Eph string `json:"eph"`
	Kem string `json:"kem"`
	IV  string `json:"iv"`
	CT  string `json:"ct"`
	Tag string `json:"tag"`
}

type wireInner struct {
	Method  string            `json:"method"`
	Route   string            `json:"route"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	TS      *int64            `json:"ts"`
}

func (h testHub) dh(t *testing.T, ephB64 string) []byte {
	t.Helper()
	raw, err := base64.StdEncoding.DecodeString(ephB64)
	if err != nil {
		t.Fatal(err)
	}
	pub, err := ecdh.X25519().NewPublicKey(raw)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := h.x25519.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}
	return shared
}

func hkdf32(t *testing.T, ikm []byte, info string) []byte {
	t.Helper()
	key, err := hkdf.Key(sha256.New, ikm, hkdfSalt, info, 32)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

func gcmOpenB64(t *testing.T, key []byte, iv, ct, tag string, aad []byte) []byte {
	t.Helper()
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	rawIV, _ := base64.StdEncoding.DecodeString(iv)
	rawCT, _ := base64.StdEncoding.DecodeString(ct)
	rawTag, _ := base64.StdEncoding.DecodeString(tag)
	plain, err := gcm.Open(nil, rawIV, append(rawCT, rawTag...), aad)
	if err != nil {
		t.Fatalf("hub could not open the envelope: %v", err)
	}
	return plain
}

func gcmSealB64(t *testing.T, key []byte, plaintext string, aad []byte) encryptedResponse {
	t.Helper()
	block, _ := aes.NewCipher(key)
	gcm, _ := cipher.NewGCM(block)
	iv := make([]byte, gcm.NonceSize())
	_, _ = rand.Read(iv)
	sealed := gcm.Seal(nil, iv, []byte(plaintext), aad)
	split := len(sealed) - gcm.Overhead()
	return encryptedResponse{
		IV:  base64.StdEncoding.EncodeToString(iv),
		CT:  base64.StdEncoding.EncodeToString(sealed[:split]),
		Tag: base64.StdEncoding.EncodeToString(sealed[split:]),
	}
}

func decodeEnvelope(t *testing.T, sealed sealedRequest) wireEnvelope {
	t.Helper()
	var env wireEnvelope
	if err := json.Unmarshal(sealed.envelope, &env); err != nil {
		t.Fatalf("envelope is not JSON: %v", err)
	}
	return env
}

func withClock(t *testing.T, ms int64) {
	t.Helper()
	previous := nowMillis
	nowMillis = func() int64 { return ms }
	t.Cleanup(func() { nowMillis = previous })
}

const replyJSON = `{"status":200,"body":{"ok":true}}`

func TestSealRequestV2RoundTripsThroughTheHub(t *testing.T) {
	hub, keys := newTestHub(t)
	withClock(t, 1_760_000_000_123)

	sealed, err := sealRequestWith(keys, "POST", "/api/register", map[string]string{"X-License-Key": "k"}, []byte(`{"region":"eu"}`))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	if sealed.route != "/v2/secure" {
		t.Fatalf("route = %q, want /v2/secure with a pinned KEM key", sealed.route)
	}

	env := decodeEnvelope(t, sealed)
	kemCT, _ := base64.StdEncoding.DecodeString(env.Kem)
	kemSecret, err := hub.kem.Decapsulate(kemCT)
	if err != nil {
		t.Fatalf("decapsulate: %v", err)
	}
	ikm := append(hub.dh(t, env.Eph), kemSecret...)
	c2s := hkdf32(t, ikm, "pangea-secure-channel-v2/c2s")
	s2c := hkdf32(t, ikm, "pangea-secure-channel-v2/s2c")
	aad := []byte(env.Eph + "." + env.Kem)

	var inner wireInner
	if err := json.Unmarshal(gcmOpenB64(t, c2s, env.IV, env.CT, env.Tag, aad), &inner); err != nil {
		t.Fatal(err)
	}
	if inner.Method != "POST" || inner.Route != "/api/register" || inner.Headers["X-License-Key"] != "k" {
		t.Fatalf("inner request = %+v", inner)
	}
	if string(inner.Body) != `{"region":"eu"}` {
		t.Fatalf("body = %s", inner.Body)
	}
	if inner.TS == nil || *inner.TS != 1_760_000_000_123 {
		t.Fatalf("ts = %v, want the send time in ms", inner.TS)
	}

	got, err := sealed.open(gcmSealB64(t, s2c, replyJSON, aad))
	if err != nil {
		t.Fatalf("open reply: %v", err)
	}
	if got.Status != 200 || string(got.Body) != `{"ok":true}` {
		t.Fatalf("reply = %+v", got)
	}
}

func TestSealRequestV2RefusesItsOwnEnvelopeEchoedBack(t *testing.T) {
	hub, keys := newTestHub(t)
	sealed, err := sealRequestWith(keys, "GET", "/api/client/regions", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, sealed)
	kemCT, _ := base64.StdEncoding.DecodeString(env.Kem)
	kemSecret, _ := hub.kem.Decapsulate(kemCT)
	c2s := hkdf32(t, append(hub.dh(t, env.Eph), kemSecret...), "pangea-secure-channel-v2/c2s")
	aad := []byte(env.Eph + "." + env.Kem)

	if _, err := sealed.open(gcmSealB64(t, c2s, replyJSON, aad)); err == nil {
		t.Fatal("a reply sealed under the request key opened; v2 must keep the directions apart")
	}
}

func TestSealRequestV2RefusesAReplyBoundToAnotherRequest(t *testing.T) {
	hub, keys := newTestHub(t)
	sealed, err := sealRequestWith(keys, "GET", "/api/client/regions", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	env := decodeEnvelope(t, sealed)
	kemCT, _ := base64.StdEncoding.DecodeString(env.Kem)
	kemSecret, _ := hub.kem.Decapsulate(kemCT)
	s2c := hkdf32(t, append(hub.dh(t, env.Eph), kemSecret...), "pangea-secure-channel-v2/s2c")

	if _, err := sealed.open(gcmSealB64(t, s2c, replyJSON, []byte("other.request"))); err == nil {
		t.Fatal("a reply authenticated for a different request opened")
	}
}

func TestSealRequestFallsBackToV1WithoutAPinnedKEMKey(t *testing.T) {
	hub, keys := newTestHub(t)
	keys.kemPublic = ""
	withClock(t, 42)

	sealed, err := sealRequestWith(keys, "GET", "/api/client/regions", map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.route != "/v1/secure" {
		t.Fatalf("route = %q, want /v1/secure", sealed.route)
	}
	env := decodeEnvelope(t, sealed)
	if env.Kem != "" {
		t.Fatal("a v1 envelope must not carry a KEM ciphertext")
	}
	key := hkdf32(t, hub.dh(t, env.Eph), "pangea-secure-channel-v1")

	var inner wireInner
	if err := json.Unmarshal(gcmOpenB64(t, key, env.IV, env.CT, env.Tag, nil), &inner); err != nil {
		t.Fatal(err)
	}
	if inner.TS == nil || *inner.TS != 42 {
		t.Fatalf("ts = %v; the hub may require it on v1 too", inner.TS)
	}
	got, err := sealed.open(gcmSealB64(t, key, replyJSON, nil))
	if err != nil || got.Status != 200 {
		t.Fatalf("open = %+v, %v", got, err)
	}
}

func TestSealRequestFallsBackToV1WhenTheKEMKeyIsUnusable(t *testing.T) {
	_, keys := newTestHub(t)
	keys.kemPublic = base64.StdEncoding.EncodeToString([]byte("too short"))

	sealed, err := sealRequestWith(keys, "GET", "/api/client/regions", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.route != "/v1/secure" {
		t.Fatalf("route = %q, want the v1 fallback", sealed.route)
	}
}

// The Go port pins the same hub keys as the desktop client; a rotation that
// reaches only one of them would cut that platform off the hub.
func TestPinnedKeysMatchTheDesktopClient(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "apps", "desktop", "src", "main", "secureChannel.ts"))
	if err != nil {
		t.Skipf("desktop source not available: %v", err)
	}
	read := func(pattern string) string {
		match := regexp.MustCompile(pattern).FindSubmatch(source)
		if match == nil {
			t.Fatalf("pattern %q not found in secureChannel.ts", pattern)
		}
		return string(match[1])
	}

	if got, want := base64.StdEncoding.EncodeToString(productionKeys().x25519Public),
		read(`SERVER_PUBLIC_KEY_B64 = "([^"]+)"`); got != want {
		t.Errorf("X25519 key = %s, desktop pins %s", got, want)
	}
	if got, want := productionKeys().kemPublic, read(`SERVER_KEM_PUBLIC_KEY_B64 =\s*"([^"]+)"`); got != want {
		t.Errorf("ML-KEM key differs from the desktop pin")
	}
	if got, want := hex.EncodeToString(hkdfSalt), read(`HKDF_SALT = Buffer.from\("([0-9a-f]+)"`); got != want {
		t.Errorf("HKDF salt = %s, desktop uses %s", got, want)
	}
}
