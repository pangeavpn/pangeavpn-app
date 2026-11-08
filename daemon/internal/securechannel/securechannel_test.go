package securechannel_test

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/securechannel"
	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/securechannel/securechanneltest"
)

func newHub(t *testing.T) (*securechanneltest.Hub, securechannel.Keys) {
	t.Helper()
	hub, keys, err := securechanneltest.NewHub()
	if err != nil {
		t.Fatal(err)
	}
	return hub, keys
}

func TestSealRoundTripsAgainstAHubHoldingBothKeys(t *testing.T) {
	hub, keys := newHub(t)
	sealed, err := securechannel.Seal(keys, "GET", "/api/client/reach?n=ab", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, reply, err := hub.Open(sealed.Envelope)
	if err != nil {
		t.Fatalf("hub could not open the envelope: %v", err)
	}
	if req.Method != "GET" || req.Route != "/api/client/reach?n=ab" || req.Headers == nil {
		t.Fatalf("inner request = %+v", req)
	}
	if age := time.Since(time.UnixMilli(req.TS)); age < 0 || age > 5*time.Second {
		t.Fatalf("ts %d is not the send time", req.TS)
	}
	raw, err := reply(securechannel.InnerResponse{Status: 200, Body: json.RawMessage(`{"n":"ab"}`)})
	if err != nil {
		t.Fatal(err)
	}
	inner, err := sealed.Open(raw)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if inner.Status != 200 || string(inner.Body) != `{"n":"ab"}` {
		t.Fatalf("inner response = %d %s", inner.Status, inner.Body)
	}
}

func TestOpenRejectsAReplyToAnotherRequest(t *testing.T) {
	hub, keys := newHub(t)
	first, _ := securechannel.Seal(keys, "GET", "/x", nil, nil)
	second, _ := securechannel.Seal(keys, "GET", "/x", nil, nil)
	_, reply, err := hub.Open(first.Envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := reply(securechannel.InnerResponse{Status: 200})
	if _, err := second.Open(raw); err == nil {
		t.Fatal("a reply sealed for one request opened for another")
	}
	if _, err := first.Open(first.Envelope); err == nil {
		t.Fatal("a reflected request opened as a reply")
	}
}

func TestSealRefusesAnUnusableKEMKey(t *testing.T) {
	keys := securechannel.HubKeys()
	keys.KEMPublic = "AAAA"
	if _, err := securechannel.Seal(keys, "GET", "/x", nil, nil); err == nil {
		t.Fatal("sealed with a broken KEM key; there is no v1 to fall back to")
	}
}

// The hub and desktop pin the same vector; a drift in salt, label or order fails all three.
func TestKeyScheduleMatchesThePinnedVector(t *testing.T) {
	c2s, s2c, err := securechannel.DeriveKeys(make([]byte, 32), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	if got := hex.EncodeToString(c2s); got != "d443eb63dab3ae24d1c3c7ea022d229564c44ace58e2cdcc4a622de51adf1aa9" {
		t.Errorf("c2s = %s", got)
	}
	if got := hex.EncodeToString(s2c); got != "b22fe5e3f91412fd7a81a8c46dbf8f5ba54a565b688c8b52ad71335123c0e1b2" {
		t.Errorf("s2c = %s", got)
	}
}

func TestHubKeysMatchTheDesktopPins(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "..", "apps", "desktop", "src", "main", "secureChannel.ts"))
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
	keys := securechannel.HubKeys()
	if got, want := base64.StdEncoding.EncodeToString(keys.X25519Public), read(`SERVER_PUBLIC_KEY_B64 = "([^"]+)"`); got != want {
		t.Errorf("X25519 key = %s, desktop pins %s", got, want)
	}
	if keys.KEMPublic != read(`SERVER_KEM_PUBLIC_KEY_B64 =\s*"([^"]+)"`) {
		t.Error("ML-KEM key differs from the desktop pin")
	}
}
