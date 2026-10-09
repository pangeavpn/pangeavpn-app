//go:build hub_live

package mobile

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

// TestLiveHubOpensASealedReply proves the Go sealing against the deployed hub.
// Unauthenticated on purpose: an inner 401 still has to decrypt.
func TestLiveHubOpensASealedReply(t *testing.T) {
	sealed, err := sealRequest(http.MethodGet, "/api/client/regions", map[string]string{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sealed.route != routeSecureV2 {
		t.Fatalf("sealed for %s, want %s", sealed.route, routeSecureV2)
	}

	// Verification off: the envelope is sealed end to end, and some networks re-sign TLS.
	client := &http.Client{
		Timeout:   15 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Post("https://"+hubHost+sealed.route, "application/json", bytes.NewReader(sealed.envelope))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("hub answered %d: %s", resp.StatusCode, body)
	}

	var encrypted encryptedResponse
	if err := json.Unmarshal(body, &encrypted); err != nil {
		t.Fatalf("not an encrypted reply: %v (%s)", err, body)
	}
	inner, err := sealed.open(encrypted)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Logf("hub opened the v2 envelope; inner status %d", inner.Status)
}
