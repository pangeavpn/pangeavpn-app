package mobile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
)

const (
	namesSalt        = "pangea-names"
	nameKeyPrefix    = "wk1:"
	sealedNamePrefix = "zk2:"
	nameKeyAAD       = "nameKey"
)

var (
	accountNumberPattern        = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{24}$`)
	lookAlikeDigits             = strings.NewReplacer("O", "0", "I", "1", "L", "1")
	errRenameNeedsAccountNumber = errors.New("Sign in with your account number to rename devices")
	errNameKeyUnreadable        = errors.New("mobile: this account's device names key does not open with your account number")
)

func normalizeAccountNumber(raw string) string {
	stripped := strings.Map(func(r rune) rune {
		if r == '-' || isJSTrimSpace(r) {
			return -1
		}
		return r
	}, strings.ToUpper(raw))
	return lookAlikeDigits.Replace(stripped)
}

// numberWrapKey is nil for a legacy token: the hub stores those readable.
func numberWrapKey(credential string) []byte {
	normalized := normalizeAccountNumber(credential)
	if !accountNumberPattern.MatchString(normalized) {
		return nil
	}
	key, err := hkdf.Key(sha256.New, []byte(normalized), []byte(namesSalt), "wrap:number:v1", 32)
	if err != nil {
		return nil
	}
	return key
}

func newNameKey() []byte {
	k := make([]byte, 32)
	rand.Read(k)
	return k
}

func namesAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealNamesValue(prefix string, key, plain []byte, aad string) (string, error) {
	nonce := make([]byte, 12)
	rand.Read(nonce)
	return sealNamesValueWith(prefix, key, nonce, plain, aad)
}

func sealNamesValueWith(prefix string, key, nonce, plain []byte, aad string) (string, error) {
	aead, err := namesAEAD(key)
	if err != nil {
		return "", err
	}
	return prefix + base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, plain, []byte(aad))), nil
}

// openNamesValue is nil for anything it cannot read, an empty plaintext included.
func openNamesValue(prefix string, key []byte, stored, aad string) []byte {
	encoded, ok := strings.CutPrefix(stored, prefix)
	if !ok || len(key) != 32 {
		return nil
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < 29 {
		return nil
	}
	aead, err := namesAEAD(key)
	if err != nil {
		return nil
	}
	plain, err := aead.Open(nil, raw[:12], raw[12:], []byte(aad))
	if err != nil {
		return nil
	}
	return plain
}

func wrapNameKey(wrapKey, k []byte) (string, error) {
	return sealNamesValue(nameKeyPrefix, wrapKey, k, nameKeyAAD)
}

func unwrapNameKey(wrapKey []byte, wrapped string) []byte {
	k := openNamesValue(nameKeyPrefix, wrapKey, wrapped, nameKeyAAD)
	if len(k) != 32 {
		return nil
	}
	return k
}

func sealName(k []byte, deviceID, name string) (string, error) {
	return sealNamesValue(sealedNamePrefix, k, []byte(name), strings.ToLower(deviceID))
}

func openName(k []byte, deviceID, sealed string) (string, bool) {
	plain := openNamesValue(sealedNamePrefix, k, sealed, strings.ToLower(deviceID))
	return string(plain), plain != nil
}
