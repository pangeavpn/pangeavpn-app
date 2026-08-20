package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var validTestToken = strings.Repeat("0123456789abcdef", 4)

func TestGenerateTokenMatchesShapePattern(t *testing.T) {
	for i := 0; i < 5; i++ {
		token, err := generateToken(32)
		if err != nil {
			t.Fatalf("generateToken: %v", err)
		}
		if !tokenShapePattern.MatchString(token) {
			t.Fatalf("generated token %q does not match expected shape", token)
		}
	}
}

func TestTokenShapePattern(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid 64 hex", validTestToken, true},
		{"single char", "a", false},
		{"empty", "", false},
		{"uppercase hex", strings.ToUpper(validTestToken), false},
		{"too short", validTestToken[:63], false},
		{"too long", validTestToken + "a", false},
		{"non-hex chars", strings.Repeat("g", 64), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tokenShapePattern.MatchString(tc.input); got != tc.want {
				t.Errorf("MatchString(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestLoadOrCreateToken_GeneratesTokenWhenMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon-token.txt")

	token, err := LoadOrCreateToken(path)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if !tokenShapePattern.MatchString(token) {
		t.Fatalf("token %q does not match expected shape", token)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if string(data) != token {
		t.Fatalf("token file content = %q, want %q", data, token)
	}
}

func TestLoadOrCreateToken_RefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	targetPath := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(targetPath, []byte("attacker-controlled"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	tokenPath := filepath.Join(dir, "daemon-token.txt")
	if err := os.Symlink(targetPath, tokenPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	token, err := LoadOrCreateToken(tokenPath)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if !tokenShapePattern.MatchString(token) {
		t.Fatalf("token %q does not match expected shape", token)
	}

	info, err := os.Lstat(tokenPath)
	if err != nil {
		t.Fatalf("lstat token path: %v", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("token path is still a symlink after regeneration")
	}

	targetData, err := os.ReadFile(targetPath)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(targetData) != "attacker-controlled" {
		t.Fatalf("symlink target was modified: %q", targetData)
	}
}

func TestLoadOrCreateToken_RefusesMalformedExistingContent(t *testing.T) {
	dir := t.TempDir()
	tokenPath := filepath.Join(dir, "daemon-token.txt")
	if err := os.WriteFile(tokenPath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write stub token: %v", err)
	}

	token, err := LoadOrCreateToken(tokenPath)
	if err != nil {
		t.Fatalf("LoadOrCreateToken: %v", err)
	}
	if token == "x" || !tokenShapePattern.MatchString(token) {
		t.Fatalf("adopted malformed stub token: %q", token)
	}
}
