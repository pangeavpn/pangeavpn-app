package mobile

import (
	"bytes"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type namesSealedVector struct {
	Plain  string `json:"plain"`
	Nonce  string `json:"nonce_hex"`
	AAD    string `json:"aad"`
	Sealed string `json:"sealed"`
}

type namesVectors struct {
	Salt                string              `json:"salt"`
	NameKeyHex          string              `json:"names_key_hex"`
	AccountNumber       string              `json:"account_number"`
	AccountNumberInputs []string            `json:"account_number_inputs"`
	NamesSecret         string              `json:"names_secret_b64url"`
	DeviceNameKey       string              `json:"device_name_key"`
	DeviceID            string              `json:"device_id"`
	OtherDeviceID       string              `json:"other_device_id"`
	NumberWrapKeyHex    string              `json:"number_wrap_key_hex"`
	SecretSealKeyHex    string              `json:"secret_seal_key_hex"`
	ServerWrapKeyHex    string              `json:"server_wrap_key_hex"`
	NameKey             namesSealedVector   `json:"name_key"`
	AccountNumberSealed namesSealedVector   `json:"account_number_sealed"`
	NameKeyServer       namesSealedVector   `json:"name_key_server"`
	SealedNames         []namesSealedVector `json:"sealed_names"`
}

func loadNamesVectors(t *testing.T) namesVectors {
	t.Helper()
	raw, err := os.ReadFile("testdata/names_v2_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v namesVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestNamesVectorsWrapTheKeyUnderTheNumber(t *testing.T) {
	v := loadNamesVectors(t)
	if v.Salt != namesSalt {
		t.Fatalf("salt = %q", v.Salt)
	}
	for _, input := range append([]string{v.AccountNumber}, v.AccountNumberInputs...) {
		if got := normalizeAccountNumber(input); got != v.AccountNumber {
			t.Fatalf("normalize(%q) = %q", input, got)
		}
		if got := hex.EncodeToString(numberWrapKey(input)); got != v.NumberWrapKeyHex {
			t.Fatalf("numberWrapKey(%q) = %s", input, got)
		}
	}
	wrapKey, k := numberWrapKey(v.AccountNumber), mustHex(t, v.NameKeyHex)
	sealed, err := sealNamesValueWith(nameKeyPrefix, wrapKey, mustHex(t, v.NameKey.Nonce), k, v.NameKey.AAD)
	if err != nil || sealed != v.NameKey.Sealed {
		t.Fatalf("wrapped = %q, %v", sealed, err)
	}
	if got := unwrapNameKey(wrapKey, v.NameKey.Sealed); !bytes.Equal(got, k) {
		t.Fatalf("unwrapped = %x", got)
	}
}

func TestNamesVectorsSealEveryName(t *testing.T) {
	v := loadNamesVectors(t)
	k := mustHex(t, v.NameKeyHex)
	for _, tc := range v.SealedNames {
		if tc.AAD != v.DeviceID {
			t.Fatalf("vector aad = %q", tc.AAD)
		}
		sealed, err := sealNamesValueWith(sealedNamePrefix, k, mustHex(t, tc.Nonce), []byte(tc.Plain), tc.AAD)
		if err != nil || sealed != tc.Sealed {
			t.Fatalf("sealed %q = %q, %v", tc.Plain, sealed, err)
		}
		if name, ok := openName(k, v.DeviceID, tc.Sealed); !ok || name != tc.Plain {
			t.Fatalf("opened = %q, %v", name, ok)
		}
		if name, ok := openName(k, strings.ToUpper(v.DeviceID), tc.Sealed); !ok || name != tc.Plain {
			t.Fatalf("an uppercase id opened %q, %v", name, ok)
		}
		if name, ok := openName(k, v.OtherDeviceID, tc.Sealed); ok || name != "" {
			t.Fatalf("another device's id opened %q", name)
		}
	}
}

func TestNamesVectorsServerValues(t *testing.T) {
	v := loadNamesVectors(t)
	secret, err := base64.RawURLEncoding.DecodeString(v.NamesSecret)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, prefix string
		ikm          []byte
		info, keyHex string
		plain        []byte
		vec          namesSealedVector
	}{
		{"sn1", "sn1:", secret, "seal:number:v1", v.SecretSealKeyHex, []byte(v.AccountNumber), v.AccountNumberSealed},
		{"ws1", "ws1:", []byte(v.DeviceNameKey), "wrap:server:v1", v.ServerWrapKeyHex, mustHex(t, v.NameKeyHex), v.NameKeyServer},
	}
	for _, tc := range cases {
		key, err := hkdf.Key(sha256.New, tc.ikm, []byte(namesSalt), tc.info, 32)
		if err != nil || hex.EncodeToString(key) != tc.keyHex {
			t.Fatalf("%s key = %x, %v", tc.name, key, err)
		}
		sealed, err := sealNamesValueWith(tc.prefix, key, mustHex(t, tc.vec.Nonce), tc.plain, tc.vec.AAD)
		if err != nil || sealed != tc.vec.Sealed {
			t.Fatalf("%s sealed = %q, %v", tc.name, sealed, err)
		}
		if got := openNamesValue(tc.prefix, key, tc.vec.Sealed, tc.vec.AAD); !bytes.Equal(got, tc.plain) {
			t.Fatalf("%s opened = %q", tc.name, got)
		}
	}
}

func TestNumberWrapKeyIsOnlyForAnAccountNumber(t *testing.T) {
	want := loadNamesVectors(t).NumberWrapKeyHex
	for _, same := range []string{" 0123 4567 89ab cdef ghjk mnpq\n", "o123-4567-89AB-CDEF-GHJK-MNPQ", string(rune(0xfeff)) + "0123456789abcdefghjkmnpq" + string(rune(0xa0))} {
		if got := hex.EncodeToString(numberWrapKey(same)); got != want {
			t.Fatalf("numberWrapKey(%q) = %s", same, got)
		}
	}
	if got := normalizeAccountNumber("i1l1-oO0"); got != "1111000" {
		t.Fatalf("look-alikes = %q", got)
	}
	for _, legacy := range []string{"1234567890123456", "", "lk", "0123456789ABCDEFGHJKMNPU", "0123456789ABCDEFGHJKMNPQ0", "0123_4567_89AB_CDEF_GHJK_MNPQ", "0123456789ABCDEFGHJKMNP\u0085Q"} {
		if numberWrapKey(legacy) != nil {
			t.Fatalf("numberWrapKey(%q) is not nil", legacy)
		}
	}
}

func TestNamesSealWithAFreshNonceAndRoundTrip(t *testing.T) {
	v := loadNamesVectors(t)
	wrapKey := numberWrapKey(v.AccountNumber)
	k := newNameKey()
	if len(k) != 32 || bytes.Equal(k, newNameKey()) {
		t.Fatalf("newNameKey = %x", k)
	}
	wrapped, err := wrapNameKey(wrapKey, k)
	if err != nil || !bytes.Equal(unwrapNameKey(wrapKey, wrapped), k) {
		t.Fatalf("wrap round trip = %q, %v", wrapped, err)
	}
	a, errA := sealName(k, v.DeviceID, "Work Phone 😀")
	b, errB := sealName(k, v.DeviceID, "Work Phone 😀")
	if errA != nil || errB != nil || a == b || !strings.HasPrefix(a, sealedNamePrefix) {
		t.Fatalf("sealed = %q, %q (%v, %v)", a, b, errA, errB)
	}
	if name, ok := openName(k, v.DeviceID, a); !ok || name != "Work Phone 😀" {
		t.Fatalf("opened = %q, %v", name, ok)
	}
}

func TestNamesOpenRefusesWhatItCannotRead(t *testing.T) {
	v := loadNamesVectors(t)
	k := mustHex(t, v.NameKeyHex)
	sealed := v.SealedNames[0].Sealed
	tampered := sealed[:20] + "A" + sealed[21:]
	empty, _ := sealNamesValue(sealedNamePrefix, k, nil, v.DeviceID)
	cases := []struct {
		name   string
		key    []byte
		stored string
	}{
		{"wrong key", numberWrapKey(v.AccountNumber), sealed},
		{"no key", nil, sealed},
		{"short key", k[:16], sealed},
		{"tampered", k, tampered},
		{"label", k, "Swift Fox"},
		{"server-side enc1", k, "enc1:" + strings.TrimPrefix(sealed, sealedNamePrefix)},
		{"v1 zk1", k, "zk1:" + strings.TrimPrefix(sealed, sealedNamePrefix)},
		{"empty name", k, empty},
		{"not base64", k, "zk2:!!!"},
		{"truncated", k, "zk2:AAECAwQFBgcICQoL"},
	}
	for _, tc := range cases {
		if got, ok := openName(tc.key, v.DeviceID, tc.stored); ok || got != "" {
			t.Fatalf("%s: opened %q", tc.name, got)
		}
	}
	if unwrapNameKey(numberWrapKey("ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ"), v.NameKey.Sealed) != nil {
		t.Fatal("another number unwrapped the key")
	}
	short, _ := sealNamesValue(nameKeyPrefix, numberWrapKey(v.AccountNumber), k[:16], nameKeyAAD)
	if unwrapNameKey(numberWrapKey(v.AccountNumber), short) != nil {
		t.Fatal("a 16-byte key unwrapped")
	}
}
