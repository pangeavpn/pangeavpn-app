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

const zk1Prefix = "zk1:"

var (
	accountNumberPattern        = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{24}$`)
	lookAlikeDigits             = strings.NewReplacer("O", "0", "I", "1", "L", "1")
	errRenameNeedsAccountNumber = errors.New("Sign in with your account number to rename devices")
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

// deviceNameKey is nil for a legacy token: the hub stores those in plaintext.
func deviceNameKey(credential string) []byte {
	normalized := normalizeAccountNumber(credential)
	if !accountNumberPattern.MatchString(normalized) {
		return nil
	}
	key, err := hkdf.Key(sha256.New, []byte(normalized), []byte("pangea-device-name"), "zk1", 32)
	if err != nil {
		return nil
	}
	return key
}

func zk1AEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func sealDeviceName(key []byte, name string) (string, error) {
	nonce := make([]byte, 12)
	rand.Read(nonce)
	return sealDeviceNameWith(key, nonce, name)
}

func sealDeviceNameWith(key, nonce []byte, name string) (string, error) {
	aead, err := zk1AEAD(key)
	if err != nil {
		return "", err
	}
	return zk1Prefix + base64.StdEncoding.EncodeToString(aead.Seal(nonce, nonce, []byte(name), nil)), nil
}

func openDeviceName(key []byte, stored string) (string, bool) {
	encoded, ok := strings.CutPrefix(stored, zk1Prefix)
	if !ok || key == nil {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil || len(raw) < 29 {
		return "", false
	}
	aead, err := zk1AEAD(key)
	if err != nil {
		return "", false
	}
	plain, err := aead.Open(nil, raw[:12], raw[12:], nil)
	if err != nil {
		return "", false
	}
	return string(plain), true
}
