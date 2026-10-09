package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServiceErrorResponseClassifiesOnlyTransportExhaustion(t *testing.T) {
	retryable := serviceErrorResponse(errors.Join(errors.New("details"), ErrTransportExhausted))
	if retryable.Error != "transport_exhausted" {
		t.Fatalf("retryable error code = %q, want transport_exhausted", retryable.Error)
	}

	ordinary := serviceErrorResponse(errors.New("preflight failed"))
	if ordinary.Error != "" {
		t.Fatalf("ordinary error code = %q, want empty", ordinary.Error)
	}
}

func TestTransportMemoryClearRoute(t *testing.T) {
	const token = "0123456789abcdef"

	newHandler := func(t *testing.T) (http.Handler, *fakeTransportMemory) {
		t.Helper()
		svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, &fakeKillSwitch{})
		mem := &fakeTransportMemory{entries: map[string]string{"wifi-a": "reality"}}
		svc.SetTransportMemory(mem)
		return NewHandler(token, svc), mem
	}

	t.Run("clears an authenticated post", func(t *testing.T) {
		handler, mem := newHandler(t)
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/transport-memory/clear", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}
		if got, ok := mem.Lookup("wifi-a"); ok {
			t.Fatalf("Lookup = %q after clear, want a miss", got)
		}
	})

	t.Run("refuses an unauthenticated post", func(t *testing.T) {
		handler, mem := newHandler(t)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "http://127.0.0.1/transport-memory/clear", nil))

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
		if _, ok := mem.Lookup("wifi-a"); !ok {
			t.Fatal("memory was cleared by an unauthenticated caller")
		}
	})

	t.Run("refuses a GET", func(t *testing.T) {
		handler, mem := newHandler(t)
		req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/transport-memory/clear", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
		}
		if _, ok := mem.Lookup("wifi-a"); !ok {
			t.Fatal("memory was cleared by a GET")
		}
	})
}

func TestDisconnectRouteReportsWhyItFailed(t *testing.T) {
	const token = "0123456789abcdef"
	profile := testProfile()
	ks := &fakeKillSwitch{clearErr: errors.New("nft delete table: permission denied")}
	svc := newTestService(t, &fakeCloakManager{}, &fakeNaiveManager{}, &fakeWGManager{}, ks, profile)
	if err := svc.Connect(context.Background(), profile.ID, ConnectOptions{}); err != nil {
		t.Fatalf("connect failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1/disconnect", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	NewHandler(token, svc).ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	var body okResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
	if body.Error != "disconnect_incomplete" {
		t.Errorf("error = %q, want disconnect_incomplete", body.Error)
	}
	if !strings.Contains(body.Detail, "kill switch clear failed") {
		t.Errorf("detail = %q, want the kill switch reason", body.Detail)
	}
}
