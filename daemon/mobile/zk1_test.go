package mobile

import (
	"encoding/hex"
	"strings"
	"testing"
)

const (
	zk1Credential = "0123-4567-89AB-CDEF-GHJK-MNPQ"
	zk1Sealed     = "zk1:AAECAwQFBgcICQoLHwxG/vnIxpQKsmvjxj/f6LESbeKr2oZxy10n5ycj7A=="
	zk1Plaintext  = "Alice's MacBook"
)

func TestZK1PinnedVector(t *testing.T) {
	if got := normalizeAccountNumber(zk1Credential); got != "0123456789ABCDEFGHJKMNPQ" {
		t.Fatalf("normalized = %q", got)
	}
	key := deviceNameKey(zk1Credential)
	if hex.EncodeToString(key) != "70e06e761f70ca150869c979046413980aaad153c012ed9a9ee5adfb4e9e3c3f" {
		t.Fatalf("key = %x", key)
	}
	nonce, _ := hex.DecodeString("000102030405060708090a0b")
	if sealed, err := sealDeviceNameWith(key, nonce, zk1Plaintext); err != nil || sealed != zk1Sealed {
		t.Fatalf("sealed = %q, %v", sealed, err)
	}
	if name, ok := openDeviceName(key, zk1Sealed); !ok || name != zk1Plaintext {
		t.Fatalf("opened = %q, %v", name, ok)
	}
}

func TestZK1KeySourceIsOnlyAnAccountNumber(t *testing.T) {
	want := deviceNameKey(zk1Credential)
	for _, same := range []string{" 0123 4567 89ab cdef ghjk mnpq\n", "o123-4567-89AB-CDEF-GHJK-MNPQ", "0123456789ABCDEFGHJKMNPQ"} {
		if got := deviceNameKey(same); hex.EncodeToString(got) != hex.EncodeToString(want) {
			t.Fatalf("deviceNameKey(%q) differs from the canonical form", same)
		}
	}
	for _, legacy := range []string{"1234567890123456", "", "lk", "0123456789ABCDEFGHJKMNPU", "0123456789ABCDEFGHJKMNPQ0"} {
		if deviceNameKey(legacy) != nil {
			t.Fatalf("deviceNameKey(%q) is not nil", legacy)
		}
	}
}

func TestZK1SealRoundTripsWithAFreshNonce(t *testing.T) {
	key := deviceNameKey(zk1Credential)
	a, errA := sealDeviceName(key, "Work Phone 😀")
	b, errB := sealDeviceName(key, "Work Phone 😀")
	if errA != nil || errB != nil || a == b || !strings.HasPrefix(a, zk1Prefix) {
		t.Fatalf("sealed = %q, %q (%v, %v)", a, b, errA, errB)
	}
	if name, ok := openDeviceName(key, a); !ok || name != "Work Phone 😀" {
		t.Fatalf("opened = %q, %v", name, ok)
	}
}

func TestZK1OpenRefusesWhatItCannotRead(t *testing.T) {
	key := deviceNameKey(zk1Credential)
	other := deviceNameKey("ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ-ZZZZ")
	tampered := zk1Sealed[:20] + "A" + zk1Sealed[21:]
	empty, _ := sealDeviceName(key, "")
	cases := []struct {
		name   string
		key    []byte
		stored string
	}{
		{"wrong key", other, zk1Sealed},
		{"no key", nil, zk1Sealed},
		{"tampered", key, tampered},
		{"label", key, "Swift Fox"},
		{"server-side enc1", key, "enc1:AAECAwQFBgcICQoLHwxG"},
		{"empty name", key, empty},
		{"not base64", key, "zk1:!!!"},
	}
	for _, tc := range cases {
		if got, ok := openDeviceName(tc.key, tc.stored); ok || got != "" {
			t.Fatalf("%s: opened %q", tc.name, got)
		}
	}
}
