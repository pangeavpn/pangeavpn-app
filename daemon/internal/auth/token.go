package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func LoadOrCreateToken(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create token directory: %w", err)
	}

	bytes, err := os.ReadFile(path)
	if err == nil {
		token := strings.TrimSpace(string(bytes))
		if token != "" {
			if aclErr := applyTokenReadACL(path); aclErr != nil {
				return "", aclErr
			}
			return token, nil
		}
	}
	if err != nil && !os.IsNotExist(err) {
		return "", fmt.Errorf("read token file: %w", err)
	}

	token, err := generateToken(32)
	if err != nil {
		return "", err
	}

	if err := os.WriteFile(path, []byte(token), 0o600); err != nil {
		return "", fmt.Errorf("write token file: %w", err)
	}
	if err := applyTokenReadACL(path); err != nil {
		return "", err
	}

	return token, nil
}

func applyTokenReadACL(path string) error {
	if err := ensureTokenReadACL(path); err != nil {
		if shouldIgnoreTokenACLError(err) {
			return nil
		}
		return err
	}
	return nil
}

func generateToken(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
