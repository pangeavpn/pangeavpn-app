package mobile

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

var deadDropNow = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

type deadDropSigner struct {
	keys    deadDropKeys
	active  ed25519.PrivateKey
	reserve ed25519.PrivateKey
}

func newDeadDropSigner(t *testing.T) deadDropSigner {
	t.Helper()
	activePub, activePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	reservePub, reservePriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return deadDropSigner{
		keys: deadDropKeys{
			active:  base64.StdEncoding.EncodeToString(activePub),
			reserve: base64.StdEncoding.EncodeToString(reservePub),
		},
		active:  activePriv,
		reserve: reservePriv,
	}
}

func validPayload() map[string]any {
	return map[string]any{
		"v":                1,
		"seq":              7,
		"expires":          "2026-11-19T00:00:00Z",
		"hubIps":           []string{"203.0.113.9"},
		"frontedEndpoints": []string{"relay.example.com"},
	}
}

func signBlob(t *testing.T, key ed25519.PrivateKey, payload map[string]any) string {
	t.Helper()
	message, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(map[string]string{
		"payload": base64.StdEncoding.EncodeToString(message),
		"sig":     base64.StdEncoding.EncodeToString(ed25519.Sign(key, message)),
	})
	return string(blob)
}

func verifyAt(signer deadDropSigner, raw string, minSeq int64) (deadDropPayload, bool) {
	return verifyDeadDropBlob([]byte(raw), signer.keys, minSeq, deadDropNow)
}

func TestVerifyDeadDropAcceptsASignedBlob(t *testing.T) {
	signer := newDeadDropSigner(t)
	got, ok := verifyAt(signer, signBlob(t, signer.active, validPayload()), 0)
	if !ok {
		t.Fatal("a valid blob was rejected")
	}
	if got.Seq != 7 || !sameStrings(got.HubIPs, []string{"203.0.113.9"}) ||
		!sameStrings(got.FrontedEndpoints, []string{"relay.example.com"}) {
		t.Fatalf("got %+v", got)
	}
}

// Re-signing with the reserve moves every client without an emergency release.
func TestVerifyDeadDropAcceptsTheReserveKey(t *testing.T) {
	signer := newDeadDropSigner(t)
	if _, ok := verifyAt(signer, signBlob(t, signer.reserve, validPayload()), 0); !ok {
		t.Fatal("a blob signed with the reserve key was rejected")
	}
}

func TestVerifyDeadDropRejects(t *testing.T) {
	signer := newDeadDropSigner(t)
	_, stranger, _ := ed25519.GenerateKey(rand.Reader)
	with := func(edit func(map[string]any)) string {
		payload := validPayload()
		edit(payload)
		return signBlob(t, signer.active, payload)
	}
	valid := signBlob(t, signer.active, validPayload())
	var envelope map[string]string
	_ = json.Unmarshal([]byte(valid), &envelope)
	padded, _ := json.Marshal(map[string]string{"payload": envelope["payload"] + "=", "sig": envelope["sig"]})

	tests := []struct {
		name   string
		raw    string
		minSeq int64
	}{
		{"unknown signer", signBlob(t, stranger, validPayload()), 0},
		{"replayed older seq", valid, 7},
		{"expired", with(func(p map[string]any) { p["expires"] = "2026-09-01T00:00:00Z" }), 0},
		{"unparseable expiry", with(func(p map[string]any) { p["expires"] = "soon" }), 0},
		{"wrong version", with(func(p map[string]any) { p["v"] = 2 }), 0},
		{"negative seq", with(func(p map[string]any) { p["seq"] = -1 }), -5},
		{"fractional seq", with(func(p map[string]any) { p["seq"] = 7.5 }), 0},
		{"names nothing usable", with(func(p map[string]any) {
			p["hubIps"] = []string{"node.example.com", "300.1.1.1"}
			p["frontedEndpoints"] = []string{"10.0.0.1", "bare"}
		}), 0},
		{"non-canonical base64", string(padded), 0},
		{"not json", "not json", 0},
		{"oversize", strings.Repeat(" ", deadDropMaxBlobBytes+1), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := verifyAt(signer, tt.raw, tt.minSeq); ok {
				t.Fatalf("accepted %+v", got)
			}
		})
	}
}

func TestVerifyDeadDropKeepsOnlyUsableEntries(t *testing.T) {
	signer := newDeadDropSigner(t)
	payload := validPayload()
	payload["hubIps"] = []string{" 203.0.113.9 ", "node.example.com", "203.0.113.9", "01.2.3.4"}
	payload["frontedEndpoints"] = []string{"Relay.Example.com", "10.0.0.1", "relay.example.com"}
	got, ok := verifyAt(signer, signBlob(t, signer.active, payload), 0)
	if !ok {
		t.Fatal("blob rejected")
	}
	if !sameStrings(got.HubIPs, []string{"203.0.113.9"}) || !sameStrings(got.FrontedEndpoints, []string{"relay.example.com"}) {
		t.Fatalf("got %+v", got)
	}
}

func TestDeadDropDue(t *testing.T) {
	now := deadDropNow.UnixMilli()
	interval := deadDropMinInterval.Milliseconds()
	tests := []struct {
		name string
		last int64
		want bool
	}{
		{"never tried", 0, true},
		{"just tried", now - 1000, false},
		{"one interval ago", now - interval, true},
		{"clock went backwards", now + interval*100, true},
	}
	for _, tt := range tests {
		if got := deadDropDue(tt.last, now); got != tt.want {
			t.Errorf("%s: deadDropDue = %v, want %v", tt.name, got, tt.want)
		}
	}
}

// Rotating the verify keys or the publishing URLs on one platform only would
// strand the other at the moment the dead drop is its last way in.
func TestDeadDropPinsMatchTheDesktopClient(t *testing.T) {
	root := filepath.Join("..", "..", "apps", "desktop", "src", "main")
	keysSource, err := os.ReadFile(filepath.Join(root, "deadDropKeys.ts"))
	if err != nil {
		t.Skipf("desktop source not available: %v", err)
	}
	for name, want := range map[string]string{"active": productionDeadDropKeys.active, "reserve": productionDeadDropKeys.reserve} {
		match := regexp.MustCompile(name + `: "([^"]+)"`).FindSubmatch(keysSource)
		if match == nil || string(match[1]) != want {
			t.Errorf("%s key differs from the desktop pin", name)
		}
	}

	fetchSource, err := os.ReadFile(filepath.Join(root, "deadDrop.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, constant := range []string{"DEAD_DROP_ORG", "DEAD_DROP_REPO", "DEAD_DROP_FILE"} {
		match := regexp.MustCompile(constant + ` = "([^"]+)"`).FindSubmatch(fetchSource)
		if match == nil {
			t.Fatalf("%s not found in deadDrop.ts", constant)
		}
		for _, url := range deadDropURLs {
			if !strings.Contains(url, string(match[1])) {
				t.Errorf("%s does not carry %s %q", url, constant, match[1])
			}
		}
	}
}
