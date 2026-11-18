package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/pangeavpn/pangeavpn-desktop/daemon/internal/state"
)

func TestUserRootsRouteFeedsOnlyTheCDNDial(t *testing.T) {
	const token = "0123456789abcdef"
	const roots = "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
	handler := NewHandler(token, svc)
	post := func(body string, authed bool) int {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/user-roots", strings.NewReader(body))
		if authed {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(`{"pem":"x"}`, false); code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want %d", code, http.StatusUnauthorized)
	}
	if code := post(`{}`, true); code != http.StatusBadRequest {
		t.Fatalf("missing pem status = %d, want %d", code, http.StatusBadRequest)
	}
	body, _ := json.Marshal(map[string]string{"pem": roots})
	if code := post(string(body), true); code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}

	profile := state.CDNProfile{RemoteHost: "edge.example", ServerName: "cdn.example", UUID: "u"}
	if got := svc.cdnStart(profile).ExtraRootsPEM; got != roots {
		t.Fatalf("cdnStart roots = %q, want the posted bundle", got)
	}
	stored, _ := json.Marshal(profile)
	if strings.Contains(string(stored), "BEGIN CERTIFICATE") {
		t.Fatal("the CDN profile serialises the user's roots")
	}

	if code := post(`{"pem":""}`, true); code != http.StatusOK || svc.cdnStart(profile).ExtraRootsPEM != "" {
		t.Fatal("an empty bundle did not clear the user's roots")
	}
}
