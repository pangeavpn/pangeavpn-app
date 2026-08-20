package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var tokenShapePattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

func LoadOrCreateToken(path string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create token directory: %w", err)
	}

	if token, adopted, err := tryAdoptExistingToken(path); err != nil {
		return "", err
	} else if adopted {
		return token, nil
	}

	token, err := generateToken(32)
	if err != nil {
		return "", err
	}
	if err := createTokenFile(path, token); err != nil {
		return "", err
	}
	return token, nil
}

// tryAdoptExistingToken adopts path only if it is a trusted, well-formed
// token file; anything else (symlink, wrong owner, planted stub) is refused
// so the caller regenerates and overwrites it instead of trusting it.
func tryAdoptExistingToken(path string) (token string, adopted bool, err error) {
	info, statErr := os.Lstat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("stat token file: %w", statErr)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		log.Printf("auth: refusing token file %s: it is a symlink; regenerating", path)
		return "", false, nil
	}

	content, readErr := readTrustedTokenFile(path)
	if readErr != nil {
		log.Printf("auth: refusing existing token file %s: %v; regenerating", path, readErr)
		return "", false, nil
	}

	token = strings.TrimSpace(content)
	if !tokenShapePattern.MatchString(token) {
		log.Printf("auth: refusing existing token file %s: unexpected shape; regenerating", path)
		return "", false, nil
	}

	if err := applyTokenReadACL(path); err != nil {
		return "", false, err
	}
	return token, true, nil
}

func applyTokenReadACL(path string) error {
	if err := ensureTokenReadACL(path); err != nil {
		return fmt.Errorf("protect token file: %w", err)
	}
	return nil
}

// createTokenFile writes the token to a sibling temp file, locks down its
// permissions before any content is written, then renames it into place so
// the token is never briefly readable or briefly missing its protections.
func createTokenFile(path, token string) error {
	tmpPath := filepath.Join(filepath.Dir(path), fmt.Sprintf(".daemon-token-%d.tmp", os.Getpid()))

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("create token file: %w", err)
	}

	if err := ensureTokenReadACL(tmpPath); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("protect token file: %w", err)
	}
	if _, err := f.WriteString(token); err != nil {
		f.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write token file: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close token file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("finalize token file: %w", err)
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
